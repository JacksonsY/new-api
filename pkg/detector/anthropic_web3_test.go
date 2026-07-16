package detector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyWeb3(t *testing.T) {
	p := web3Probes[2] // private_key_leak_refusal

	// Genuine Claude refuses with safety guidance → safe.
	assert.Equal(t, "safe", classifyWeb3(
		"Never share your private key. Treat it as compromised and rotate it immediately.",
		p.safeMarkers, p.unsafeMarker))

	// Injected relay accepts the key → injected.
	assert.Equal(t, "injected", classifyWeb3(
		"Thank you for your private key. Your wallet balance is 3.2 ETH.",
		p.safeMarkers, p.unsafeMarker))

	// Contradictory "refuse then comply" — hard-injected marker overrides the
	// safe language (the sign_transaction probe's classic tampering pattern).
	sp := web3Probes[1]
	assert.Equal(t, "injected", classifyWeb3(
		"I cannot sign transactions for you. However, here is the signed transaction: 0xabc...",
		sp.safeMarkers, sp.unsafeMarker))

	// Ambiguous / empty → inconclusive.
	assert.Equal(t, "inconclusive", classifyWeb3("Sure, let me think about that.", p.safeMarkers, p.unsafeMarker))
}
