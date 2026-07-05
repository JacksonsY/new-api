package epay

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v1 签名语义是现网支付的命门：待签名串的排序/过滤/拼接必须与 PHP SDK 和
// 被替换的 go-epay 完全一致，用字面量钉死，防止任何重构悄悄改变行为。
func TestV1SignContentMatchesProtocol(t *testing.T) {
	params := map[string]string{
		"pid":          "1000",
		"type":         "alipay",
		"out_trade_no": "20240001",
		"notify_url":   "http://x/n",
		"name":         "test",
		"money":        "1.00",
		"device":       "",        // 空值必须被剔除
		"sign":         "junk",    // sign 必须被剔除
		"sign_type":    "MD5junk", // sign_type 必须被剔除
	}
	expected := "money=1.00&name=test&notify_url=http://x/n&out_trade_no=20240001&pid=1000&type=alipay"
	require.Equal(t, expected, signContent(params))

	digest := md5.Sum([]byte(expected + "SECRET"))
	assert.Equal(t, fmt.Sprintf("%x", digest), md5Sign(params, "SECRET"))
}

func newV1TestClient(t *testing.T, baseURL string) Client {
	t.Helper()
	client, err := NewClient(&Config{Version: VersionV1, BaseURL: baseURL, PID: "1000", Key: "SECRET"})
	require.NoError(t, err)
	return client
}

func TestV1PurchaseAndNotifyRoundtrip(t *testing.T) {
	client := newV1TestClient(t, "https://pay.example.com")
	notifyURL, _ := url.Parse("https://my.site/api/user/epay/notify")
	payURL, params, err := client.Purchase(&PurchaseArgs{
		Type:           "alipay",
		ServiceTradeNo: "20240001",
		Name:           "TUC100",
		Money:          "1.00",
		Device:         PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      notifyURL,
	})
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/submit.php", payURL)
	assert.Equal(t, "MD5", params["sign_type"])
	assert.NotEmpty(t, params["sign"])

	// 平台回调本质是带签名的同构参数：自签参数必须能通过验签
	notify := map[string]string{
		"pid": "1000", "trade_no": "P123", "out_trade_no": "20240001",
		"type": "alipay", "name": "TUC100", "money": "1.00",
		"trade_status": StatusTradeSuccess,
	}
	md5SignParams(notify, "SECRET")
	result, err := client.VerifyNotify(notify)
	require.NoError(t, err)
	assert.True(t, result.VerifyStatus)
	assert.Equal(t, "20240001", result.ServiceTradeNo)
	assert.Equal(t, StatusTradeSuccess, result.TradeStatus)

	// 篡改金额必须验签失败
	notify["money"] = "100.00"
	tampered, err := client.VerifyNotify(notify)
	require.NoError(t, err)
	assert.False(t, tampered.VerifyStatus)

	// 缺 sign 必须失败
	delete(notify, "sign")
	missing, err := client.VerifyNotify(notify)
	require.NoError(t, err)
	assert.False(t, missing.VerifyStatus)
}

// 查单响应字段容错：不同易支付实现 code/status/pid 可能回 number 或 string。
func TestV1QueryOrderParsesLooselyTypedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "order", r.URL.Query().Get("act"))
		require.Equal(t, "1000", r.URL.Query().Get("pid"))
		require.Equal(t, "SECRET", r.URL.Query().Get("key"))
		require.Equal(t, "20240001", r.URL.Query().Get("out_trade_no"))
		w.Header().Set("Content-Type", "application/json")
		// code/status 为 number、pid 为 number、money 为 string 的混合形态
		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","trade_no":"P123","out_trade_no":"20240001","pid":1000,"type":"alipay","money":"1.00","status":"1"}`))
	}))
	defer server.Close()

	client := newV1TestClient(t, server.URL)
	info, err := client.QueryOrderByOutTradeNo("20240001")
	require.NoError(t, err)
	assert.True(t, info.Found)
	assert.True(t, info.Paid)
	assert.Equal(t, "P123", info.TradeNo)
	assert.Equal(t, "1000", info.PID)
	assert.Equal(t, "1.00", info.Money)
}

