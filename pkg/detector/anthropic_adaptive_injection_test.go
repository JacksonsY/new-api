package detector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEchoedVerbatim(t *testing.T) {
	// Exact echo.
	assert.True(t, echoedVerbatim(adaptiveNeutralPayload, adaptiveNeutralPayload))
	// Tolerant of surrounding prose/punctuation (compactForLeakCheck).
	assert.True(t, echoedVerbatim("Sure! apple banana cherry date elderberry.", adaptiveNeutralPayload))
	// The sensitive-token payload echoed verbatim is clean.
	assert.True(t, echoedVerbatim("apple banana aws_secret_access_key date elderberry", adaptiveTriggerPayload))
	// A refusal / non-echo is not verbatim.
	assert.False(t, echoedVerbatim("I can't help with that.", adaptiveNeutralPayload))
	// Neutral payload does not satisfy the trigger payload (they differ by the
	// sensitive token), so a relay that swaps trigger→neutral is caught.
	assert.False(t, echoedVerbatim(adaptiveNeutralPayload, adaptiveTriggerPayload))
}
