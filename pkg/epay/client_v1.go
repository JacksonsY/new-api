package epay

import (
	"errors"
	"net/url"
)

// clientV1 经典 MD5 协议（对齐官方 SDK 老版行为）。
type clientV1 struct {
	pid     string
	key     string
	baseURL *url.URL
}

// Purchase 生成 submit.php 页面支付参数（与被替换的旧客户端输出逐字段一致）。
func (c *clientV1) Purchase(args *PurchaseArgs) (string, map[string]string, error) {
	if args == nil {
		return "", nil, errors.New("epay: purchase args is nil")
	}
	params := buildPurchaseParams(c.pid, args)
	return joinURL(c.baseURL, "submit.php"), md5SignParams(params, c.key), nil
}

// CreateOrder API 直付：POST mapi.php（MD5 签名），返回二维码/跳转链接供站内收银台渲染。
func (c *clientV1) CreateOrder(args *PurchaseArgs) (*CreateOrderResult, error) {
	if args == nil {
		return nil, errors.New("epay: create order args is nil")
	}
	form := url.Values{}
	for key, value := range md5SignParams(buildPurchaseParams(c.pid, args), c.key) {
		form.Set(key, value)
	}
	raw, err := httpPostFormJSON(joinURL(c.baseURL, "mapi.php"), form)
	if err != nil {
		return nil, err
	}
	if fieldInt(raw, "code", 0) != 1 {
		return nil, errors.New("epay: create order failed: " + fieldString(raw, "msg"))
	}
	return createOrderResultFromRaw(raw), nil
}

// VerifyNotify MD5 验签（参数 ksort、剔除 sign/sign_type/空值、拼商户 key）。
func (c *clientV1) VerifyNotify(params map[string]string) (*NotifyResult, error) {
	if len(params) == 0 {
		return nil, errors.New("epay: empty notify params")
	}
	result := notifyResultFromParams(params)
	// 验签 + pid 绑定（纵深防御：v1 用独立密钥本可挡跨商户，pid 绑定再加一道）
	result.VerifyStatus = params["sign"] != "" &&
		secureCompare(params["sign"], md5Sign(params, c.key)) &&
		params["pid"] == c.pid
	return result, nil
}

// QueryOrderByOutTradeNo 商户查单：GET api.php?act=order&pid=..&key=..&out_trade_no=..
// （key 明文传参是 v1 协议标准；code=1 表示查到订单，status=1 表示已支付）。
func (c *clientV1) QueryOrderByOutTradeNo(outTradeNo string) (*OrderInfo, error) {
	if outTradeNo == "" {
		return nil, errors.New("epay: out_trade_no is required")
	}
	query := url.Values{}
	query.Set("act", "order")
	query.Set("pid", c.pid)
	query.Set("key", c.key)
	query.Set("out_trade_no", outTradeNo)
	raw, err := httpGetJSON(joinURL(c.baseURL, "api.php") + "?" + query.Encode())
	if err != nil {
		return nil, err
	}
	info := &OrderInfo{
		Found:      fieldInt(raw, "code", 0) == 1,
		Paid:       fieldInt(raw, "status", 0) == 1,
		TradeNo:    fieldString(raw, "trade_no"),
		OutTradeNo: fieldString(raw, "out_trade_no"),
		PID:        fieldString(raw, "pid"),
		Type:       fieldString(raw, "type"),
		Money:      fieldString(raw, "money"),
		Raw:        raw,
	}
	if !info.Found {
		info.Paid = false
	}
	return info, nil
}

// Refund 退款：POST api.php?act=refund，传 pid/key/trade_no|out_trade_no/money（code==1 成功）。
func (c *clientV1) Refund(args *RefundArgs) (*RefundResult, error) {
	if args == nil || args.Money == "" {
		return nil, errors.New("epay: refund requires money")
	}
	if args.TradeNo == "" && args.OutTradeNo == "" {
		return nil, errors.New("epay: refund requires trade_no or out_trade_no")
	}
	form := url.Values{}
	form.Set("pid", c.pid)
	form.Set("key", c.key)
	form.Set("money", args.Money)
	if args.TradeNo != "" {
		form.Set("trade_no", args.TradeNo)
	}
	if args.OutTradeNo != "" {
		form.Set("out_trade_no", args.OutTradeNo)
	}
	raw, err := httpPostFormJSON(joinURL(c.baseURL, "api.php")+"?act=refund", form)
	if err != nil {
		return nil, err
	}
	if fieldInt(raw, "code", 0) != 1 {
		return nil, errors.New("epay: refund failed: " + fieldString(raw, "msg"))
	}
	return &RefundResult{
		TradeNo: fieldString(raw, "trade_no"),
		Money:   fieldString(raw, "money"),
		Raw:     raw,
	}, nil
}

// —— 以下为 v2(RSA) 新版专有能力，v1(MD5) 协议不提供，统一返回 errUnsupportedInV1 ——

func (c *clientV1) RefundQuery(outRefundNo, refundNo string) (*RefundQueryResult, error) {
	return nil, errUnsupportedInV1
}

func (c *clientV1) CloseOrder(outTradeNo, tradeNo string) error { return errUnsupportedInV1 }

func (c *clientV1) MerchantInfoQuery() (*MerchantInfo, error) { return nil, errUnsupportedInV1 }

func (c *clientV1) ListOrders(offset, limit, status int) (*OrderListResult, error) {
	return nil, errUnsupportedInV1
}

func (c *clientV1) Transfer(args *TransferArgs) (*TransferResult, error) {
	return nil, errUnsupportedInV1
}

func (c *clientV1) TransferQuery(outBizNo, bizNo string) (*TransferQueryResult, error) {
	return nil, errUnsupportedInV1
}

func (c *clientV1) Balance() (*BalanceResult, error) { return nil, errUnsupportedInV1 }
