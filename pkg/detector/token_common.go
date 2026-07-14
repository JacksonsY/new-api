package detector

import (
	"math"
	"strings"
)

// Shared constants/helpers for the token sanity detectors, ported from the
// OpenAI/Gemini token_billing.py / token_usage.py sources.

const (
	tokenShortPrompt   = "Reply with exactly: ok"
	tokenLongExtraWord = " apple"
	tokenLongRepeats   = 80
)

// tokenLongPrompt is the short prompt plus a fixed block of simple English used
// to measure the prompt-token delta.
func tokenLongPrompt() string {
	return tokenShortPrompt + "\n\nReference text:" + strings.Repeat(tokenLongExtraWord, tokenLongRepeats)
}

// openaiChatPayloadMC builds a Chat Completions payload using
// max_completion_tokens and temperature 0 (the shape the token detectors use).
func openaiChatPayloadMC(model, prompt string, maxCompletionTokens int) map[string]interface{} {
	return map[string]interface{}{
		"model":                 model,
		"max_completion_tokens": maxCompletionTokens,
		"temperature":           0,
		"messages": []map[string]interface{}{
			{"role": "user", "content": prompt},
		},
	}
}

// tokenInt mirrors Python's `isinstance(v, int) and not isinstance(v, bool)`:
// JSON numbers decode to float64, so a value counts as an int only if it is a
// whole, finite number (JSON booleans decode to Go bool, so they are excluded).
func tokenInt(v interface{}) (int, bool) {
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}

func tokenField(usage map[string]interface{}, key string) (int, bool) {
	if usage == nil {
		return 0, false
	}
	return tokenInt(usage[key])
}

// intOrNil returns v when ok, else nil — for recording Python-style None in
// diagnostic details.
func intOrNil(v int, ok bool) interface{} {
	if ok {
		return v
	}
	return nil
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// streamUsageFromSSE returns the last usage object present in an OpenAI/Gemini
// SSE body (emitted when stream_options.include_usage is honored) and the
// number of parsed data chunks.
func streamUsageFromSSE(raw string) (map[string]interface{}, int) {
	objs := sseDataObjects(raw)
	var usage map[string]interface{}
	for _, obj := range objs {
		if u, ok := obj["usage"].(map[string]interface{}); ok {
			usage = u
		}
	}
	return usage, len(objs)
}

// countPassedSubChecks counts sub-check entries whose "pass" flag is true.
func countPassedSubChecks(sub map[string]interface{}) int {
	n := 0
	for _, v := range sub {
		if m, ok := v.(map[string]interface{}); ok {
			if pass, _ := m["pass"].(bool); pass {
				n++
			}
		}
	}
	return n
}
