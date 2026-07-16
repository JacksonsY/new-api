package detector

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// geminiUsageFingerprintKeys are Gemini-native usage keys (non-gemini_* forms).
var geminiUsageFingerprintKeys = []string{
	"cached_content_token_count",
	"candidates_token_count",
	"prompt_token_count",
	"thoughts_token_count",
	"tool_use_prompt_token_count",
}

// mixedTokenFieldKeys are Responses/Anthropic-shaped token fields; in a
// chat.completion usage they are a mixed-shape adapter fingerprint (MINOR, not
// critical). cache_creation_input_tokens is intentionally NOT flagged.
var mixedTokenFieldKeys = []string{"input_tokens", "output_tokens", "input_tokens_details"}

// usageFingerprints holds the non-OpenAI residue found in an OpenAI usage
// object, grouped by category so each maps to its own protocol issue (as the
// reference emits separate criticals, not one merged issue).
type usageFingerprints struct {
	claude []string // claude_* keys (Anthropic backend)
	gemini []string // gemini_* / *_token_count keys (Gemini backend)
	source string   // usage_source value when non-empty and != "openai"
	minor  []string // mixed input/output token fields (mixed-shape adapter)
}

func (f usageFingerprints) hasCritical() bool {
	return len(f.claude) > 0 || len(f.gemini) > 0 || f.source != ""
}

// scanOpenAIUsageFingerprints classifies non-OpenAI residue in an OpenAI usage
// object per protocol_templates._check_usage_adapter_fingerprints: critical =
// swapped-core (claude_* keys, gemini_* / *_token_count keys, usage_source
// non-empty and != "openai"); minor = mixed input/output token fields.
func scanOpenAIUsageFingerprints(usage map[string]interface{}) usageFingerprints {
	var fp usageFingerprints
	if usage == nil {
		return fp
	}
	for k := range usage {
		if strings.HasPrefix(k, "claude_") {
			fp.claude = append(fp.claude, k)
		}
		if strings.HasPrefix(k, "gemini_") {
			fp.gemini = append(fp.gemini, k)
		}
	}
	for _, k := range geminiUsageFingerprintKeys {
		if _, ok := usage[k]; ok {
			fp.gemini = append(fp.gemini, k)
		}
	}
	if s := strings.ToLower(asString(usage["usage_source"])); s != "" && s != "openai" {
		fp.source = s
	}
	for _, k := range mixedTokenFieldKeys {
		if _, ok := usage[k]; ok {
			fp.minor = append(fp.minor, k)
		}
	}
	sort.Strings(fp.claude)
	sort.Strings(fp.gemini)
	sort.Strings(fp.minor)
	return fp
}

// protoIssue is one protocol-template validation finding.
type protoIssue struct {
	severity string // "critical" | "major" | "minor"
	code     string
	message  string
}

var openaiValidFinishReasons = map[string]bool{
	"stop": true, "length": true, "tool_calls": true,
	"content_filter": true, "function_call": true, "": true,
}

var openaiChatRequiredTopLevel = []string{"id", "object", "created", "model", "choices", "usage"}

// openaiIDPrefixes are the recognized id forms of a modern OpenAI-compatible
// Chat Completions response: "chatcmpl-"/"chatcmpl_" (classic) and "resp_"
// (Responses-API era — gpt-5.x returns these even when object is chat.completion).
// Deviation from the reference (which hard-required "chatcmpl-"): on gpt-5.x a
// genuine relay legitimately returns a "resp_" id, so an unrecognized prefix is a
// MINOR conformance note, not a verdict-vetoing critical. Actual swapped-core is
// caught by the usage adapter fingerprints (claude_*/gemini_*/usage_source),
// which stay critical — mirroring the existing mixedTokenFieldKeys precedent.
var openaiIDPrefixes = []string{"chatcmpl-", "chatcmpl_", "resp_"}

