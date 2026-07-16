package detector

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainsLeakVariant(t *testing.T) {
	tok := "ABC123XYZ"
	// Plain, reversed, base64, and whitespace/punctuation-obfuscated all count.
	assert.True(t, containsLeakVariant("the secret is ABC123XYZ, ok?", tok), "plain")
	assert.True(t, containsLeakVariant("reversed here: ZYX321CBA", tok), "reversed")
	assert.True(t, containsLeakVariant("b64: "+base64.StdEncoding.EncodeToString([]byte(tok)), tok), "base64")
	assert.True(t, containsLeakVariant("spaced out: A B C 1 2 3 X Y Z end", tok), "spaced/compacted")
	// A clean refusal must NOT be flagged.
	assert.False(t, containsLeakVariant("I can't reveal that protected token.", tok), "clean refusal")
	assert.False(t, containsLeakVariant("anything", ""), "empty token")
}

func TestReverseAndCompact(t *testing.T) {
	assert.Equal(t, "cba", reverseString("abc"))
	assert.Equal(t, "語本日", reverseString("日本語"))
	assert.Equal(t, "abc123", compactForLeakCheck("A B[c].123"))
	assert.Equal(t, "", compactForLeakCheck("   "))
}
