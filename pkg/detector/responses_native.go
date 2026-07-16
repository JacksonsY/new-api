package detector

// responses_native.go adds detection for OpenAI's Responses API
// (POST {base}/v1/responses) as a first-class protocol, symmetric with the
// Claude / OpenAI-chat / Gemini / Grok surfaces. Chat Completions is already
// native OpenAI, so this is NOT a compat-shim fix (as Gemini was) but a SECOND
// native surface: the Responses API is the modern gpt-5.x / o-series wire, and
// it natively exposes fingerprints the chat surface hides — the resp_ id form,
// the output[] item stream (reasoning + message items), and
// usage.output_tokens_details.reasoning_tokens. Probing it forces a relay to
// serve the real Responses protocol, and a response leaking chat.completion
// shape while claiming to be a Responses object is swapped-surface evidence.
//
// Shapes mirror this repo's own dto/openai_response.go (OpenAIResponsesResponse
// / ResponsesOutput / Usage).

import (
	"context"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const responsesPath = "/v1/responses"

// responsesBody is a minimal single-turn Responses request. `input` accepts a
// plain string. temperature is omitted (reasoning models reject it; the
// Responses surface does not need it for a deterministic probe).
func responsesBody(model, prompt string, maxTokens int) map[string]interface{} {
	return map[string]interface{}{
		"model":             model,
		"input":             prompt,
		"max_output_tokens": maxTokens,
	}
}

// responsesBodyInstructions adds a system-equivalent `instructions` field (the
// Responses home for a system prompt).
func responsesBodyInstructions(model, instructions, input string, maxTokens int) map[string]interface{} {
	b := responsesBody(model, input, maxTokens)
	b["instructions"] = instructions
	return b
}

// --- native response parsing -----------------------------------------------

// responsesText concatenates the output_text of the message items in output[].
func responsesText(resp map[string]interface{}) string {
	var b strings.Builder
	for _, raw := range subSlice(resp, "output") {
		item, ok := raw.(map[string]interface{})
		if !ok || strField(item, "type") != "message" {
			continue
		}
		for _, craw := range subSlice(item, "content") {
			c, ok := craw.(map[string]interface{})
			if !ok {
				continue
			}
			if strField(c, "type") == "output_text" {
				b.WriteString(strField(c, "text"))
			}
		}
	}
	return b.String()
}

func responsesStatus(resp map[string]interface{}) string {
	return strField(resp, "status")
}

func responsesID(resp map[string]interface{}) string {
	return strField(resp, "id")
}

func responsesModel(resp map[string]interface{}) string {
	return strField(resp, "model")
}

func responsesUsage(resp map[string]interface{}) map[string]interface{} {
	return subMap(resp, "usage")
}

// responsesUsageToOpenAI normalizes Responses usage to the OpenAI chat keys the
// shared content probes read (float64 for JSON-number semantics).
func responsesUsageToOpenAI(u map[string]interface{}) map[string]interface{} {
	if len(u) == 0 {
		return nil
	}
	out := map[string]interface{}{}
	if v, ok := intField(u, "input_tokens"); ok {
		out["prompt_tokens"] = float64(v)
	}
	if v, ok := intField(u, "output_tokens"); ok {
		out["completion_tokens"] = float64(v)
	}
	if v, ok := intField(u, "total_tokens"); ok {
		out["total_tokens"] = float64(v)
	}
	return out
}

// responsesFunctionCall returns the first function_call output item (name +
// arguments string + call_id), or nil.
func responsesFunctionCall(resp map[string]interface{}) map[string]interface{} {
	for _, raw := range subSlice(resp, "output") {
		item, ok := raw.(map[string]interface{})
		if ok && strField(item, "type") == "function_call" {
			return item
		}
	}
	return nil
}

// responsesHasOutputType reports whether any output item is of the given type
// (e.g. "reasoning", "message").
func responsesHasOutputType(resp map[string]interface{}, typ string) bool {
	for _, raw := range subSlice(resp, "output") {
		if item, ok := raw.(map[string]interface{}); ok && strField(item, "type") == typ {
			return true
		}
	}
	return false
}

// --- responses detectors ---------------------------------------------------

// responsesValidStatus is the Responses object's terminal status enum ("" is
// tolerated for a streamed intermediate).
var responsesValidStatus = map[string]bool{
	"": true, "completed": true, "incomplete": true, "in_progress": true, "failed": true,
}

// responsesProtocol probes the native Responses endpoint and validates the
// envelope. It SKIPS when the relay does not expose /v1/responses (error /
// non-2xx — many relays implement only Chat Completions, which is not fraud),
// and otherwise scores the native shape: resp_ id (25), object=response (20),
// output[] present (15), a message or reasoning-exhausted-but-well-formed output
// (15), input/output token usage (25). A response leaking Chat-Completions shape
// (choices / object=chat.completion) on the Responses endpoint is not a native
// Responses implementation → major, capped fail. pass≥70.
func responsesProtocol(ctx context.Context, p *prober, cfg Config) DetectorResult {
	// Reasoning models burn output budget before the visible text; 256 leaves
	// room for a short "pong" after hidden reasoning.
	res := p.postJSONUnobserved(ctx, responsesPath, openaiHeaders(p.apiKey),
		responsesBody(cfg.Model, "Reply with exactly: pong", 256))
	if res.err != nil {
		return detectorSkip("Responses API probe could not run: " + res.err.Error())
	}
	if !res.ok() {
		return detectorSkip("relay does not expose the Responses API (status " + strconv.Itoa(res.statusCode) + ")")
	}

	payload := res.parsed
	id := responsesID(payload)
	object := strField(payload, "object")
	status := responsesStatus(payload)
	text := responsesText(payload)
	usage := responsesUsage(payload)
	_, hasIn := intField(usage, "input_tokens")
	_, hasOut := intField(usage, "output_tokens")
	outputs := subSlice(payload, "output")
	hasReasoning := responsesHasOutputType(payload, "reasoning")

	details := map[string]interface{}{
		"response_id":    id,
		"object":         object,
		"status":         status,
		"has_reasoning":  hasReasoning,
		"output_items":   len(outputs),
		"response_text":  truncate(text, 300),
		"model_returned": responsesModel(payload),
	}

	// Chat-Completions shape on the Responses endpoint = not a native Responses
	// implementation (a translator, or a backend that cannot serve Responses).
	if _, leaksChoices := payload["choices"]; leaksChoices || object == "chat.completion" {
		attachIssues(details, []map[string]interface{}{newIssue(sevMajor, "responses_not_native",
			"Responses 端点返回的是 Chat Completions 形状(choices / object=chat.completion)——中转没有原生实现 Responses API,只是把 chat 结果转译过来")})
		details["summary"] = "端点存在但非原生 Responses 实现(返回 chat 形状)"
		return DetectorResult{Status: "fail", Score: 30, Details: details}
	}

	score := 0.0
	if strings.HasPrefix(id, "resp_") {
		score += 25
	}
	if object == "response" {
		score += 20
	}
	if len(outputs) > 0 {
		score += 15
	}
	// A visible message OR a well-formed reasoning-exhausted response (valid
	// envelope, reasoning item, no text yet) both count as a conformant output.
	if text != "" || hasReasoning {
		score += 15
	}
	if hasIn && hasOut {
		score += 25
	}
	if !responsesValidStatus[status] {
		details["status_unexpected"] = true
	}

	// Surface the resolved preference so the report shows whether the shared
	// behavioral/identity/security probes ran against Responses or fell back to
	// Chat Completions.
	if p.openaiSurface == surfaceResponses {
		details["preferred_surface"] = "responses"
		details["summary"] = "原生 Responses API 可用且已设为首选面: object=" + object + " · id 前缀 resp_ · 行为/身份/安全探针优先走 /v1/responses"
	} else {
		details["preferred_surface"] = p.openaiSurface
		details["summary"] = "原生 Responses API 可用: object=" + object + " · id 前缀 resp_ · input/output tokens"
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: details}
}

// responsesFunctionCalling forces a tool call on the Responses surface and scores
// the native function_call output item (has_call 40 / name 30 / arguments JSON
// 30). Skips when the endpoint is absent. The Responses tool schema is flat
// (name/description/parameters at the tool level, not nested under "function"),
// and the call surfaces as an output[] item of type function_call — a distinct
// native shape from Chat Completions' message.tool_calls.
func responsesFunctionCalling(ctx context.Context, p *prober, cfg Config) DetectorResult {
	const toolName = "get_current_weather"
	body := responsesBody(cfg.Model, "Use get_current_weather for Boston, MA in celsius. Do not answer directly.", 256)
	body["tools"] = []map[string]interface{}{{
		"type": "function", "name": toolName, "description": "Get current weather for a city.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"city": map[string]interface{}{"type": "string"},
				"unit": map[string]interface{}{"type": "string", "enum": []string{"celsius", "fahrenheit"}},
			},
			"required": []string{"city", "unit"},
		},
	}}
	body["tool_choice"] = "required"

	res := p.postJSONUnobserved(ctx, responsesPath, openaiHeaders(p.apiKey), body)
	if res.err != nil {
		return detectorSkip("Responses function-calling probe could not run: " + res.err.Error())
	}
	if !res.ok() {
		return detectorSkip("relay does not expose the Responses API (status " + strconv.Itoa(res.statusCode) + ")")
	}

	sub := map[string]interface{}{}
	score := 0.0
	call := responsesFunctionCall(res.parsed)
	hasCall := call != nil
	sub["has_tool_call"] = map[string]interface{}{"pass": hasCall}
	if !hasCall {
		return DetectorResult{Status: "fail", Score: 0, Details: map[string]interface{}{
			"sub_checks": sub, "status": responsesStatus(res.parsed),
		}}
	}
	score += 40
	nameOK := strField(call, "name") == toolName
	sub["name"] = map[string]interface{}{"value": strField(call, "name"), "pass": nameOK}
	if nameOK {
		score += 30
	}
	argsOK := false
	if args := strField(call, "arguments"); args != "" {
		var parsed map[string]interface{}
		if common.UnmarshalJsonStr(args, &parsed) == nil {
			_, cityStr := parsed["city"].(string)
			unit, _ := parsed["unit"].(string)
			argsOK = cityStr && (unit == "celsius" || unit == "fahrenheit")
		}
	}
	sub["arguments_json"] = map[string]interface{}{"pass": argsOK}
	if argsOK {
		score += 30
	}
	return DetectorResult{Status: passFail(score, 70), Score: score, Details: map[string]interface{}{
		"sub_checks": sub, "status": responsesStatus(res.parsed),
	}}
}
