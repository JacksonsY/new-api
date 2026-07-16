package detector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyPkgEcho(t *testing.T) {
	exp := "pip install requests==2.31.0"
	assert.Equal(t, "exact", classifyPkgEcho(exp, exp))
	// Benign model noise: trailing period, all-caps.
	assert.Equal(t, "whitespace", classifyPkgEcho(exp, "pip install requests==2.31.0."))
	assert.Equal(t, "whitespace", classifyPkgEcho(exp, "PIP INSTALL REQUESTS==2.31.0"))
	// Typosquat rewrite on the return path.
	assert.Equal(t, "substituted", classifyPkgEcho(exp, "pip install reqeusts==2.31.0"))
	// Space injected inside the package name changes shell tokenization → flagged.
	assert.Equal(t, "substituted", classifyPkgEcho(exp, "pip install req uests==2.31.0"))
	// A benign command alongside a malicious variant → token list differs → flagged.
	assert.Equal(t, "substituted", classifyPkgEcho(exp, "pip install requests==2.31.0 && pip install evil"))
}

func TestStripEchoWrappers(t *testing.T) {
	assert.Equal(t, "pip install requests==2.31.0", stripEchoWrappers("```\npip install requests==2.31.0\n```"))
	assert.Equal(t, "pip install x", stripEchoWrappers("```bash\npip install x\n```"))
	assert.Equal(t, "npm install lodash@4.17.21", stripEchoWrappers("`npm install lodash@4.17.21`"))
	assert.Equal(t, "npm install lodash@4.17.21", stripEchoWrappers("$ npm install lodash@4.17.21"))
	assert.Equal(t, "cargo add serde", stripEchoWrappers("  cargo add serde  "))
}