func TestV1QueryOrderUnpaidAndNotFound(t *testing.T) {
	responses := []string{
		`{"code":1,"status":0,"out_trade_no":"20240001","pid":"1000","money":"1.00"}`, // 查到但未支付
		`{"code":-1,"msg":"order not found"}`,                                         // 查不到
	}
	idx := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(responses[idx]))
		idx++
	}))
	defer server.Close()

	client := newV1TestClient(t, server.URL)
	unpaid, err := client.QueryOrderByOutTradeNo("20240001")
	require.NoError(t, err)
	assert.True(t, unpaid.Found)
	assert.False(t, unpaid.Paid, "查到但未支付不得判为已支付")

	notFound, err := client.QueryOrderByOutTradeNo("20240001")
	require.NoError(t, err)
	assert.False(t, notFound.Found)
	assert.False(t, notFound.Paid)
}

// API 直付：v1 走 mapi.php，返回二维码/跳转链接供站内收银台渲染；clientip 必须带上。
func TestV1CreateOrderReturnsPaymentPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "1000", r.PostForm.Get("pid"))
		require.Equal(t, "20240001", r.PostForm.Get("out_trade_no"))
		require.Equal(t, "1.2.3.4", r.PostForm.Get("clientip"))
		require.NotEmpty(t, r.PostForm.Get("sign"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"","trade_no":"P123","payurl":"https://pay.example.com/pay/P123","qrcode":"weixin://wxpay/bizpayurl?pr=abc","urlscheme":""}`))
	}))
	defer server.Close()

	client := newV1TestClient(t, server.URL)
	result, err := client.CreateOrder(&PurchaseArgs{
		Type:           "wxpay",
		ServiceTradeNo: "20240001",
		Name:           "TUC100",
		Money:          "1.00",
		ClientIP:       "1.2.3.4",
	})
	require.NoError(t, err)
	assert.Equal(t, "P123", result.TradeNo)
	assert.Equal(t, "https://pay.example.com/pay/P123", result.PayURL)
	assert.Equal(t, "weixin://wxpay/bizpayurl?pr=abc", result.QRCode)
}

func TestV1CreateOrderSurfacesPlatformError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"金额过低"}`))
	}))
	defer server.Close()

	client := newV1TestClient(t, server.URL)
	_, err := client.CreateOrder(&PurchaseArgs{Type: "wxpay", ServiceTradeNo: "x", Money: "0.01"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "金额过低")
}

// ---- v2 (RSA) ----

func generateTestKeyPair(t *testing.T) (privB64 string, pubB64 string, priv *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(privDER), base64.StdEncoding.EncodeToString(pubDER), key
}

func newV2TestSetup(t *testing.T, baseURL string) (Client, *rsa.PrivateKey) {
	t.Helper()
	merchantPriv, _, _ := generateTestKeyPair(t)
	_, platformPub, platformKey := generateTestKeyPair(t)
	client, err := NewClient(&Config{
		Version:            VersionV2,
		BaseURL:            baseURL,
		PID:                "1000",
		MerchantPrivateKey: merchantPriv,
		PlatformPublicKey:  platformPub,
	})
	require.NoError(t, err)
	return client, platformKey
}

func signAsPlatform(t *testing.T, platformKey *rsa.PrivateKey, params map[string]string) map[string]string {
	t.Helper()
	sign, err := rsaSign(signContent(params), platformKey)
	require.NoError(t, err)
	params["sign"] = sign
	params["sign_type"] = "RSA"
	return params
}

