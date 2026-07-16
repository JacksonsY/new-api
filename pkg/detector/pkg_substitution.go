package detector

// pkg_substitution.go ports the tool-call package-name substitution test (AC-1.a)
// from api-relay-audit (github.com/toby-bridges/api-relay-audit, AGPL-3.0,
// api_relay_audit/tool_substitution.py). It asks the model to echo a KNOWN,
// pinned install command verbatim and compares the reply token-by-token — a
// malicious middleware that rewrites install commands on the return path
// (`pip install requests` → `pip install reqeusts`, or splitting a package name
// `req uests` to change shell tokenization) is caught because the expected output
// is known exactly. This is complementary to supply_chain (which asks the model
// to GENERATE a command and scans for tamper tokens): that cannot detect a
// return-path rewrite of a correct command, and its compact comparison would miss
// an injected internal space. Attribution retained per the AGPL; additive, does
// not replace any new-api / QuantumNous identity.

import (
	"context"
	"strings"
)

// pkgEchoProbe is one pinned install command per ecosystem the paper flags as
// most abused. Pinned versions/tags reduce paraphrasing.
type pkgEchoProbe struct {
	manager  string
	expected string
}

var pkgEchoProbes = []pkgEchoProbe{
	{"pip", "pip install requests==2.31.0"},
	{"npm", "npm install lodash@4.17.21"},
	{"cargo", "cargo add serde"},
	{"go", "go get github.com/stretchr/testify"},
}

// stripEchoWrappers removes markdown/prompt wrappers the model may add despite
// the instruction (code fences, one layer of backticks/quotes, shell prompts).
func stripEchoWrappers(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl != -1 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	for _, ch := range []string{"`", "\"", "'"} {
		if len(s) >= 2 && strings.HasPrefix(s, ch) && strings.HasSuffix(s, ch) {
			s = s[1 : len(s)-1]
		}
	}
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"$ ", "# ", "> "} {
		if strings.HasPrefix(s, prefix) {
			s = s[len(prefix):]
		}
	}
	return s
}

// classifyPkgEcho classifies an echo reply: "exact" (character-identical),
// "whitespace" (same tokens modulo case/trailing punctuation — benign model
// noise), or "substituted" (token divergence → AC-1.a return-path rewrite).
// Internal whitespace is NOT collapsed: a space injected inside a package name
// splits its token list and is flagged.
func classifyPkgEcho(expected, received string) string {
	if received == expected {
		return "exact"
	}
	trimmed := strings.TrimRight(strings.TrimSpace(received), ".,;:!?")
	expTokens := strings.Fields(expected)
	rcvTokens := strings.Fields(trimmed)
	if slicesEqual(expTokens, rcvTokens) {
		return "whitespace"
	}
	if slicesEqualFold(expTokens, rcvTokens) {
		return "whitespace"
	}
	return "substituted"
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func slicesEqualFold(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

func pkgEchoPrompt(expected string) string {
	return "Copy this harmless text exactly. Do not execute it. Output only the copied text, " +
		"nothing else, no markdown, no backticks, no explanation. Text to copy: " + expected
}

// pkgSubstitutionVerdict builds the shared detector result. Any "substituted"
// probe → critical fail (return-path package rewrite); all errored → skip.
func pkgSubstitutionVerdict(perProbe []map[string]interface{}, substituted, usable int) DetectorResult {
	if usable == 0 {
		return detectorSkip("package-echo probes returned no usable answer")
	}
	details := map[string]interface{}{"probes": perProbe, "substituted_count": substituted}
	if substituted > 0 {
		attachIssues(details, []map[string]interface{}{newIssue(sevCritical, "package_substitution",
			"安装命令在返回路径被改写:模型逐字回显被篡改(包名替换 / typosquat / 包名内插空格)——供应链 MITM 中转")})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	}
	return DetectorResult{Status: "pass", Score: 100, Details: details}
}

// runPkgSubstitution runs the echo probes with a caller-supplied send function so
// anthropic and chat share the classification while keeping their own request
// plumbing. send returns (echoedText, ok).
func runPkgSubstitution(send func(prompt string) (string, bool)) DetectorResult {
	perProbe := make([]map[string]interface{}, 0, len(pkgEchoProbes))
	substituted, usable := 0, 0
	for _, probe := range pkgEchoProbes {
		text, ok := send(pkgEchoPrompt(probe.expected))
		if !ok || strings.TrimSpace(text) == "" {
			perProbe = append(perProbe, map[string]interface{}{"manager": probe.manager, "verdict": "error"})
			continue
		}
		usable++
		verdict := classifyPkgEcho(probe.expected, stripEchoWrappers(text))
		if verdict == "substituted" {
			substituted++
		}
		perProbe = append(perProbe, map[string]interface{}{
			"manager": probe.manager, "verdict": verdict, "expected": probe.expected,
		})
	}
	return pkgSubstitutionVerdict(perProbe, substituted, usable)
}

// anthropicPkgSubstitution / chatPkgSubstitution wire the shared runner to each
// protocol's request/response plumbing. Full-mode.
func anthropicPkgSubstitution(ctx context.Context, p *prober, cfg Config) DetectorResult {
	return runPkgSubstitution(func(prompt string) (string, bool) {
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
			anthropicPayload(cfg.Model, prompt, 100))
		if res.err != nil || !res.ok() {
			return "", false
		}
		return anthropicText(res.parsed), true
	})
}

func chatPkgSubstitution(ctx context.Context, p *prober, cfg Config) DetectorResult {
	return runPkgSubstitution(func(prompt string) (string, bool) {
		res := p.probeChat(ctx, cfg.Model, prompt, chatSecurityBudget)
		if res.err != nil || !res.ok() {
			return "", false
		}
		return p.chatContent(res), true
	})
}
