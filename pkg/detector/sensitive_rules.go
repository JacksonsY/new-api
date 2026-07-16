package detector

// sensitive_rules.go ports the sensitive-data-leakage rule bank from AITTAK
// (github.com/KaGty1/AITTAK, Apache-2.0, Copyright (c) 2026 KaGty1 AITTAK
// contributors; app/sensitive.py + app/database.py SEED_RULES). It scans a
// relay's response to a controlled benign prompt for credentials / PII /
// internal-network artifacts that a genuine upstream would never emit — flagging
// a relay that bleeds its own internal config (real API keys, private keys,
// internal IPs, JDBC strings) or injects personal data into normal traffic.
//
// Change from the source (Apache-2.0 §4 statement of changes): the phone-number
// rule's negative look-behind/-ahead `(?<!\w)…(?!\w)` is dropped because Go's
// RE2 engine has no look-around. For a leakage AUDIT signal this is the safe
// direction (it may over-match a phone embedded in a longer digit run, never
// under-match). All other 14 rules are copied verbatim as RE2-compatible
// patterns. Attribution retained; additive, does not replace any new-api /
// QuantumNous identity.

import (
	"context"
	"regexp"
	"strings"
)

// sensitive-data categories (from AITTAK SEED_RULES).
const (
	sensCategoryPII        = "个人信息"
	sensCategoryFinancial  = "金融信息"
	sensCategoryCredential = "凭证"
	sensCategoryNetwork    = "网络信息"
)

type sensitiveRule struct {
	name     string
	category string
	re       *regexp.Regexp
}

// sensitiveRules is the 15-rule bank. Patterns are AITTAK's SEED_RULES verbatim
// except rule 1 (look-around removed for RE2 — see file header).
var sensitiveRules = []sensitiveRule{
	{"手机号", sensCategoryPII, regexp.MustCompile(`(?:(?:\+|0{0,2})86)?1(?:3\d|4[5-79]|5[0-35-9]|6[5-7]|7[0-8]|8\d|9[189])\d{8}`)},
	{"身份证号", sensCategoryPII, regexp.MustCompile(`[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]`)},
	{"邮箱地址", sensCategoryPII, regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,5}\b`)},
	{"银行卡号", sensCategoryFinancial, regexp.MustCompile(`\b[456]\d{3}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{4}\b`)},
	{"JWT Token", sensCategoryCredential, regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9._\-]{10,}`)},
	{"AWS Access Key", sensCategoryCredential, regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"阿里云 AccessKey", sensCategoryCredential, regexp.MustCompile(`LTAI[a-z0-9]{12,20}`)},
	{"云厂商 Key 字段", sensCategoryCredential, regexp.MustCompile(`(?i)(?:access[_\-]?key[_\-]?(?:id|secret))\s*[=:]\s*['"]?[A-Za-z0-9/+=_\-]{16,}`)},
	{"通用 Secret/Token", sensCategoryCredential, regexp.MustCompile(`(?i)(?:api[_\-]?key|apikey|secret[_\-]?key|app[_\-]?secret|token)\s*[=:]\s*['"]?[A-Za-z0-9_\-]{20,}`)},
	{"私钥文件", sensCategoryCredential, regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`)},
	{"Authorization 凭证", sensCategoryCredential, regexp.MustCompile(`(?i)(?:basic|bearer)\s+[a-z0-9_.=:_+/\-]{5,100}`)},
	{"密码赋值", sensCategoryCredential, regexp.MustCompile(`(?i)(?:password|passwd|pwd)\s*[=:]\s*['"]?[^\s'"]{6,}`)},
	{"IPv4 内网地址", sensCategoryNetwork, regexp.MustCompile(`(?:127\.0\.0\.1|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})`)},
	{"MAC 地址", sensCategoryNetwork, regexp.MustCompile(`[a-fA-F0-9]{2}(?::[a-fA-F0-9]{2}){5}`)},
	{"JDBC 连接串", sensCategoryNetwork, regexp.MustCompile(`jdbc:[a-z:]+://[a-z0-9.\-_:;=/@?,&]+`)},
}

type sensitiveHit struct {
	Rule     string `json:"rule"`
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// scanSensitiveData runs every rule over text and returns per-rule hit counts,
// in rule order (mirrors AITTAK scan_text findall semantics).
func scanSensitiveData(text string) []sensitiveHit {
	if text == "" {
		return nil
	}
	var hits []sensitiveHit
	for _, r := range sensitiveRules {
		if m := r.re.FindAllString(text, -1); len(m) > 0 {
			hits = append(hits, sensitiveHit{Rule: r.name, Category: r.category, Count: len(m)})
		}
	}
	return hits
}

// sensitiveWorstSeverity returns the highest issue severity implied by the hits.
// Credentials and financial data are critical; PII and network artifacts major.
func sensitiveWorstSeverity(hits []sensitiveHit) string {
	worst := ""
	for _, h := range hits {
		sev := sevMajor
		if h.Category == sensCategoryCredential || h.Category == sensCategoryFinancial {
			sev = sevCritical
		}
		if sev == sevCritical {
			return sevCritical
		}
		worst = sev
	}
	return worst
}

// sensitiveLeakVerdict builds the shared detector result from a scanned response.
func sensitiveLeakVerdict(hits []sensitiveHit) DetectorResult {
	if len(hits) == 0 {
		return DetectorResult{Status: "pass", Score: 100, Details: map[string]interface{}{"hits": []sensitiveHit{}}}
	}
	rules := make([]string, 0, len(hits))
	for _, h := range hits {
		rules = append(rules, h.Rule)
	}
	details := map[string]interface{}{"hits": hits}
	if sensitiveWorstSeverity(hits) == sevCritical {
		attachIssues(details, []map[string]interface{}{newIssue(sevCritical, "sensitive_credential_leak",
			"良性响应里泄漏了凭证/金融敏感信息("+strings.Join(rules, ", ")+")——中转在向客户端暴露真实密钥/卡号等敏感数据")})
		return DetectorResult{Status: "fail", Score: 0, Details: details}
	}
	attachIssues(details, []map[string]interface{}{newIssue(sevMajor, "sensitive_data_leak",
		"良性响应里泄漏了个人信息/内网信息("+strings.Join(rules, ", ")+")——中转在响应里带出了本不该出现的敏感数据")})
	return DetectorResult{Status: "fail", Score: 40, Details: details}
}

const sensitiveLeakPrompt = "Reply with exactly the word OK and nothing else."

// anthropicSensitiveLeak sends a controlled benign probe and fails if the
// response leaks credentials / PII / internal-network data (a genuine upstream
// answering "OK" emits none). Full-mode.
func anthropicSensitiveLeak(ctx context.Context, p *prober, cfg Config) DetectorResult {
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey),
		anthropicPayload(cfg.Model, sensitiveLeakPrompt, 64))
	if res.err != nil || !res.ok() {
		return detectorSkip("sensitive-leak probe could not run")
	}
	return sensitiveLeakVerdict(scanSensitiveData(anthropicText(res.parsed)))
}

// chatSensitiveLeak is the OpenAI/Gemini variant.
func chatSensitiveLeak(ctx context.Context, p *prober, cfg Config) DetectorResult {
	res := p.probeChat(ctx, cfg.Model, sensitiveLeakPrompt, chatSecurityBudget)
	if res.err != nil || !res.ok() {
		return detectorSkip("sensitive-leak probe could not run")
	}
	text := p.chatContent(res)
	if strings.TrimSpace(text) == "" {
		return detectorSkip("empty response (reasoning-exhausted)")
	}
	return sensitiveLeakVerdict(scanSensitiveData(text))
}