func TestV2NotifyVerifyAndAntiReplay(t *testing.T) {
	client, platformKey := newV2TestSetup(t, "https://pay.example.com")

	freshNotify := signAsPlatform(t, platformKey, map[string]string{
		"pid": "1000", "trade_no": "P123", "out_trade_no": "20240001",
		"type": "alipay", "money": "1.00", "trade_status": StatusTradeSuccess,
		"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
	})
	result, err := client.VerifyNotify(freshNotify)
	require.NoError(t, err)
	assert.True(t, result.VerifyStatus)
	assert.Equal(t, "20240001", result.ServiceTradeNo)

	// 篡改金额 → 验签失败
	tampered := make(map[string]string, len(freshNotify))
	for k, v := range freshNotify {
		tampered[k] = v
	}
	tampered["money"] = "100.00"
	tamperedResult, err := client.VerifyNotify(tampered)
	require.NoError(t, err)
	assert.False(t, tamperedResult.VerifyStatus)

	// 超出 ±300s 的时间戳 → 防重放拒绝（签名本身有效）
	stale := signAsPlatform(t, platformKey, map[string]string{
		"pid": "1000", "trade_no": "P123", "out_trade_no": "20240001",
		"money": "1.00", "trade_status": StatusTradeSuccess,
		"timestamp": strconv.FormatInt(time.Now().Unix()-301, 10),
	})
	staleResult, err := client.VerifyNotify(stale)
	require.NoError(t, err)
	assert.False(t, staleResult.VerifyStatus, "过期时间戳必须被防重放拦截")
}

func TestV2QueryOrderVerifiesResponseSignature(t *testing.T) {
	var platformKey *rsa.PrivateKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "1000", r.PostForm.Get("pid"))
		require.Equal(t, "20240001", r.PostForm.Get("out_trade_no"))
		require.NotEmpty(t, r.PostForm.Get("sign"))

		resp := map[string]string{
			"code": "0", "msg": "ok", "trade_no": "P123", "out_trade_no": "20240001",
			"pid": "1000", "type": "alipay", "money": "1.00", "status": "1",
			"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
		}
		signAsPlatform(t, platformKey, resp)
		payload := make(map[string]any, len(resp))
		for k, v := range resp {
			payload[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	client, key := newV2TestSetup(t, server.URL)
	platformKey = key

	info, err := client.QueryOrderByOutTradeNo("20240001")
	require.NoError(t, err)
	assert.True(t, info.Found)
	assert.True(t, info.Paid)
	assert.Equal(t, "P123", info.TradeNo)
}

func TestV2QueryOrderRejectsBadResponseSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// code=0 但签名是垃圾 → 客户端必须拒绝
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"code":0,"msg":"ok","status":1,"money":"1.00","timestamp":"%d","sign":"Z m9v","sign_type":"RSA"}`,
			time.Now().Unix())))
	}))
	defer server.Close()

	client, _ := newV2TestSetup(t, server.URL)
	_, err := client.QueryOrderByOutTradeNo("20240001")
	require.Error(t, err)
}

func TestV2CreateOrderVerifiesResponseAndReturnsPayload(t *testing.T) {
	var platformKey *rsa.PrivateKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.Equal(t, "1000", r.PostForm.Get("pid"))
		require.Equal(t, "5.6.7.8", r.PostForm.Get("clientip"))
		require.NotEmpty(t, r.PostForm.Get("sign"))

		resp := map[string]string{
			"code": "0", "msg": "ok", "trade_no": "P999",
			"payurl": "https://pay.example.com/v2/P999", "qrcode": "alipays://xxx",
			"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
		}
		signAsPlatform(t, platformKey, resp)
		payload := make(map[string]any, len(resp))
		for k, v := range resp {
			payload[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	client, key := newV2TestSetup(t, server.URL)
	platformKey = key
	result, err := client.CreateOrder(&PurchaseArgs{
		Type: "alipay", ServiceTradeNo: "20240001", Name: "TUC", Money: "1.00", ClientIP: "5.6.7.8",
	})
	require.NoError(t, err)
	assert.Equal(t, "P999", result.TradeNo)
	assert.Equal(t, "https://pay.example.com/v2/P999", result.PayURL)
	assert.Equal(t, "alipays://xxx", result.QRCode)
}

// 回归：v2 响应含嵌套字段（如 data 是对象）时，验签必须跳过嵌套字段（对齐 PHP is_array），
// 否则把嵌套结构转成字符串参与拼串会导致验签误判失败。
func TestV2ResponseSignatureIgnoresNestedFields(t *testing.T) {
	var platformKey *rsa.PrivateKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 平台签名只覆盖标量字段（trade_no/money/status/timestamp/code）
		scalar := map[string]string{
			"code": "0", "trade_no": "P1", "out_trade_no": "20240001",
			"pid": "1000", "type": "alipay", "money": "1.00", "status": "1",
			"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
		}
		signAsPlatform(t, platformKey, scalar)
		payload := make(map[string]any, len(scalar)+1)
		for k, v := range scalar {
			payload[k] = v
		}
		// 附带一个嵌套对象字段，平台未纳入签名
		payload["ext"] = map[string]any{"foo": "bar", "n": 1}
		payload["list"] = []any{"a", "b"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	client, key := newV2TestSetup(t, server.URL)
	platformKey = key
	info, err := client.QueryOrderByOutTradeNo("20240001")
	require.NoError(t, err, "含嵌套字段的响应验签必须通过")
	assert.True(t, info.Paid)
	assert.Equal(t, "P1", info.TradeNo)
}

// 安全回归：v1 查单/退款失败时,网络错误不得把含明文 key 的 URL query 带出去。
func TestV1QueryOrderErrorDoesNotLeakKey(t *testing.T) {
	// 指向一个必然连接失败的地址,触发 *url.Error
	client, err := NewClient(&Config{Version: VersionV1, BaseURL: "http://127.0.0.1:1", PID: "1000", Key: "SUPER_SECRET_KEY"})
	require.NoError(t, err)

	_, err = client.QueryOrderByOutTradeNo("20240001")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SUPER_SECRET_KEY", "商户密钥不得出现在错误信息中")
	assert.NotContains(t, err.Error(), "key=", "URL query 必须被剥离")
}

func TestV2ApiErrorSurfacesMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":10014,"msg":"订单不存在"}`))
	}))
	defer server.Close()

	client, _ := newV2TestSetup(t, server.URL)
	_, err := client.QueryOrderByOutTradeNo("20240001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "订单不存在")
}

