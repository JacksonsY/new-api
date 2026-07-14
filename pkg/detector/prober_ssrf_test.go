package detector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		// Public destinations pass. IP literals keep the test deterministic and
		// offline (no DNS needed).
		{"public ip", "https://8.8.8.8", false},
		{"public ip with path", "https://1.1.1.1/v1", false},

		// Loopback.
		{"loopback ipv4", "http://127.0.0.1", true},
		{"loopback ipv4 subnet", "http://127.0.0.2:8080", true},
		{"loopback name", "http://localhost", true},
		{"loopback ipv6", "http://[::1]", true},

		// RFC1918 private ranges.
		{"private 10/8", "http://10.0.0.5", true},
		{"private 172.16/12", "http://172.16.5.5:8080", true},
		{"private 192.168/16", "http://192.168.1.1", true},

		// Link-local incl. cloud metadata.
		{"link-local", "http://169.254.1.1", true},
		{"cloud metadata", "http://169.254.169.254", true},

		// Unspecified.
		{"unspecified ipv4", "http://0.0.0.0", true},

		// Scheme / parse rejections.
		{"ftp scheme", "ftp://8.8.8.8", true},
		{"file scheme", "file:///etc/passwd", true},
		{"empty", "", true},
		{"no host", "http://", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := guardBaseURL(c.rawURL)
			if c.wantErr {
				assert.Errorf(t, err, "expected %q to be rejected", c.rawURL)
			} else {
				assert.NoErrorf(t, err, "expected %q to be allowed", c.rawURL)
			}
		})
	}
}

func TestNewProberRejectsBlockedURL(t *testing.T) {
	_, err := newProber(Config{BaseURL: "http://127.0.0.1", TimeoutSeconds: 5})
	require.Error(t, err)
}

func TestNewProberAcceptsPublicURL(t *testing.T) {
	p, err := newProber(Config{BaseURL: "https://8.8.8.8/", TimeoutSeconds: 5})
	require.NoError(t, err)
	require.NotNil(t, p)
	// Base URL is normalized (trailing slash trimmed) and redirects are disabled.
	assert.Equal(t, "https://8.8.8.8", p.baseURL)
	require.NotNil(t, p.client)
	require.NotNil(t, p.client.CheckRedirect)
}

func TestGuardDialAddress(t *testing.T) {
	assert.NoError(t, guardDialAddress("8.8.8.8:443"))
	assert.Error(t, guardDialAddress("127.0.0.1:443"))
	assert.Error(t, guardDialAddress("169.254.169.254:80"))
	assert.Error(t, guardDialAddress("10.1.2.3:443"))
}
