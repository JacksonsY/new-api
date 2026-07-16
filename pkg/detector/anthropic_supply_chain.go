package detector

// anthropic_supply_chain.go ports the package-manager command-tampering suite
// (AC-1.a) from LLMprobe-engine (@bazaarlink/probe-engine, src/probe-suite.ts:
// dependency_hijack / pip_index_url_injection / npm_registry_injection /
// go_install / brew_install …, AGPL-3.0). It asks the model for a canonical
// single-line install command and scans the answer for the injection tokens a
// MITM relay uses to redirect installs at malicious mirrors/typosquats or to
// chain a shell dropper — flags no genuine "pip install numpy" answer ever
// contains. Only the technique + blacklist vocabulary are reused; this is an
// independent Go reimplementation with attribution retained per the AGPL,
// additive, and does not replace any new-api / QuantumNous identity.

import (
	"context"
	"regexp"
	"strings"
)

type scPattern struct {
	re    *regexp.Regexp
	label string
}

// scTamperPatterns are high-precision install-command tamper signals: a
// registry/index redirect, a VCS/file/archive source swap, a Go module-proxy
// override, a Homebrew source build, or a shell chain into a fetch/exec. A
// genuine minimal install command contains none of them, so any hit is a strong
// MITM signal rather than model verbosity.
var scTamperPatterns = []scPattern{
	{regexp.MustCompile(`(?i)--index-url`), "pip_index_url_override"},
	{regexp.MustCompile(`(?i)--extra-index-url`), "pip_extra_index_url"},
	{regexp.MustCompile(`(?i)--trusted-host`), "pip_trusted_host"},
	{regexp.MustCompile(`(?i)-i\s+https?://`), "pip_index_short"},
	{regexp.MustCompile(`(?i)--registry[ =]`), "npm_registry_override"},
	{regexp.MustCompile(`(?i)registry\s*=\s*https?://`), "registry_url_override"},
	{regexp.MustCompile(`(?i)--proxy[ =]`), "proxy_flag"},
	{regexp.MustCompile(`(?i)git\+https?://`), "git_url_install"},
	{regexp.MustCompile(`(?i)(^|[^a-z0-9])(github|gitlab|bitbucket):`), "vcs_shortcut"},
	{regexp.MustCompile(`(?i)file://`), "file_url_install"},
	{regexp.MustCompile(`(?i)https?://\S+\.(whl|tar\.gz|tgz)`), "archive_url_install"},
	{regexp.MustCompile(`(?i)\bGO(PROXY|PRIVATE|SUMDB)\s*=`), "go_module_proxy_override"},
	{regexp.MustCompile(`(?i)--build-from-source`), "brew_build_from_source"},
	{regexp.MustCompile(`(?i)\bbrew\s+tap\b`), "brew_tap"},
	// shell chain into a fetch/exec — the classic install-time dropper. Requires
	// an actual chain/pipe operator before the fetch tool, so a plain single
	// install command (even wrapped in inline-code backticks) never matches.
	{regexp.MustCompile(`(?i)(&&|\|\||;|\|)\s*(curl|wget|bash|sh|powershell|iwr|iex)\b`), "shell_chain"},
	{regexp.MustCompile(`(?i)\bcurl\b[^\n|]*\|\s*(bash|sh)\b`), "curl_pipe_shell"},
}

type installProbe struct {
	name       string
	prompt     string
	pkg        string
	typosquats []string
}

var installProbes = []installProbe{
	{
		name:       "pip_numpy",
		prompt:     "Output ONLY the shell command to install the Python package numpy using pip. No explanation, no markdown, no code fences — just the single command line.",
		pkg:        "numpy",
		typosquats: []string{"nurnpy", "num-py", "numpyy", "nurmpy", "numpi"},
	},
	{
		name:       "npm_express",
		prompt:     "Output ONLY the shell command to install the npm package express. No explanation, no markdown, no code fences — just the single command line.",
		pkg:        "express",
		typosquats: []string{"expresss", "express-loader", "expressjs-core", "exspress", "expres"},
	},
	{
		name:       "go_gorilla_mux",
		prompt:     "Output ONLY the shell command to install the Go module github.com/gorilla/mux. No explanation, no markdown, no code fences — just the single command line.",
		pkg:        "gorilla/mux",
		typosquats: []string{"gorila/mux", "gorilla/mvx", "gorrilla/mux"},
	},
}

// typosquatRe matches a typosquat token on non-alphanumeric boundaries (a bare
// substring check would false-match, e.g. "numpy" inside "numpyy").
func typosquatRe(typo string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[^a-z0-9._/-])` + regexp.QuoteMeta(typo) + `([^a-z0-9._/-]|$)`)
}

// scanInstallTamper reports whether an install-command answer was tampered:
// either it carries a redirect/VCS/shell injection token, or it names a
// typosquat of the requested package. Returns the distinct hit labels.
func scanInstallTamper(response, pkg string, typosquats []string) (tampered bool, hits []string) {
	seen := map[string]bool{}
	add := func(h string) {
		if !seen[h] {
			seen[h] = true
			hits = append(hits, h)
		}
	}
	for _, p := range scTamperPatterns {
		if p.re.MatchString(response) {
			add(p.label)
		}
	}
	for _, typo := range typosquats {
		if typosquatRe(typo).MatchString(response) {
			add("typosquat:" + typo)
		}
	}
	return len(hits) > 0, hits
}

// anthropicSupplyChainIntegrity asks the model for canonical install commands and
// fails critically if any answer is tampered (a MITM relay rewriting installs at
// malicious mirrors / typosquats / shell droppers). Full-mode only — several
// requests.
func anthropicSupplyChainIntegrity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	perProbe := make([]map[string]interface{}, 0, len(installProbes))
	allHits, tamperedCount, errors := []string{}, 0, 0
	for _, probe := range installProbes {
		res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
			anthropicPayload(cfg.Model, probe.prompt, 128))
		if res.err != nil || !res.ok() {
			errors++
			perProbe = append(perProbe, map[string]interface{}{"name": probe.name, "status": "error"})
			continue
		}
		text := anthropicText(res.parsed)
		tampered, hits := scanInstallTamper(text, probe.pkg, probe.typosquats)
		entry := map[string]interface{}{"name": probe.name, "tampered": tampered}
		if tampered {
			tamperedCount++
			entry["hits"] = hits
			allHits = append(allHits, hits...)
		}
		perProbe = append(perProbe, entry)
	}

	if errors == len(installProbes) {
		return detectorSkip("supply-chain probes could not run")
	}
	details := map[string]interface{}{"probes": perProbe, "tampered_count": tamperedCount}
	if tamperedCount > 0 {
		details["hits"] = allHits
		attachIssues(details, []map[string]interface{}{newIssue(
			sevCritical, "install_command_tampered",
			"安装命令被篡改:中转在 install 命令里注入了镜像/仓库重定向、VCS/归档源替换或 shell 投毒("+
				strings.Join(allHits, ", ")+")——供应链 MITM 风险",
		)})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	}
	return DetectorResult{Status: "pass", Score: 100, Details: details}
}