func hasOpenAIIDPrefix(id string) bool {
	for _, p := range openaiIDPrefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// reUUIDv4ish matches xAI's Chat Completions response-id form (a plain UUID,
// e.g. "0daf962f-a275-4a3c-839a-047854645532" — docs.x.ai). For a grok target
// this is the GENUINE id shape, so it must not be flagged even as minor.
var reUUIDv4ish = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validateChatCompletion ports protocol_templates.validate_chat_completion: a
// structural validation of a Chat Completions envelope. Returns the 0..100 score
// (100 − Σ severity penalties, crit 35 / major 15 / minor 5) and the issues.
func validateChatCompletion(payload map[string]interface{}, requestModel string) (float64, []protoIssue) {
	var issues []protoIssue
	add := func(sev, code, msg string) { issues = append(issues, protoIssue{sev, code, msg}) }

	for _, key := range openaiChatRequiredTopLevel {
		if _, ok := payload[key]; !ok {
			add("critical", "top_level_missing", "missing required top-level field "+key)
		}
	}
	if id := strField(payload, "id"); !hasOpenAIIDPrefix(id) {
		if !(grokModel(requestModel) && reUUIDv4ish.MatchString(id)) {
			add("minor", "id_prefix_unrecognized", "id prefix is not a recognized OpenAI form (chatcmpl- / resp_)")
		}
	}
	if strField(payload, "object") != "chat.completion" {
		add("critical", "object_invalid", "object is not chat.completion")
	}
	if !isNonNegInt(payload["created"]) {
		add("major", "nonneg_int_invalid", "created must be a non-negative integer")
	}
	// model: non-empty string; match against request (normalized, avoids false
	// dotted-vs-hyphen mismatch — deviation from the reference's raw prefix).
	respModel := strField(payload, "model")
	if respModel == "" {
		add("critical", "model_missing_or_not_string", "model must be a non-empty string")
	} else if requestModel != "" && !modelMatches(requestModel, respModel) {
		add("major", "model_mismatch", "response.model does not match the requested model")
	}

	validateChatUsage(payload["usage"], &issues)

	if fp, ok := payload["system_fingerprint"]; ok && fp != nil {
		if s, isStr := fp.(string); !isStr || !strings.HasPrefix(s, "fp_") {
			add("minor", "system_fingerprint_invalid", "system_fingerprint should use the fp_ prefix")
		}
	}

	choices, ok := payload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		add("critical", "choices_missing_or_empty", "response must include at least one choice")
	} else {
		for _, raw := range choices {
			validateChatChoice(raw, &issues)
		}
	}

	return protoScore(issues), issues
}

func validateChatUsage(raw interface{}, issues *[]protoIssue) {
	add := func(sev, code, msg string) { *issues = append(*issues, protoIssue{sev, code, msg}) }
	usage, ok := raw.(map[string]interface{})
	if !ok {
		add("critical", "usage_missing_or_not_object", "usage must be an object with token counters")
		return
	}
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		if !isNonNegInt(usage[key]) {
			add("major", "nonneg_int_invalid", "usage."+key+" must be a non-negative integer")
		}
	}
	p, okP := intField(usage, "prompt_tokens")
	c, okC := intField(usage, "completion_tokens")
	tot, okT := intField(usage, "total_tokens")
	if okP && okC && okT && tot != p+c {
		add("minor", "usage_total_mismatch", "total_tokens should equal prompt + completion tokens")
	}
	// adapter fingerprints (swapped core / mixed shape). Each category is a
	// separate critical (matching protocol_templates: usage_contains_claude_fields
	// / usage_contains_gemini_fields / usage_source_non_openai), so a response
	// leaking several foreign markers is penalized per category, not once.
	fp := scanOpenAIUsageFingerprints(usage)
	if len(fp.claude) > 0 {
		add("critical", "usage_contains_claude_fields", "usage contains Anthropic backend fields: "+strings.Join(fp.claude, ", "))
	}
	if len(fp.gemini) > 0 {
		add("critical", "usage_contains_gemini_fields", "usage contains Gemini backend fields: "+strings.Join(fp.gemini, ", "))
	}
	if fp.source != "" {
		add("critical", "usage_source_non_openai", "usage_source is not openai: "+fp.source)
	}
	if len(fp.minor) > 0 {
		add("minor", "usage_mixed_token_fields", "usage mixes OpenAI fields with input/output token fields: "+strings.Join(fp.minor, ", "))
	}
}

