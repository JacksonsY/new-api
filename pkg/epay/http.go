package epay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 与 SDK getHttpResponse 对齐的超时；响应限读 1MiB 防异常平台回吐超大体。
// 显式禁止重定向：支付客户端不应被平台/中间人的 3xx 引导到其它地址。
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

const maxResponseBytes = 1 << 20

// NonJSONResponseError 表示平台返回了 HTTP 响应，但响应体不是 JSON（通常是 HTML 错误页，
// 如服务端框架的「系统发生错误」页）。它与传输层错误（DNS/连接/超时）本质不同：拿到它恰恰
// 说明平台地址是**可达**的，问题多为接口地址或协议不匹配——最典型的是平台不支持 v2(RSA)
// 新版 REST 接口（api/pay/*），此时应改用 v1(MD5)。
type NonJSONResponseError struct {
	Preview     string
	ContentType string
}

func (e *NonJSONResponseError) Error() string {
	if e.ContentType != "" {
		return "epay: non-json response (content-type=" + e.ContentType + "): " + e.Preview
	}
	return "epay: non-json response: " + e.Preview
}

func doRequest(req *http.Request) (map[string]any, error) {
	req.Header.Set("Accept", "*/*")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, sanitizeHTTPError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("epay: response too large")
	}
	// UseNumber 让 JSON 数字保留原始文本（如 "1.00"、超 15 位单号），
	// 避免 float64 往返改变响应验签串导致 v2 验签假阴性。
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		preview := strings.TrimSpace(string(body))
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, &NonJSONResponseError{Preview: preview, ContentType: resp.Header.Get("Content-Type")}
	}
	return raw, nil
}

func httpGetJSON(requestURL string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	return doRequest(req)
}

func httpPostFormJSON(requestURL string, form url.Values) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodPost, requestURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doRequest(req)
}

// joinURL 拼接平台接口路径（容忍 BaseURL 末尾斜杠差异）。
func joinURL(baseURL *url.URL, subPath string) string {
	base := strings.TrimSuffix(baseURL.String(), "/")
	return base + "/" + strings.TrimPrefix(subPath, "/")
}

// sanitizeHTTPError 剥掉网络错误里 URL 的 query（v1 查单/退款把商户 key 明文放在
// query 参数里，Go 的 *url.Error 会带完整 URL，直接透传/记录会泄漏商户密钥）。
func sanitizeHTTPError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if u, perr := url.Parse(urlErr.URL); perr == nil {
			u.RawQuery = ""
			u.User = nil
			return fmt.Errorf("%s %q: %w", urlErr.Op, u.String(), urlErr.Err)
		}
		// URL 无法解析时宁可只暴露操作类型，也不带出可能含 key 的原文
		return fmt.Errorf("%s: %w", urlErr.Op, urlErr.Err)
	}
	return err
}

// fieldString 宽容读取平台响应字段：不同易支付实现同一字段可能回 string 或 number。
func fieldString(raw map[string]any, key string) string {
	value, ok := raw[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String() // 保留平台下发的原始数字文本，保证验签串一致
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// rawObjectArray 从平台响应里取出一个对象数组字段（如订单列表的 data）。
// 非数组、或元素非对象时安全跳过，返回可能为空的切片。
func rawObjectArray(raw map[string]any, key string) []map[string]any {
	arr, ok := raw[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

// fieldInt 宽容读取整型字段（"1" / 1 / 1.0 均可），缺失或不可解析返回 fallback。
func fieldInt(raw map[string]any, key string, fallback int) int {
	value, ok := raw[key]
	if !ok || value == nil {
		return fallback
	}
	switch v := value.(type) {
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
		if f, err := v.Float64(); err == nil {
			return int(f)
		}
		return fallback
	case float64:
		return int(v)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed
		}
		return fallback
	default:
		return fallback
	}
}
