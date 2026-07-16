package detector

// gemini_native.go moves Gemini detection onto Google's NATIVE generateContent
// protocol (POST {base}/v1beta/models/{model}:generateContent with the
// x-goog-api-key header), instead of the OpenAI-compatibility surface the port
// originally reused. For an authenticity detector this is the whole point: the
// compat shim normalizes every Gemini-native tell — the candidates[].content.
// parts[] envelope, usageMetadata field shapes (promptTokenCount /
// candidatesTokenCount / thoughtsTokenCount), modelVersion, native finishReason
// enum, safetyRatings, thoughtSignature — into OpenAI shape, erasing exactly the
// swapped-core fingerprints the detector exists to catch. Probing native forces
// the relay to actually serve Google's protocol, and a response leaking
// OpenAI-shape fields while claiming Gemini becomes hard substitution evidence.
//
// Request/response shapes mirror this repo's own Gemini adaptor (dto/gemini.go,
// relay/channel/gemini) so the probes speak the exact wire the gateway forwards.

import (
	"context"
	"strings"
)

// --- native wire helpers ---------------------------------------------------

func geminiNativeHeaders(apiKey string) map[string]string {
	// Native Gemini authenticates with x-goog-api-key (query ?key= is the
	// alternative); Bearer is an OpenAI-compat-only convention.
	return map[string]string{"x-goog-api-key": apiKey}
}

// geminiNativePath returns the generateContent path suffix; buildURL appends it
// to the base with /v1beta de-duplication.
func geminiNativePath(model string, stream bool) string {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}
	return "/v1beta/models/" + model + ":" + action
}

func geminiUserContents(prompt string) []map[string]interface{} {
	return []map[string]interface{}{
		{"role": "user", "parts": []map[string]interface{}{{"text": prompt}}},
	}
}

// geminiNativeBody is a minimal single-user-turn generateContent request.
func geminiNativeBody(prompt string, maxTokens int) map[string]interface{} {
	return map[string]interface{}{
		"contents":         geminiUserContents(prompt),
		"generationConfig": map[string]interface{}{"maxOutputTokens": maxTokens, "temperature": 0},
	}
}

// geminiNativeBodySystem adds a systemInstruction (native home for a system
// prompt; the compat surface's system message has no native equivalent).
func geminiNativeBodySystem(system, user string, maxTokens int) map[string]interface{} {
	b := geminiNativeBody(user, maxTokens)
	b["systemInstruction"] = map[string]interface{}{"parts": []map[string]interface{}{{"text": system}}}
	return b
}

// --- native response parsing -----------------------------------------------

func geminiFirstCandidate(resp map[string]interface{}) map[string]interface{} {
	cs := subSlice(resp, "candidates")
	if len(cs) == 0 {
		return nil
	}
	c0, _ := cs[0].(map[string]interface{})
	return c0
}

// geminiNativeText concatenates the visible (non-thought) text parts of the
// first candidate.
func geminiNativeText(resp map[string]interface{}) string {
	content := subMap(geminiFirstCandidate(resp), "content")
	var b strings.Builder
	for _, raw := range subSlice(content, "parts") {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := part["thought"].(bool); t {
			continue // reasoning part, not visible output
		}
		b.WriteString(strField(part, "text"))
	}
	return b.String()
}

func geminiNativeFinish(resp map[string]interface{}) string {
	return strField(geminiFirstCandidate(resp), "finishReason")
}

func geminiNativeUsage(resp map[string]interface{}) map[string]interface{} {
	return subMap(resp, "usageMetadata")
}

func geminiNativeModelVersion(resp map[string]interface{}) string {
	return strField(resp, "modelVersion")
}

func geminiNativeResponseID(resp map[string]interface{}) string {
	return strField(resp, "responseId")
}

// geminiFunctionCallPart returns the first functionCall part of the first
// candidate (name + args), or nil.
func geminiFunctionCallPart(resp map[string]interface{}) map[string]interface{} {
	content := subMap(geminiFirstCandidate(resp), "content")
	for _, raw := range subSlice(content, "parts") {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if fc := subMap(part, "functionCall"); fc != nil {
			return fc
		}
	}
	return nil
}

// geminiUsageToOpenAI normalizes native usageMetadata to the OpenAI usage keys
// the shared content probes read, storing float64 so intField (JSON-number
// semantics) accepts them. completion_tokens folds candidates + thoughts (both
// are output the model generated) so the hidden-prompt / arithmetic checks see
// the full output count.
func geminiUsageToOpenAI(u map[string]interface{}) map[string]interface{} {
	if len(u) == 0 {
		return nil
	}
	out := map[string]interface{}{}
	if v, ok := intField(u, "promptTokenCount"); ok {
		out["prompt_tokens"] = float64(v)
	}
	if cand, ok := intField(u, "candidatesTokenCount"); ok {
		thoughts, _ := intField(u, "thoughtsTokenCount")
		out["completion_tokens"] = float64(cand + thoughts)
	}
	if v, ok := intField(u, "totalTokenCount"); ok {
		out["total_tokens"] = float64(v)
	}
	return out
}

