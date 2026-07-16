package detector

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// --- Anthropic structured_output (tool_use schema validation) ---
// Ported from protocols/anthropic/detectors/structured_output.py.
// Forces a tool_use call and validates 5 mandatory sub-checks × 20 = 100;
// an invalid string `caller` is -10.

const structuredToolName = "get_weather"

var validStructuredCallers = map[string]bool{
	"direct":                  true,
	"code_execution_20250825": true,
	"code_execution_20260120": true,
}

func anthropicStructuredOutput(ctx context.Context, p *prober, cfg Config) DetectorResult {
	tool := map[string]interface{}{
		"name":        structuredToolName,
		"description": "Get the current weather for a city.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{
					"type":        "string",
					"description": "The city to look up, e.g. 'Tokyo' or 'San Francisco'.",
				},
				"unit": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"celsius", "fahrenheit"},
					"description": "Temperature unit.",
				},
			},
			"required": []string{"city", "unit"},
		},
	}
	payload := map[string]interface{}{
		"model":       cfg.Model,
		"max_tokens":  200,
		"temperature": 0,
		"tools":       []interface{}{tool},
		"tool_choice": map[string]interface{}{"type": "any"},
		"messages": []map[string]interface{}{
			{"role": "user", "content": "What's the current weather in Tokyo? Use celsius."},
		},
	}
	res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), payload)
	details := map[string]interface{}{"status_code": res.statusCode}
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(), Details: details}
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		details["error"] = msg
		return DetectorResult{Status: "error", Score: 0, Error: msg, Details: details}
	}

	content := subSlice(res.parsed, "content")
	var toolBlocks []map[string]interface{}
	blockTypes := make([]interface{}, 0, len(content))
	for _, raw := range content {
		b, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		blockTypes = append(blockTypes, strField(b, "type"))
		if strField(b, "type") == "tool_use" {
			toolBlocks = append(toolBlocks, b)
		}
	}

	sub := map[string]interface{}{}
	score := 0.0

	hasBlock := len(toolBlocks) > 0
	sub["has_tool_use_block"] = map[string]interface{}{"value": hasBlock, "pass": hasBlock}
	if hasBlock {
		score += 20.0
	}
	if !hasBlock {
		// Record why the model didn't call the tool — the text is often the
		// smoking gun (e.g. a relay's system prompt refusing tool calls).
		var textExcerpt strings.Builder
		for _, raw := range content {
			if b, ok := raw.(map[string]interface{}); ok && strField(b, "type") == "text" {
				textExcerpt.WriteString(strField(b, "text"))
			}
		}
		details["sub_checks"] = sub
		details["content_block_types"] = blockTypes
		details["stop_reason"] = strField(res.parsed, "stop_reason")
		details["text_response"] = truncate(textExcerpt.String(), 400)
		return DetectorResult{Status: "fail", Score: score, Details: details}
	}

	tool0 := toolBlocks[0]

	bid := strField(tool0, "id")
	okID := strings.HasPrefix(bid, "toolu_")
	sub["id_prefix"] = map[string]interface{}{"value": bid, "pass": okID}
	if okID {
		score += 20.0
	}

	name := strField(tool0, "name")
	okName := name == structuredToolName
	sub["name"] = map[string]interface{}{"value": name, "pass": okName}
	if okName {
		score += 20.0
	}

	inp := subMap(tool0, "input")
	city := strField(inp, "city")
	unit := strField(inp, "unit")
	okInput := inp != nil && city != "" && (unit == "celsius" || unit == "fahrenheit")
	sub["input_schema"] = map[string]interface{}{"value": inp, "pass": okInput}
	if okInput {
		score += 20.0
	}

	sr := strField(res.parsed, "stop_reason")
	okStop := sr == "tool_use"
	sub["stop_reason"] = map[string]interface{}{"value": sr, "pass": okStop}
	if okStop {
		score += 20.0
	}

	// Optional `caller`: only a string not in the known enum is penalized;
	// a non-string (Anthropic API drift) is recorded without penalty.
	if caller, present := tool0["caller"]; present {
		if s, isStr := caller.(string); isStr {
			isValid := validStructuredCallers[s]
			sub["caller"] = map[string]interface{}{"value": s, "pass": isValid}
			if !isValid {
				score = clampScore(score - 10.0)
			}
		} else {
			sub["caller"] = map[string]interface{}{
				"value": fmt.Sprintf("<%T>", caller),
				"pass":  true,
				"note":  "non-string caller (Anthropic API drift) — recorded, not penalized",
			}
		}
	}

	details["sub_checks"] = sub
	details["content_block_types"] = blockTypes
	details["stop_reason"] = sr

	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