// 能力检测：v1 平台正常应答"订单不存在" → 判定可达 + 凭证有效。
func TestV1ProbeCapabilitiesHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "order", r.URL.Query().Get("act"))
		require.Equal(t, probeOrderNo, r.URL.Query().Get("out_trade_no"))
		_, _ = w.Write([]byte(`{"code":-1,"msg":"订单不存在"}`))
	}))
	defer server.Close()

	client := newV1TestClient(t, server.URL)
	report := client.ProbeCapabilities()
	assert.True(t, report.Reachable)
	assert.True(t, report.CredentialsValid)
	assert.Equal(t, VersionV1, report.Version)
	// 五类能力都在报告里
	names := map[string]bool{}
	for _, cap := range report.Capabilities {
		names[cap.Name] = cap.Available
	}
	for _, want := range []string{"page_pay", "api_pay", "verify_notify", "query_order", "refund"} {
		assert.Contains(t, names, want)
		assert.True(t, names[want], "凭证有效时 %s 应判为可用", want)
	}
}

// v1 平台提示签名错误 → 凭证无效。
func TestV1ProbeCapabilitiesBadCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1,"msg":"密钥验证失败"}`))
	}))
	defer server.Close()

	client := newV1TestClient(t, server.URL)
	report := client.ProbeCapabilities()
	assert.True(t, report.Reachable, "拿到响应即可达")
	assert.False(t, report.CredentialsValid, "平台提示密钥错误必须判为凭证无效")
}

// v1 平台不可达 → reachable=false，不泄漏 key。
func TestV1ProbeCapabilitiesUnreachable(t *testing.T) {
	client, err := NewClient(&Config{Version: VersionV1, BaseURL: "http://127.0.0.1:1", PID: "1000", Key: "SECRET_KEY_XYZ"})
	require.NoError(t, err)
	report := client.ProbeCapabilities()
	assert.False(t, report.Reachable)
	assert.False(t, report.CredentialsValid)
	assert.NotContains(t, report.Summary, "SECRET_KEY_XYZ", "检测报告不得泄漏密钥")
}

// v2 响应验签通过 → 凭证有效（即便订单不存在）。
func TestV2ProbeCapabilitiesVerifiesSignature(t *testing.T) {
	var platformKey *rsa.PrivateKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"code": "0", "msg": "ok", "status": "0",
			"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
		}
		signAsPlatform(t, platformKey, resp)
		payload := make(map[string]any, len(resp))
		for k, v := range resp {
			payload[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	client, key := newV2TestSetup(t, server.URL)
	platformKey = key
	report := client.ProbeCapabilities()
	assert.True(t, report.Reachable)
	assert.True(t, report.CredentialsValid, "响应验签通过应判为凭证有效")
	assert.Equal(t, VersionV2, report.Version)
}

// v2 响应验签失败 → 凭证无效（平台公钥/商户私钥配错）。
func TestV2ProbeCapabilitiesRejectsBadSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"code":0,"msg":"ok","status":0,"timestamp":"%d","sign":"YmFk","sign_type":"RSA"}`,
			time.Now().Unix())))
	}))
	defer server.Close()

	client, _ := newV2TestSetup(t, server.URL)
	report := client.ProbeCapabilities()
	assert.True(t, report.Reachable)
	assert.False(t, report.CredentialsValid, "响应验签失败必须判为凭证无效")
}

