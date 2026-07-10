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
	"github.com/QuantumNous/new-api/setting/system_setting"

	"golang.org/x/net/proxy"
)

var (
	httpClient              *http.Client
	ssrfProtectedHTTPClient *http.Client
	proxyClientLock         sync.Mutex
	proxyClients            = make(map[string]*http.Client)
	globalProxyState        atomic.Value // *globalProxyEntry
	globalProxyBuildLock    sync.Mutex
)

// globalProxyEntry 缓存按当前全局代理设置构建的客户端。
// client 为 nil 表示配置无效（已记录日志），此时走直连。
type globalProxyEntry struct {
	key    string
	client *http.Client
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	urlStr := req.URL.String()
	if err := validateURLWithCurrentFetchSetting(urlStr, true); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
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
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func validateURLWithCurrentFetchSetting(urlStr string, applyDomainIPFilter bool) error {
	fetchSetting := system_setting.GetFetchSetting()
	return common.ValidateURLWithFetchSetting(urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, applyDomainIPFilter && fetchSetting.ApplyIPFilterForDomain)
}

func ValidateSSRFProtectedFetchURL(urlStr string) error {
	return validateURLWithCurrentFetchSetting(urlStr, true)
}

func InitHttpClient() {
	transport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
		ForceAttemptHTTP2:   true,
		Proxy:               http.ProxyFromEnvironment, // Support HTTP_PROXY, HTTPS_PROXY, NO_PROXY env vars
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}

	if common.RelayTimeout == 0 {
		httpClient = &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
	} else {
		httpClient = &http.Client{
			Transport:     transport,
			Timeout:       time.Duration(common.RelayTimeout) * time.Second,
			CheckRedirect: checkRedirect,
		}
	}
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
// 配置了全局代理时返回全局代理客户端（渠道自身代理仍通过 NewProxyHttpClient 优先生效）。
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
	parsedURL, err := url.Parse(proxyURL)
	var transport *http.Transport
	if err == nil {
		transport, err = newProxyTransport(parsedURL)
	}
	if err != nil {
		common.SysError(fmt.Sprintf("invalid global proxy url, falling back to direct connection: %v", err))
		return nil
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

// directFallbackTransport 先经全局代理发送请求；代理层失败时改用直连重试。
// 仅当请求体可重放（无请求体或 GetBody 非 nil）且错误不是调用方取消/超时时才回退。
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

// GetHttpClientWithProxy returns the default client or a proxy-enabled one when proxyURL is provided.
func GetHttpClientWithProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return GetHttpClient(), nil
	}
	return NewProxyHttpClient(proxyURL)
}

// ResetProxyClientCache 清空代理客户端缓存，确保下次使用时重新初始化
func ResetProxyClientCache() {
	proxyClientLock.Lock()
	defer proxyClientLock.Unlock()
	for _, client := range proxyClients {
		if transport, ok := client.Transport.(*http.Transport); ok && transport != nil {
			transport.CloseIdleConnections()
		}
	}
	proxyClients = make(map[string]*http.Client)
}

// NewProxyHttpClient 创建支持代理的 HTTP 客户端
func NewProxyHttpClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		if client := GetHttpClient(); client != nil {
			return client, nil
		}
		return http.DefaultClient, nil
	}

	proxyClientLock.Lock()
	if client, ok := proxyClients[proxyURL]; ok {
		proxyClientLock.Unlock()
		return client, nil
	}
	proxyClientLock.Unlock()

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	transport, err := newProxyTransport(parsedURL)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport:     transport,
		CheckRedirect: checkRedirect,
	}
	client.Timeout = time.Duration(common.RelayTimeout) * time.Second
	proxyClientLock.Lock()
	proxyClients[proxyURL] = client
	proxyClientLock.Unlock()
	return client, nil
}

// newProxyTransport 构建经 parsedURL 指向的代理出站的传输层，
// 支持 http/https/socks5/socks5h 四种代理协议。
func newProxyTransport(parsedURL *url.URL) (*http.Transport, error) {
	transport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}

	switch parsedURL.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsedURL)
		return transport, nil

	case "socks5", "socks5h":
		// 获取认证信息
		var auth *proxy.Auth
		if parsedURL.User != nil {
			auth = &proxy.Auth{
				User:     parsedURL.User.Username(),
				Password: "",
			}
			if password, ok := parsedURL.User.Password(); ok {
				auth.Password = password
			}
		}

		// 创建 SOCKS5 代理拨号器
		// proxy.SOCKS5 使用 tcp 参数，所有 TCP 连接包括 DNS 查询都将通过代理进行。行为与 socks5h 相同
		dialer, err := proxy.SOCKS5("tcp", parsedURL.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
		return transport, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s, must be http, https, socks5 or socks5h", parsedURL.Scheme)
	}
}