// --- OpenAI / Gemini structured_output (response_format json_schema strict) ---
// Ported from protocols/{openai,gemini}/detectors/structured_output.py.

var fencedJSONRe = regexp.MustCompile(`(?i)` + "```" + `(?:json)?\s*\{`)

func looksLikeMarkdownJSON(text string) bool {
	return fencedJSONRe.MatchString(text)
}

func openaiFinishReason(resp map[string]interface{}) string {
	choices := subSlice(resp, "choices")
	if len(choices) == 0 {
		return ""
	}
	c0, _ := choices[0].(map[string]interface{})
	return strField(c0, "finish_reason")
}

// structuredOutputJSONSchema runs the OpenAI-compatible json_schema strict
// probe (shared by OpenAI and Gemini) with a protocol-specific nonce and token
// budget.
func structuredOutputJSONSchema(ctx context.Context, p *prober, cfg Config, nonce string, maxTokens int) DetectorResult {
	payload := map[string]interface{}{
		"model":                 cfg.Model,
		"max_completion_tokens": maxTokens,
		"temperature":           0,
		"messages": []map[string]interface{}{
			{"role": "user", "content": fmt.Sprintf(`Return JSON matching the schema with ok=true and nonce="%s".`, nonce)},
		},
		"response_format": map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name":   "detector_result",
				"strict": true,
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ok":    map[string]interface{}{"type": "boolean"},
						"nonce": map[string]interface{}{"type": "string"},
					},
					"required":             []string{"ok", "nonce"},
					"additionalProperties": false,
				},
			},
		},
	}
	res := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), payload)
	details := map[string]interface{}{"status_code": res.statusCode}
	if res.err != nil {
		return DetectorResult{Status: "error", Score: 0, Error: res.err.Error(), Details: details}
	}
	if !res.ok() {
		msg := upstreamErrorText(res)
		details["error"] = msg
		return DetectorResult{Status: "error", Score: 0, Error: msg, Details: details}
	}

	text := openaiContent(res.parsed)
	var parsedAny interface{}
	parseOK := common.UnmarshalJsonStr(text, &parsedAny) == nil
	parsedMap, isMap := parsedAny.(map[string]interface{})
	okJSON := parseOK && isMap
	okSchema := okJSON && parsedMap["ok"] == true && strField(parsedMap, "nonce") == nonce

	finish := openaiFinishReason(res.parsed)
	score := 0.0
	if okJSON {
		score += 40
	}
	if okSchema {
		score += 50
	}
	if finish == "stop" || finish == "" {
		score += 10
	}
	markdownSeen := looksLikeMarkdownJSON(text)

	var evaluation string
	switch {
	case okSchema:
		evaluation = "结构化输出正常: 返回内容是纯 JSON,且字段符合 schema。"
	case markdownSeen:
		evaluation = "请求已发送 response_format=json_schema strict=true,但返回的是普通 Markdown 文本,说明中转站可能没有透传或没有实现 OpenAI 结构化输出参数。"
	default:
		evaluation = "请求已发送 response_format=json_schema strict=true,但返回内容不能按 JSON schema 解析。"
	}

	if isMap {
		details["parsed"] = parsedMap
	} else {
		details["parsed"] = nil
	}
	details["response_text"] = truncate(text, 300)
	details["json_parse"] = okJSON
	details["schema_match"] = okSchema
	details["markdown_json_seen"] = markdownSeen
	details["evaluation_zh"] = evaluation
	details["finish_reason"] = finish

	status := "pass"
	if score < 70 {
		status = "fail"
	}
	return DetectorResult{Status: status, Score: score, Details: details}
}

func openaiStructuredOutput(ctx context.Context, p *prober, cfg Config) DetectorResult {
	return structuredOutputJSONSchema(ctx, p, cfg, "openai-detector", 128)
}

// geminiStructuredOutput lives in gemini.go — it uses the native
// generationConfig.responseSchema, not the OpenAI response_format shim.

// truncate returns s limited to n bytes (rune-safe at the boundary is not
// required here; details are diagnostic excerpts).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
