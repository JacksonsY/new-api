package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"golang.org/x/net/proxy"
)

var (
	httpClient              *http.Client
	ssrfProtectedHTTPClient *http.Client
	proxyClients            = proxyHTTPClientCache{
		clients: make(map[string]*http.Client),
		aliases: make(map[string]string),
	}
	legacyProxyURLWarnings sync.Map
	globalProxyState       atomic.Value // *globalProxyEntry
	globalProxyBuildLock   sync.Mutex
)

type proxyHTTPClientCache struct {
	mutex   sync.RWMutex
	clients map[string]*http.Client
	aliases map[string]string
}

type proxyURLConfig struct {
	parsedURL *url.URL
	cacheKey  string
}

// globalProxyEntry 缓存按当前全局代理设置构建的客户端。
// client 为 nil 仅表示配置无效且管理员明确允许直连降级。
type globalProxyEntry struct {
	key    string
	client *http.Client
}

// applyRelayHTTP2Setting downgrades an upstream relay transport to HTTP/1.1 when
// RELAY_DISABLE_HTTP2 is enabled. By default Go multiplexes all requests to a
// host onto a small pool of shared HTTP/2 connections; under heavy concurrent
// streaming to the same upstream those shared connections head-of-line-block
// latency-sensitive requests. Forcing HTTP/1.1 gives each in-flight request its
// own pooled connection (bounded by MaxIdleConnsPerHost), removing the drag.
func applyRelayHTTP2Setting(transport *http.Transport) {
	if !common.RelayDisableHTTP2 {
		return
	}
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	transport.Protocols = &protocols
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	urlStr := req.URL.String()
	if err := validateURLWithCurrentFetchSetting(urlStr, true); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) > 0 && !isIdempotentHTTPMethod(req.Method) {
		return http.ErrUseLastResponse
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func checkProtectedFetchRedirect(req *http.Request, via []*http.Request) error {
	urlStr := req.URL.String()
	if err := ValidateSSRFProtectedFetchURL(urlStr); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) > 0 && !isIdempotentHTTPMethod(req.Method) {
		return http.ErrUseLastResponse
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func isIdempotentHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func validateURLWithCurrentFetchSetting(urlStr string, applyDomainIPFilter bool) error {
	fetchSetting := system_setting.GetFetchSetting()
	return common.ValidateURLWithFetchSetting(urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, applyDomainIPFilter && fetchSetting.ApplyIPFilterForDomain)
}

func ValidateSSRFProtectedFetchURL(urlStr string) error {
	return validateURLWithCurrentFetchSetting(urlStr, true)
}

func newRelayHTTPTransport() *http.Transport {
	var transport *http.Transport
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok && defaultTransport != nil {
		transport = defaultTransport.Clone()
	} else {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	transport.MaxIdleConns = common.RelayMaxIdleConns
	transport.MaxIdleConnsPerHost = common.RelayMaxIdleConnsPerHost
	transport.IdleConnTimeout = time.Duration(common.RelayIdleConnTimeout) * time.Second
	transport.ForceAttemptHTTP2 = true
	applyRelayHTTP2Setting(transport)
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	return transport
}

func newRelayHTTPClient(transport *http.Transport) *http.Client {
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
	if common.RelayTimeout != 0 {
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
	}
	return client
}

func InitHttpClient() {
	transport := newRelayHTTPTransport()
	transport.Proxy = http.ProxyFromEnvironment
	httpClient = newRelayHTTPClient(transport)
	ssrfProtectedHTTPClient = newProtectedFetchHTTPClient()
}

// GetHttpClient returns the general outbound client used by relay/provider
// integrations. Do not attach the SSRF-protected dialer here: provider base URLs
// are root/operator-managed deployment targets, not arbitrary user-controlled
// input, and may legitimately point at private networks, private-link endpoints,
// self-hosted services, or local proxies. Code paths that fetch arbitrary
// user-controlled URLs must use GetSSRFProtectedHTTPClient or
// ValidateSSRFProtectedFetchURL instead.
//
// 配置了全局代理时返回全局代理客户端（渠道自身代理仍通过 GetHttpClientWithProxy 优先生效）。
func GetHttpClient() *http.Client {
	if client := getGlobalProxyClient(); client != nil {
		return client
	}
	return httpClient
}

// getGlobalProxyClient 返回按全局代理设置构建的客户端。
// 未配置全局代理或配置无效时返回 nil（调用方回退直连客户端）。
func getGlobalProxyClient() *http.Client {
	proxyURL := strings.TrimSpace(system_setting.GlobalProxyUrl)
	if proxyURL == "" {
		return nil
	}
	directFallback := system_setting.GlobalProxyDirectFallbackEnabled
	key := proxyURL
	if directFallback {
		key += "|direct-fallback"
	}
	if entry, ok := globalProxyState.Load().(*globalProxyEntry); ok && entry.key == key {
		return entry.client
	}

	globalProxyBuildLock.Lock()
	defer globalProxyBuildLock.Unlock()
	if entry, ok := globalProxyState.Load().(*globalProxyEntry); ok && entry.key == key {
		return entry.client
	}
	client := buildGlobalProxyClient(proxyURL, directFallback)
	globalProxyState.Store(&globalProxyEntry{key: key, client: client})
	return client
}

func buildGlobalProxyClient(proxyURL string, directFallback bool) *http.Client {
	parsedURL, legacySuffixStripped, err := common.ParseProxyURLRuntime(proxyURL)
	if err == nil && parsedURL == nil {
		err = fmt.Errorf("empty global proxy url")
	}
	var transport *http.Transport
	if err == nil {
		if legacySuffixStripped {
			warnLegacyProxyURLOnce(newProxyURLConfig(parsedURL))
		}
		transport, err = newProxyTransport(parsedURL)
	}
	if err != nil {
		maskedErr := errors.New(common.MaskSensitiveInfo(err.Error()))
		if directFallback {
			common.SysError(fmt.Sprintf("invalid global proxy url, falling back to direct connection: %v", maskedErr))
			return nil
		}
		common.SysError(fmt.Sprintf("invalid global proxy url, outbound requests will fail closed: %v", maskedErr))
		return &http.Client{
			Transport:     &staticErrorTransport{err: fmt.Errorf("invalid global proxy configuration: %w", maskedErr)},
			CheckRedirect: checkRedirect,
		}
	}
	var rt http.RoundTripper = transport
	if directFallback && httpClient != nil {
		rt = &directFallbackTransport{proxy: transport, direct: httpClient.Transport}
	}
	client := &http.Client{Transport: rt, CheckRedirect: checkRedirect}
	if common.RelayTimeout != 0 {
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
	}
	return client
}

type staticErrorTransport struct {
	err error
}

func (t *staticErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

// directFallbackTransport 先经全局代理发送请求；代理层失败时改用直连重试。
// 仅当方法幂等、请求体可重放（无请求体或 GetBody 非 nil），且错误不是调用方
// 取消/超时时才回退。
type directFallbackTransport struct {
	proxy  http.RoundTripper
	direct http.RoundTripper
}

func (t *directFallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.proxy.RoundTrip(req)
	if err == nil {
		return resp, nil
	}
	// req.Context().Err() 兜住 Client.Timeout/调用方取消：本类型对 http.Client 是未知
	// RoundTripper，超时错误可能是不包装 context 错误的 "net/http: request canceled"。
	if req.Context().Err() != nil ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if !isIdempotentHTTPMethod(req.Method) {
		return nil, err
	}
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return nil, err
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		body, bodyErr := req.GetBody()
		if bodyErr != nil {
			return nil, err
		}
		retry.Body = body
	}
	common.SysError(fmt.Sprintf("global proxy request to %s failed, retrying with direct connection: %v", req.URL.Host, err))
	return t.direct.RoundTrip(retry)
}

// GetSSRFProtectedHTTPClient 返回带拨号时 SSRF 校验的客户端。
// ssrfProtectedHTTPClient 由 InitHttpClient 在启动时初始化，运行期只读。
func GetSSRFProtectedHTTPClient() *http.Client {
	if fetchSetting := system_setting.GetFetchSetting(); fetchSetting != nil && !fetchSetting.EnableSSRFProtection {
		return GetHttpClient()
	}
	return ssrfProtectedHTTPClient
}

func newProxyURLConfig(parsedURL *url.URL) *proxyURLConfig {
	return &proxyURLConfig{
		parsedURL: parsedURL,
		cacheKey:  parsedURL.String(),
	}
}

func warnLegacyProxyURLOnce(config *proxyURLConfig) {
	if _, loaded := legacyProxyURLWarnings.LoadOrStore(config.cacheKey, struct{}{}); loaded {
		return
	}
	logger.LogWarn(
		context.Background(),
		fmt.Sprintf(
			"legacy proxy URL suffix ignored at runtime: scheme=%s host=%s; update the channel proxy setting",
			config.parsedURL.Scheme,
			config.parsedURL.Host,
		),
	)
}

// NormalizeProxyURL validates a proxy URL using runtime-compatible rules and returns its canonical cache key.
func NormalizeProxyURL(rawProxyURL string) (string, error) {
	parsedURL, legacySuffixStripped, err := common.ParseProxyURLRuntime(rawProxyURL)
	if err != nil {
		return "", err
	}
	if parsedURL == nil {
		return "", nil
	}
	config := newProxyURLConfig(parsedURL)
	if legacySuffixStripped {
		warnLegacyProxyURLOnce(config)
	}
	return config.cacheKey, nil
}

// ValidateProxyURL validates a channel proxy URL without connecting to it.
func ValidateProxyURL(rawProxyURL string) error {
	_, err := common.ParseProxyURLStrict(rawProxyURL)
	return err
}

func (cache *proxyHTTPClientCache) get(rawCacheKey string) (*http.Client, bool) {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	cacheKey := rawCacheKey
	if canonicalKey, ok := cache.aliases[rawCacheKey]; ok {
		cacheKey = canonicalKey
	}
	client, ok := cache.clients[cacheKey]
	return client, ok
}

func (cache *proxyHTTPClientCache) getOrCreate(rawCacheKey string, config *proxyURLConfig) (*http.Client, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if client, ok := cache.clients[config.cacheKey]; ok {
		cache.aliases[rawCacheKey] = config.cacheKey
		return client, nil
	}

	client, err := newProxyHTTPClient(config.parsedURL)
	if err != nil {
		return nil, err
	}
	cache.clients[config.cacheKey] = client
	cache.aliases[rawCacheKey] = config.cacheKey
	return client, nil
}

func (cache *proxyHTTPClientCache) remove(cacheKey string) *http.Client {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	client := cache.clients[cacheKey]
	delete(cache.clients, cacheKey)
	for alias, canonicalKey := range cache.aliases {
		if canonicalKey == cacheKey {
			delete(cache.aliases, alias)
		}
	}
	return client
}

func (cache *proxyHTTPClientCache) reset() map[string]*http.Client {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	oldClients := cache.clients
	cache.clients = make(map[string]*http.Client)
	cache.aliases = make(map[string]string)
	return oldClients
}

// newProxyTransport 构建经 parsedURL 指向的代理出站的传输层，
// 支持 http/https/socks5/socks5h 四种代理协议。
func newProxyTransport(proxyURL *url.URL) (*http.Transport, error) {
	transport := newRelayHTTPTransport()

	switch proxyURL.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)

	case "socks5", "socks5h":
		transport.Proxy = nil
		forwardDialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		dialer, err := proxy.FromURL(proxyURL, forwardDialer)
		if err != nil {
			return nil, err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS proxy dialer does not support context cancellation")
		}
		transport.DialContext = contextDialer.DialContext

	default:
		return nil, fmt.Errorf("unsupported proxy scheme")
	}

	return transport, nil
}

func newProxyHTTPClient(proxyURL *url.URL) (*http.Client, error) {
	transport, err := newProxyTransport(proxyURL)
	if err != nil {
		return nil, err
	}
	return newRelayHTTPClient(transport), nil
}

// GetHttpClientWithProxy returns the default client or a cached proxy-enabled client.
func GetHttpClientWithProxy(rawProxyURL string) (*http.Client, error) {
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" {
		if client := GetHttpClient(); client != nil {
			return client, nil
		}
		return http.DefaultClient, nil
	}
	if client, ok := proxyClients.get(trimmedProxyURL); ok {
		return client, nil
	}

	parsedURL, legacySuffixStripped, err := common.ParseProxyURLRuntime(trimmedProxyURL)
	if err != nil {
		return nil, err
	}
	config := newProxyURLConfig(parsedURL)
	if legacySuffixStripped {
		warnLegacyProxyURLOnce(config)
	}
	return proxyClients.getOrCreate(trimmedProxyURL, config)
}

// InvalidateProxyClient removes one proxy client and closes its idle connections.
func InvalidateProxyClient(rawProxyURL string) {
	parsedURL, legacySuffixStripped, err := common.ParseProxyURLRuntime(rawProxyURL)
	if err != nil || parsedURL == nil {
		return
	}
	config := newProxyURLConfig(parsedURL)
	if legacySuffixStripped {
		warnLegacyProxyURLOnce(config)
	}
	if client := proxyClients.remove(config.cacheKey); client != nil {
		client.CloseIdleConnections()
	}
}

// ResetProxyClientCache clears all cached proxy clients.
func ResetProxyClientCache() {
	for _, client := range proxyClients.reset() {
		client.CloseIdleConnections()
	}
}

// NewProxyHttpClient is kept for compatibility.
// Deprecated: use GetHttpClientWithProxy.
func NewProxyHttpClient(proxyURL string) (*http.Client, error) {
	return GetHttpClientWithProxy(proxyURL)
}
