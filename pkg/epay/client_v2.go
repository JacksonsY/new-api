package epay

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// clientV2 新版 RSA 协议（对齐 PHP SDK 新版 EpayCore）：
// 商户私钥 SHA256WithRSA 签请求，平台公钥验回调与响应，timestamp ±300s 防重放。
type clientV2 struct {
	pid        string
	baseURL    *url.URL
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

const v2TimestampToleranceSeconds = 300

func newClientV2(config *Config, baseURL *url.URL) (*clientV2, error) {
	privateKey, err := parseMerchantPrivateKey(config.MerchantPrivateKey)
	if err != nil {
		return nil, err
	}
	publicKey, err := parsePlatformPublicKey(config.PlatformPublicKey)
	if err != nil {
		return nil, err
	}
	return &clientV2{
		pid:        config.PID,
		baseURL:    baseURL,
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

// buildSignedParams 补 pid/timestamp 后用商户私钥签名（对齐 PHP buildRequestParam）。
func (c *clientV2) buildSignedParams(params map[string]string) (map[string]string, error) {
	params["pid"] = c.pid
	params["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	sign, err := rsaSign(signContent(params), c.privateKey)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign
	params["sign_type"] = "RSA"
	return params, nil
}

// verifySignedParams 平台公钥验签 + timestamp 防重放（对齐 PHP verify()）。
func (c *clientV2) verifySignedParams(params map[string]string) bool {
	sign := params["sign"]
	if sign == "" {
		return false
	}
	timestamp, err := strconv.ParseInt(params["timestamp"], 10, 64)
	if err != nil {
		return false
	}
	drift := time.Now().Unix() - timestamp
	if drift < -v2TimestampToleranceSeconds || drift > v2TimestampToleranceSeconds {
		return false
	}
	return rsaVerify(signContent(params), sign, c.publicKey) == nil
}

// execute 发起 v2 API 请求并验证响应签名（对齐 PHP execute()：code==0 为成功且必须验签通过）。
func (c *clientV2) execute(subPath string, params map[string]string) (map[string]any, error) {
	signed, err := c.buildSignedParams(params)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	for key, value := range signed {
		form.Set(key, value)
	}
	raw, err := httpPostFormJSON(joinURL(c.baseURL, subPath), form)
	if err != nil {
		return nil, err
	}
	if fieldInt(raw, "code", -1) != 0 {
		// 出错返回 nil raw：避免调用方误用未验签的响应体（脚枪防护）
		return nil, fmt.Errorf("epay: api error: %s", fieldString(raw, "msg"))
	}
	if !c.verifySignedParams(rawToSignParams(raw)) {
		return nil, errors.New("epay: response signature verification failed")
	}
	return raw, nil
}

// rawToSignParams 把平台响应转成验签用的标量参数集：跳过 map/slice 值，
// 对齐 PHP getSignContent 的 is_array 跳过（否则嵌套字段被转成字符串参与拼串会误判验签失败）。
func rawToSignParams(raw map[string]any) map[string]string {
	params := make(map[string]string, len(raw))
	for key, value := range raw {
		switch value.(type) {
		case map[string]any, []any:
			continue
		}
		params[key] = fieldString(raw, key)
	}
	return params
}

// Purchase 生成 api/pay/submit 页面支付参数。
func (c *clientV2) Purchase(args *PurchaseArgs) (string, map[string]string, error) {
	if args == nil {
		return "", nil, errors.New("epay: purchase args is nil")
	}
	signed, err := c.buildSignedParams(buildPurchaseParams(c.pid, args))
	if err != nil {
		return "", nil, err
	}
	return joinURL(c.baseURL, "api/pay/submit"), signed, nil
}

// CreateOrder API 直付：POST api/pay/create（RSA 签名 + 响应验签），返回站内可渲染的支付载体。
func (c *clientV2) CreateOrder(args *PurchaseArgs) (*CreateOrderResult, error) {
	if args == nil {
		return nil, errors.New("epay: create order args is nil")
	}
	raw, err := c.execute("api/pay/create", buildPurchaseParams(c.pid, args))
	if err != nil {
		return nil, err
	}
	return createOrderResultFromRaw(raw), nil
}

// VerifyNotify 平台公钥验签 + 防重放后解出订单信息。
func (c *clientV2) VerifyNotify(params map[string]string) (*NotifyResult, error) {
	if len(params) == 0 {
		return nil, errors.New("epay: empty notify params")
	}
	result := notifyResultFromParams(params)
	// 验签 + pid 绑定：C1 关键防线，平台公钥全平台共享，必须校验 pid 才能确认回调发给本商户。
	result.VerifyStatus = c.verifySignedParams(params) && params["pid"] == c.pid
	return result, nil
}

// QueryOrderByOutTradeNo 查单：POST api/pay/query。
// PHP SDK 示例按平台单号 trade_no 查询；主动对账时我们只有商户单号，
// v2 查单接口同样接受 out_trade_no（平台文档），两个字段都带上以最大化兼容。
func (c *clientV2) QueryOrderByOutTradeNo(outTradeNo string) (*OrderInfo, error) {
	if outTradeNo == "" {
		return nil, errors.New("epay: out_trade_no is required")
	}
	raw, err := c.execute("api/pay/query", map[string]string{
		"out_trade_no": outTradeNo,
	})
	if err != nil {
		return nil, err
	}
	return &OrderInfo{
		Found:      true, // v2 code==0 即查到订单（查不到时平台回非零 code，已在 execute 报错）
		Paid:       fieldInt(raw, "status", 0) == 1,
		TradeNo:    fieldString(raw, "trade_no"),
		OutTradeNo: fieldString(raw, "out_trade_no"),
		PID:        fieldString(raw, "pid"),
		Type:       fieldString(raw, "type"),
		Money:      fieldString(raw, "money"),
		Raw:        raw,
	}, nil
}

// Refund 退款：POST api/pay/refund（out_refund_no 为商户退款幂等号）。
func (c *clientV2) Refund(args *RefundArgs) (map[string]any, error) {
	if args == nil || args.TradeNo == "" || args.Money == "" {
		return nil, errors.New("epay: refund requires trade_no and money")
	}
	if args.OutRefundNo == "" {
		return nil, errors.New("epay: v2 refund requires out_refund_no")
	}
	return c.execute("api/pay/refund", map[string]string{
		"trade_no":      args.TradeNo,
		"out_refund_no": args.OutRefundNo,
		"money":         args.Money,
	})
}