// --- native streaming ------------------------------------------------------

// geminiFirstToken reports whether a streamed native chunk carries visible text
// (the TTFT trigger for the speed benchmark).
func geminiFirstToken(o map[string]interface{}) bool {
	return geminiNativeText(o) != ""
}

// geminiStreamText concatenates visible text across native SSE chunks.
func geminiStreamText(objs []map[string]interface{}) string {
	var b strings.Builder
	for _, o := range objs {
		b.WriteString(geminiNativeText(o))
	}
	return b.String()
}

// geminiStreamUsage returns the last usageMetadata seen across native SSE chunks
// (the final chunk carries the totals).
func geminiStreamUsage(objs []map[string]interface{}) map[string]interface{} {
	var last map[string]interface{}
	for _, o := range objs {
		if u := geminiNativeUsage(o); len(u) > 0 {
			last = u
		}
	}
	return last
}

func geminiStreamFinish(objs []map[string]interface{}) string {
	finish := ""
	for _, o := range objs {
		if f := geminiNativeFinish(o); f != "" {
			finish = f
		}
	}
	return finish
}

// geminiStreamOutputTokens reports the streamed output token count (candidates +
// thoughts) from the final usageMetadata, falling back to a word count.
func geminiStreamOutputTokens(body string) int {
	objs := sseDataObjects(body)
	u := geminiStreamUsage(objs)
	cand, ok := intField(u, "candidatesTokenCount")
	if ok && cand > 0 {
		thoughts, _ := intField(u, "thoughtsTokenCount")
		return cand + thoughts
	}
	return len(strings.Fields(geminiStreamText(objs)))
}

// --- protocol-aware dispatch (used by shared chat probes) ------------------

// probeChat issues a single-user-turn chat request on the active protocol's
// preferred surface: Gemini → native generateContent; openai/grok → the
// Responses API when the relay serves it (resolveOpenAISurface), else Chat
// Completions (byte-identical to the pre-Responses behavior). Responses probes
// are UNOBSERVED so their object=response / output[] shape does not pollute the
// chat-shape passive protocol validator (responsesProtocol validates Responses
// separately); the chat surface stays the observed one.
func (p *prober) probeChat(ctx context.Context, model, prompt string, maxTokens int) httpResult {
	switch {
	case p.protocol == ProtocolGemini:
		return p.postJSON(ctx, geminiNativePath(model, false), geminiNativeHeaders(p.apiKey), geminiNativeBody(prompt, maxTokens))
	case p.openaiSurface == surfaceResponses:
		return p.postJSONUnobserved(ctx, responsesPath, openaiHeaders(p.apiKey), responsesBody(model, prompt, maxTokens))
	default:
		return p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), openaiPayload(model, prompt, maxTokens))
	}
}

// probeChatSystem issues a system+user chat request on the active protocol's
// preferred surface.
func (p *prober) probeChatSystem(ctx context.Context, model, system, user string, maxTokens int) httpResult {
	switch {
	case p.protocol == ProtocolGemini:
		return p.postJSON(ctx, geminiNativePath(model, false), geminiNativeHeaders(p.apiKey), geminiNativeBodySystem(system, user, maxTokens))
	case p.openaiSurface == surfaceResponses:
		return p.postJSONUnobserved(ctx, responsesPath, openaiHeaders(p.apiKey), responsesBodyInstructions(model, system, user, maxTokens))
	default:
		body := map[string]interface{}{
			"model": model, "max_completion_tokens": maxTokens, "temperature": 0,
			"messages": []map[string]interface{}{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
		}
		return p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), body)
	}
}

// chatContent extracts assistant text from a probeChat response, parsing by the
// surface that probeChat used.
func (p *prober) chatContent(res httpResult) string {
	switch {
	case p.protocol == ProtocolGemini:
		return geminiNativeText(res.parsed)
	case p.openaiSurface == surfaceResponses:
		return responsesText(res.parsed)
	default:
		return openaiContent(res.parsed)
	}
}

// chatUsageOpenAI returns usage normalized to the OpenAI keys (prompt_tokens /
// completion_tokens / total_tokens), parsing by the surface probeChat used.
func (p *prober) chatUsageOpenAI(res httpResult) map[string]interface{} {
	switch {
	case p.protocol == ProtocolGemini:
		return geminiUsageToOpenAI(geminiNativeUsage(res.parsed))
	case p.openaiSurface == surfaceResponses:
		return responsesUsageToOpenAI(responsesUsage(res.parsed))
	default:
		return openaiUsage(res.parsed)
	}
}
