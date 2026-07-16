package detector

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScoreHeaderFingerprint(t *testing.T) {
	mk := func(kv map[string]string) http.Header {
		h := http.Header{}
		for k, v := range kv {
			h.Set(k, v)
		}
		return h
	}
	// request-id + json → 2 markers (pass threshold).
	hits, _ := scoreHeaderFingerprint(mk(map[string]string{
		"request-id": "req_x", "content-type": "application/json; charset=utf-8",
	}))
	assert.Equal(t, 2, hits)
	// all four official-infra markers.
	hits, present := scoreHeaderFingerprint(mk(map[string]string{
		"request-id": "a", "x-request-id": "b", "content-type": "application/json", "cf-ray": "c",
	}))
	assert.Equal(t, 4, hits)
	assert.Len(t, present, 4)
	// only content-type → 1 (reverse proxy stripped the rest).
	hits, _ = scoreHeaderFingerprint(mk(map[string]string{"content-type": "application/json"}))
	assert.Equal(t, 1, hits)
	// nothing.
	hits, _ = scoreHeaderFingerprint(http.Header{})
	assert.Equal(t, 0, hits)
}

func TestIsAnthropicErrorEnvelope(t *testing.T) {
	valid := map[string]interface{}{
		"type":  "error",
		"error": map[string]interface{}{"type": "invalid_request_error", "message": "messages: at least one message is required"},
	}
	assert.True(t, isAnthropicErrorEnvelope(valid))

	// Missing message.
	assert.False(t, isAnthropicErrorEnvelope(map[string]interface{}{
		"type": "error", "error": map[string]interface{}{"type": "x"},
	}))
	// Wrong top-level type (a relay that 4xx's with a non-Anthropic body).
	assert.False(t, isAnthropicErrorEnvelope(map[string]interface{}{"type": "message"}))
	// No error object.
	assert.False(t, isAnthropicErrorEnvelope(map[string]interface{}{"type": "error"}))
	assert.False(t, isAnthropicErrorEnvelope(nil))
}
