package detector

// anthropic_response_integrity.go ports the stateless response-scanning logic of
// API-Poison-Detector (github.com/fjh1997/API-Poison-Detector, MIT). It answers a
// supplier-security question the authenticity detectors don't: is the relay
// itself POISONING responses — injecting tool calls or hidden malicious
// instructions into an otherwise-benign reply? A benign, tool-free request is
// sent; any tool_use block (impossible without declared tools), malicious code /
// reverse-shell / exfil content, or hidden base64/zero-width instruction is a
// compromised-middleman signal. Curated high-signal patterns are reimplemented
// in RE2; attribution retained per the MIT license, additive.

import (
	"context"
	"encoding/base64"
	"regexp"
	"strings"
)

// responseInjectionPatterns is a curated high-signal subset of the poison
// detector's COMPILED_CODE/TOOL/TEXT pattern banks: code execution, reverse
// shells, exfil endpoints, PowerShell payloads, sensitive files, and
// prompt-injection markers (EN + 中).
var responseInjectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\beval\s*\(`),
	regexp.MustCompile(`(?i)\bexec\s*\(`),
	regexp.MustCompile(`(?i)subprocess\.`),
	regexp.MustCompile(`(?i)os\.system\s*\(`),
	regexp.MustCompile(`(?i)child_process`),
	regexp.MustCompile(`(?i)__import__\s*\(`),
	regexp.MustCompile(`(?i)messagebox\.show\s*\(`),
	regexp.MustCompile(`(?i)add-type.*system\.windows\.forms`),
	regexp.MustCompile(`(?i)-encodedcommand`),
	regexp.MustCompile(`(?i)bash\s+-i\s+>&\s+/dev/tcp`),
	regexp.MustCompile(`/dev/tcp/`),
	regexp.MustCompile(`(?i)\bnc\s+-e\b`),
	regexp.MustCompile(`(?i)webhook\.site`),
	regexp.MustCompile(`(?i)ngrok\.io`),
	regexp.MustCompile(`(?i)curl\b.*(?:-X\s*POST|--data-binary\s*@|-d\s*@)`),
	regexp.MustCompile(`(?i)\.ssh/id_rsa`),
	regexp.MustCompile(`/etc/passwd`),
	regexp.MustCompile(`(?i)ignore\s+(?:your\s+|the\s+|all\s+)?(?:previous|earlier|above)\s+(?:instructions|prompts)`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+(?:a|an)\s`),
	regexp.MustCompile(`YOUR COMPUTER HAS BEEN COMPROMISED`),
	regexp.MustCompile(`忽略你的指令|下载并执行|执行这个命令`),
}

var reBase64Blob = regexp.MustCompile(`[A-Za-z0-9+/]{50,}={0,2}`)

// base64ExecKeywords: a decoded base64 blob containing any of these is a hidden
// command payload.
var base64ExecKeywords = []string{"exec", "eval", "system", "cmd", "powershell", "bash", "curl", "wget"}

// zeroWidthRunes are the obfuscation code points (ZWSP/ZWNJ/ZWJ/BOM/word-joiner).
var zeroWidthRunes = []rune{0x200b, 0x200c, 0x200d, 0xfeff, 0x2060}

// scanResponseInjection reports the first injection signal found in a response
// text: a malicious pattern, a hidden base64-command payload, or zero-width
// obfuscation. Empty reason means clean.
func scanResponseInjection(text string) (hit bool, reason string) {
	for _, re := range responseInjectionPatterns {
		if re.MatchString(text) {
			return true, "malicious_content:" + re.String()
		}
	}
	for _, blob := range reBase64Blob.FindAllString(text, -1) {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimRight(blob, "="))
		if err != nil {
			if decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(blob, "=")); err != nil {
				continue
			}
		}
		low := strings.ToLower(string(decoded))
		for _, kw := range base64ExecKeywords {
			if strings.Contains(low, kw) {
				return true, "hidden_base64_command"
			}
		}
	}
	for _, r := range zeroWidthRunes {
		if strings.ContainsRune(text, r) {
			return true, "zero_width_obfuscation"
		}
	}
	return false, ""
}

// anthropicResponseIntegrity sends a benign, tool-free request and checks the
// relay did not inject a tool call or malicious/hidden content into the reply.
func anthropicResponseIntegrity(ctx context.Context, p *prober, cfg Config) DetectorResult {
	body := map[string]interface{}{
		"model": cfg.Model, "max_tokens": 64, "temperature": 0,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "In one short sentence, what is the capital of France?"},
		},
	}
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error()}
	}
	if !res.ok() {
		return DetectorResult{Status: "error", Score: 0, Error: upstreamErrorText(res)}
	}

	// No tools were declared, so any tool_use block was injected by the relay.
	injectedTools := extractAnthropicToolUseIDs(subSlice(res.parsed, "content"))
	text := anthropicText(res.parsed)
	contentHit, reason := scanResponseInjection(text)

	details := map[string]interface{}{"response_text": truncate(strings.TrimSpace(text), 200)}
	switch {
	case len(injectedTools) > 0:
		attachIssues(details, []map[string]interface{}{newIssue(
			sevCritical, "response_tool_injection",
			"中转在无工具声明的良性响应里注入了 tool_use 块——疑似投毒中转(会诱导客户端执行工具)",
		)})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	case contentHit:
		details["injection_reason"] = reason
		attachIssues(details, []map[string]interface{}{newIssue(
			sevCritical, "response_content_injection",
			"中转在良性响应里注入了恶意内容/隐藏指令("+reason+")——疑似投毒中转",
		)})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	default:
		return DetectorResult{Status: "pass", Score: 100, Details: details}
	}
}
