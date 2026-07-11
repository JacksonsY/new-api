package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckRedirectDoesNotReplayNonIdempotentRequest(t *testing.T) {
	var redirectedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			redirectedRequests.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	allowRedirectTestServer(t, server.URL)

	client := &http.Client{CheckRedirect: checkRedirect}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/submit", strings.NewReader(`{"prompt":"test"}`))
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Zero(t, redirectedRequests.Load(), "the client must not replay POST to the redirect target")
}

func TestCheckRedirectAllowsPostConvertedToGet(t *testing.T) {
	redirectedMethod := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			redirectedMethod <- r.Method
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/target", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	allowRedirectTestServer(t, server.URL)

	client := &http.Client{CheckRedirect: checkRedirect}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/submit", strings.NewReader(`{"prompt":"test"}`))
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, http.MethodGet, <-redirectedMethod)
}

func allowRedirectTestServer(t *testing.T, rawURL string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	fetchSetting := system_setting.GetFetchSetting()
	original := *fetchSetting
	t.Cleanup(func() { *fetchSetting = original })
	fetchSetting.EnableSSRFProtection = false
	fetchSetting.AllowedPorts = []string{parsed.Port()}
}