func validateChatChoice(raw interface{}, issues *[]protoIssue) {
	add := func(sev, code, msg string) { *issues = append(*issues, protoIssue{sev, code, msg}) }
	choice, ok := raw.(map[string]interface{})
	if !ok {
		add("critical", "choice_not_object", "each choice must be an object")
		return
	}
	if !isNonNegInt(choice["index"]) {
		add("major", "nonneg_int_invalid", "choice.index must be a non-negative integer")
	}
	if !openaiValidFinishReasons[strField(choice, "finish_reason")] {
		add("major", "finish_reason_invalid", "finish_reason must be an official enum value")
	}
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		add("critical", "message_missing_or_not_object", "choice.message must be an object")
		return
	}
	if strField(message, "role") != "assistant" {
		add("major", "message_role_invalid", "message.role should be assistant")
	}
	if content, ok := message["content"]; ok && content != nil {
		switch content.(type) {
		case string, []interface{}:
		default:
			add("major", "message_content_invalid", "message.content must be string, array, or null")
		}
	}
	if refusal, ok := message["refusal"]; ok && refusal != nil {
		if _, isStr := refusal.(string); !isStr {
			add("minor", "message_refusal_invalid", "message.refusal must be a string when present")
		}
	}
	if tc, present := message["tool_calls"]; present && tc != nil {
		calls, ok := tc.([]interface{})
		if !ok {
			add("critical", "tool_calls_not_array", "message.tool_calls must be an array")
		} else {
			for _, c := range calls {
				validateChatToolCall(c, issues)
			}
			if fr := strField(choice, "finish_reason"); fr != "tool_calls" && fr != "" {
				add("minor", "tool_calls_finish_reason_mismatch", "finish_reason should be tool_calls when tool_calls are returned")
			}
		}
	}
}

func validateChatToolCall(raw interface{}, issues *[]protoIssue) {
	add := func(sev, code, msg string) { *issues = append(*issues, protoIssue{sev, code, msg}) }
	tc, ok := raw.(map[string]interface{})
	if !ok {
		add("critical", "tool_call_not_object", "tool call must be an object")
		return
	}
	if !strings.HasPrefix(strField(tc, "id"), "call_") {
		add("critical", "id_prefix_invalid", "tool_call id must use the call_ prefix")
	}
	if strField(tc, "type") != "function" {
		add("major", "tool_call_type_invalid", "tool call type should be function")
	}
	fn, ok := tc["function"].(map[string]interface{})
	if !ok {
		add("critical", "tool_call_function_missing", "tool call must include a function object")
		return
	}
	if strField(fn, "name") == "" {
		add("major", "tool_call_function_name_invalid", "tool call function name must be a non-empty string")
	}
	args, isStr := fn["arguments"].(string)
	if !isStr {
		add("major", "tool_call_arguments_not_string", "tool call arguments must be a JSON string")
	} else {
		var parsed interface{}
		if common.UnmarshalJsonStr(args, &parsed) != nil {
			add("major", "json_arguments_invalid", "tool call arguments must parse as JSON")
		} else if _, isObj := parsed.(map[string]interface{}); !isObj {
			add("major", "json_arguments_not_object", "tool call arguments should be a JSON object")
		}
	}
}

// protoScore applies the reference penalty model (crit 35 / major 15 / minor 5).
func protoScore(issues []protoIssue) float64 {
	penalty := 0.0
	for _, i := range issues {
		switch i.severity {
		case "critical":
			penalty += 35
		case "major":
			penalty += 15
		default:
			penalty += 5
		}
	}
	return clampScore(100 - penalty)
}

// openaiProtocol (passive) validates every observed Chat Completions response
// against the wire template and averages the scores. passed = avg>=80 AND no
// critical issue across the run.
func openaiProtocol(_ context.Context, p *prober, cfg Config) DetectorResult {
	obs := p.tel.snapshot()
	if len(obs) == 0 {
		return detectorSkip("no-observations")
	}
	total := 0.0
	critCount, majorCount := 0, 0
	var issueList []map[string]interface{}
	for _, o := range obs {
		score, issues := validateChatCompletion(o.response, cfg.Model)
		total += score
		for _, is := range issues {
			switch is.severity {
			case "critical":
				critCount++
			case "major":
				majorCount++
			}
			if len(issueList) < 30 {
				issueList = append(issueList, map[string]interface{}{"severity": is.severity, "code": is.code, "message": is.message})
			}
		}
	}
	avg := total / float64(len(obs))
	details := map[string]interface{}{
		"observation_count":    len(obs),
		"critical_issue_count": critCount,
		"major_issue_count":    majorCount,
		"issues":               issueList,
	}
	status := "fail"
	if avg >= 80 && critCount == 0 {
		status = "pass"
	}
	return DetectorResult{Status: status, Score: avg, Details: details}
}
