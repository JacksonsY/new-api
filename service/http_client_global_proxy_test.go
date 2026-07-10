package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func setGlobalProxyForTest(t *testing.T, proxyURL string, directFallback bool) {
	t.Helper()
	if httpClient == nil {
		InitHttpClient()
	}
	system_setting.GlobalProxyUrl = proxyURL
	system_setting.GlobalProxyDirectFallbackEnabled = directFallback
	t.Cleanup(func() {
		system_setting.GlobalProxyUrl = ""
		system_setting.GlobalProxyDirectFallbackEnabled = false
		globalProxyState.Store(&globalProxyEntry{})
	})
}

func TestDirectFallbackTransportRetriesWithReplayableBody(t *testing.T) {
	proxyErr := errors.New("proxy connect refused")
	directBody := ""
	transport := &directFallbackTransport{
		proxy: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, proxyErr
		}),
		direct: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			data, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			directBody = string(data)
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodPost, "http://upstream.example/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o"}`)))
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"model":"gpt-4o"}`, directBody)
}

func TestDirectFallbackTransportSkipsNonReplayableBody(t *testing.T) {
	proxyErr := errors.New("proxy connect refused")
	directCalled := false
	transport := &directFallbackTransport{
		proxy: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, proxyErr
		}),
		direct: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			directCalled = true
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		}),
	}

	// io.NopCloser 包装后 http.NewRequest 无法生成 GetBody，请求体不可重放
	req, err := http.NewRequest(http.MethodPost, "http://upstream.example/v1/chat/completions", io.NopCloser(strings.NewReader("streaming body")))
	require.NoError(t, err)
	require.Nil(t, req.GetBody)

	resp, err := transport.RoundTrip(req)
	require.ErrorIs(t, err, proxyErr)
	assert.Nil(t, resp)
	assert.False(t, directCalled)
}

func TestDirectFallbackTransportRespectsContextCancellation(t *testing.T) {
	cases := []struct {
		name     string
		proxyErr func(req *http.Request) error
	}{
		{
			name:     "wrapped context error",
			proxyErr: func(req *http.Request) error { return req.Context().Err() },
		},
		{
			// Client.Timeout 对未知 RoundTripper 可能返回不包装 context 错误的取消错误
			name:     "opaque cancellation error",
			proxyErr: func(*http.Request) error { return errors.New("net/http: request canceled") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			directCalled := false
			transport := &directFallbackTransport{
				proxy: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					cancel()
					return nil, tc.proxyErr(req)
				}),
				direct: roundTripperFunc(func(*http.Request) (*http.Response, error) {
					directCalled = true
					return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
				}),
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://upstream.example/v1/models", nil)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.False(t, directCalled)
		})
	}
}

func TestGetHttpClientGlobalProxyDirectFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// 全局代理指向不可达端口，开启直连兜底后请求仍应成功
	setGlobalProxyForTest(t, "socks5h://127.0.0.1:1", true)

	client := GetHttpClient()
	require.NotNil(t, client)
	require.NotSame(t, httpClient, client)

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestGetHttpClientGlobalProxyWithoutFallbackFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	setGlobalProxyForTest(t, "socks5h://127.0.0.1:1", false)

	client := GetHttpClient()
	require.NotNil(t, client)
	require.NotSame(t, httpClient, client)

	resp, err := client.Get(server.URL) //nolint:bodyclose
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetHttpClientInvalidGlobalProxySchemeFallsBackDirect(t *testing.T) {
	setGlobalProxyForTest(t, "ftp://127.0.0.1:21", true)

	client := GetHttpClient()
	assert.Same(t, httpClient, client)
}