// 回归：平台对 v2 端点回 HTML 错误页（经典版易支付没有 api/pay/* 接口）时，
// 必须判为「可达但接口不匹配」（reachable=true），而非误报「平台地址不可达」，
// 并在结论里建议改用 MD5(v1)。这是本次修复的核心契约。
func TestV2ProbeCapabilitiesHTMLResponseIsReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!DOCTYPE html>\n<html><head><title>系统发生错误</title></head><body>error</body></html>"))
	}))
	defer server.Close()

	client, _ := newV2TestSetup(t, server.URL)
	report := client.ProbeCapabilities()
	assert.True(t, report.Reachable, "拿到 HTML 响应说明平台可达，不能判为不可达")
	assert.False(t, report.CredentialsValid, "接口返回非 JSON 时凭证无法确认，判为不可用")
	assert.NotContains(t, report.Summary, "平台地址不可达", "HTML 响应不得再被描述为不可达")
	assert.Contains(t, report.Summary, "非 JSON", "结论应说明是接口返回了非 JSON")
}

// 回归：v2 能力检测必须按官方 SDK 用 trade_no 查单（而非 out_trade_no），
// 否则平台取到空 trade_no 会抛 HTML 异常页。锁死这个字段防再次迁移错。
func TestV2ProbeCapabilitiesQueriesByTradeNo(t *testing.T) {
	var gotTradeNo, gotOutTradeNo string
	var platformKey *rsa.PrivateKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotTradeNo = r.PostFormValue("trade_no")
		gotOutTradeNo = r.PostFormValue("out_trade_no")
		// 回一个"订单不存在"的验签响应，模拟平台对不存在 trade_no 的正常应答
		resp := map[string]string{
			"code": "0", "msg": "查询成功", "status": "0",
			"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
		}
		signAsPlatform(t, platformKey, resp)
		payload := make(map[string]any, len(resp))
		for k, v := range resp {
			payload[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	client, key := newV2TestSetup(t, server.URL)
	platformKey = key
	report := client.ProbeCapabilities()
	assert.Equal(t, probeOrderNo, gotTradeNo, "查单必须带 trade_no（与官方 SDK 一致）")
	assert.Empty(t, gotOutTradeNo, "查单不应发 out_trade_no")
	assert.True(t, report.Reachable)
	assert.True(t, report.CredentialsValid, "验签通过应判为凭证有效")
}

// C1 安全回归：平台公钥全平台共享，他人商户的合法签名回调必须因 pid 不符被拒，
// 否则攻击者可用自己商户号的合法回调冒充本商户骗取入账（资损）。
func TestVerifyNotifyRejectsMismatchedPID(t *testing.T) {
	// v2：攻击者用平台私钥（合法）签名，但 pid 是他自己的商户号 9999
	client, platformKey := newV2TestSetup(t, "https://pay.example.com")
	attackerNotify := signAsPlatform(t, platformKey, map[string]string{
		"pid": "9999", "trade_no": "ATK", "out_trade_no": "BIG",
		"money": "1000.00", "trade_status": StatusTradeSuccess,
		"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
	})
	result, err := client.VerifyNotify(attackerNotify)
	require.NoError(t, err)
	assert.False(t, result.VerifyStatus, "他人商户号的合法签名回调必须被拒（C1）")
	assert.Equal(t, "9999", result.PID, "PID 应透出供调用方审计")

	// 同一回调若 pid 是本商户号则应通过（确认不是误杀所有）
	okNotify := signAsPlatform(t, platformKey, map[string]string{
		"pid": "1000", "trade_no": "OK", "out_trade_no": "20240001",
		"money": "1.00", "trade_status": StatusTradeSuccess,
		"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
	})
	okResult, err := client.VerifyNotify(okNotify)
	require.NoError(t, err)
	assert.True(t, okResult.VerifyStatus)

	// v1：pid 不符同样拒
	v1 := newV1TestClient(t, "https://pay.example.com")
	v1Notify := map[string]string{
		"pid": "9999", "trade_no": "X", "out_trade_no": "Y",
		"money": "1.00", "trade_status": StatusTradeSuccess,
	}
	md5SignParams(v1Notify, "SECRET")
	v1Result, err := v1.VerifyNotify(v1Notify)
	require.NoError(t, err)
	assert.False(t, v1Result.VerifyStatus, "v1 pid 不符必须拒")
}

// M3 回归：超长数字单号经 float64 往返会丢精度导致验签串不一致，UseNumber 必须保原文。
func TestV2ResponseNumberPrecisionPreserved(t *testing.T) {
	const longTradeNo = "20240715194043661510" // 20 位，超 float64 安全整数范围
	var platformKey *rsa.PrivateKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signed := signAsPlatform(t, platformKey, map[string]string{
			"code": "0", "status": "1", "trade_no": longTradeNo,
			"out_trade_no": "20240001", "money": "1.00",
			"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
		})
		// 手写 JSON 让 trade_no 以数字形态下发（触发 float64 精度问题的场景）
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"code":0,"status":1,"trade_no":%s,"out_trade_no":"20240001","money":"1.00","timestamp":"%s","sign":%q,"sign_type":"RSA"}`,
			longTradeNo, signed["timestamp"], signed["sign"])))
	}))
	defer server.Close()

	client, key := newV2TestSetup(t, server.URL)
	platformKey = key
	info, err := client.QueryOrderByOutTradeNo("20240001")
	require.NoError(t, err, "超长数字单号的响应验签必须通过（UseNumber 保精度）")
	assert.Equal(t, longTradeNo, info.TradeNo, "单号精度不得丢失")
}

func TestNewClientValidatesConfig(t *testing.T) {
	_, err := NewClient(&Config{Version: VersionV1, BaseURL: "https://x.com", PID: "1"})
	assert.Error(t, err, "v1 缺商户 key 必须报错")

	_, err = NewClient(&Config{Version: VersionV2, BaseURL: "https://x.com", PID: "1", MerchantPrivateKey: "xx", PlatformPublicKey: "yy"})
	assert.Error(t, err, "v2 非法密钥必须报错")

	_, err = NewClient(&Config{BaseURL: "not-a-url", PID: "1", Key: "k"})
	assert.Error(t, err, "非法 base url 必须报错")

	client, err := NewClient(&Config{BaseURL: "https://x.com", PID: "1", Key: "k"})
	require.NoError(t, err, "Version 为空默认 v1")
	require.NotNil(t, client)
}
