package epay

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// clientV2 新版 RSA 协议（对齐官方 SDK 新版协议）：
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

// buildSignedParams 补 pid/timestamp 后用商户私钥签名（对齐官方 SDK 的 buildRequestParam）。
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

// verifySignedParams 平台公钥验签 + timestamp 防重放（对齐官方 SDK 的 verify()）。
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

// execute 发起 v2 API 请求并验证响应签名（对齐官方 SDK 的 execute()：code==0 为成功且必须验签通过）。
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
// 对齐官方 SDK getSignContent 的数组跳过（否则嵌套字段被转成字符串参与拼串会误判验签失败）。
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
//
// 与旧 SDK 关键差异（对齐平台实测文档 pay_create）：
//   - 必带 method（接口类型）。默认 web：平台按 device 自动返回 二维码/跳转URL/小程序参数；
//     付款码支付传 method=scan + auth_code。
//   - 响应是 pay_type + pay_info（不是 qrcode/payurl/urlscheme），由 v2 专用解析归一。
func (c *clientV2) CreateOrder(args *PurchaseArgs) (*CreateOrderResult, error) {
	if args == nil {
		return nil, errors.New("epay: create order args is nil")
	}
	params := buildPurchaseParams(c.pid, args)
	method := args.Method
	if method == "" {
		method = "web"
	}
	params["method"] = method
	if args.AuthCode != "" {
		params["auth_code"] = args.AuthCode
	}
	raw, err := c.execute("api/pay/create", params)
	if err != nil {
		return nil, err
	}
	return createOrderResultFromV2Raw(raw), nil
}

// createOrderResultFromV2Raw 解析 v2 api/pay/create 响应：平台回 pay_type + pay_info，
// 据 pay_type 归一到 QRCode/PayURL 供站内收银台直接渲染，并原样保留 PayType/PayInfo
// 供 jsapi/app/小程序等端侧场景使用。
func createOrderResultFromV2Raw(raw map[string]any) *CreateOrderResult {
	result := &CreateOrderResult{
		TradeNo: fieldString(raw, "trade_no"),
		PayType: fieldString(raw, "pay_type"),
		PayInfo: fieldString(raw, "pay_info"),
		Raw:     raw,
	}
	switch result.PayType {
	case "qrcode":
		result.QRCode = result.PayInfo // 二维码内容，前端渲染成二维码图
	case "jump":
		result.PayURL = result.PayInfo // 跳转 URL，浏览器直接打开
	}
	// jsapi/scan/wxplugin/wxapp：pay_info 为端侧 JSON 参数串，仅置于 PayInfo，
	// 由调用方按 PayType 分发；站内二维码收银台用不到，故不填 QRCode/PayURL。
	return result
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

// QueryOrderByOutTradeNo 查单：POST api/pay/query,仅带商户单号 out_trade_no
// (主动对账兜住的是"回调丢失"的单,此时拿不到平台单号 trade_no;发空 trade_no
// 实测会让平台回 HTML 错误页,不能同时带)。已知局限:官方 SDK 示例按 trade_no
// 查询,严格只认 trade_no 的 v2 平台会回非 JSON 错误页(NonJSONResponseError),
// 对账调用方须把该错误当作"平台不支持按商户单号查单"明确告警,而不是静默跳过。
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

// Refund 退款：POST api/pay/refund（trade_no / out_trade_no 二选一，out_refund_no 选填）。
func (c *clientV2) Refund(args *RefundArgs) (*RefundResult, error) {
	if args == nil || args.Money == "" {
		return nil, errors.New("epay: refund requires money")
	}
	if args.TradeNo == "" && args.OutTradeNo == "" {
		return nil, errors.New("epay: refund requires trade_no or out_trade_no")
	}
	params := map[string]string{"money": args.Money}
	if args.TradeNo != "" {
		params["trade_no"] = args.TradeNo
	}
	if args.OutTradeNo != "" {
		params["out_trade_no"] = args.OutTradeNo
	}
	if args.OutRefundNo != "" {
		params["out_refund_no"] = args.OutRefundNo
	}
	raw, err := c.execute("api/pay/refund", params)
	if err != nil {
		return nil, err
	}
	return &RefundResult{
		RefundNo:    fieldString(raw, "refund_no"),
		OutRefundNo: fieldString(raw, "out_refund_no"),
		TradeNo:     fieldString(raw, "trade_no"),
		Money:       fieldString(raw, "money"),
		ReduceMoney: fieldString(raw, "reducemoney"),
		Raw:         raw,
	}, nil
}

// RefundQuery 退款查询：POST api/pay/refundquery（out_refund_no / refund_no 二选一）。
func (c *clientV2) RefundQuery(outRefundNo, refundNo string) (*RefundQueryResult, error) {
	if outRefundNo == "" && refundNo == "" {
		return nil, errors.New("epay: refund query requires out_refund_no or refund_no")
	}
	params := map[string]string{}
	if outRefundNo != "" {
		params["out_refund_no"] = outRefundNo
	}
	if refundNo != "" {
		params["refund_no"] = refundNo
	}
	raw, err := c.execute("api/pay/refundquery", params)
	if err != nil {
		return nil, err
	}
	status := fieldInt(raw, "status", 0)
	return &RefundQueryResult{
		RefundNo:    fieldString(raw, "refund_no"),
		OutRefundNo: fieldString(raw, "out_refund_no"),
		TradeNo:     fieldString(raw, "trade_no"),
		OutTradeNo:  fieldString(raw, "out_trade_no"),
		Money:       fieldString(raw, "money"),
		ReduceMoney: fieldString(raw, "reducemoney"),
		Status:      status,
		Success:     status == 1,
		AddTime:     fieldString(raw, "addtime"),
		Raw:         raw,
	}, nil
}

// CloseOrder 关单：POST api/pay/close（out_trade_no / trade_no 二选一）。
func (c *clientV2) CloseOrder(outTradeNo, tradeNo string) error {
	if outTradeNo == "" && tradeNo == "" {
		return errors.New("epay: close order requires out_trade_no or trade_no")
	}
	params := map[string]string{}
	if outTradeNo != "" {
		params["out_trade_no"] = outTradeNo
	}
	if tradeNo != "" {
		params["trade_no"] = tradeNo
	}
	_, err := c.execute("api/pay/close", params)
	return err
}

// MerchantInfoQuery 商户信息：POST api/merchant/info（余额、结算、订单统计）。
func (c *clientV2) MerchantInfoQuery() (*MerchantInfo, error) {
	raw, err := c.execute("api/merchant/info", map[string]string{})
	if err != nil {
		return nil, err
	}
	return &MerchantInfo{
		PID:               fieldString(raw, "pid"),
		Status:            fieldInt(raw, "status", 0),
		PayStatus:         fieldInt(raw, "pay_status", 0),
		SettleStatus:      fieldInt(raw, "settle_status", 0),
		Money:             fieldString(raw, "money"),
		SettleType:        fieldInt(raw, "settle_type", 0),
		SettleAccount:     fieldString(raw, "settle_account"),
		SettleName:        fieldString(raw, "settle_name"),
		OrderNum:          fieldInt(raw, "order_num", 0),
		OrderNumToday:     fieldInt(raw, "order_num_today", 0),
		OrderNumLastday:   fieldInt(raw, "order_num_lastday", 0),
		OrderMoneyToday:   fieldString(raw, "order_money_today"),
		OrderMoneyLastday: fieldString(raw, "order_money_lastday"),
		Raw:               raw,
	}, nil
}

// ListOrders 商户订单列表：POST api/merchant/orders（offset 从 0，limit≤50，status<0 不过滤）。
func (c *clientV2) ListOrders(offset, limit, status int) (*OrderListResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	params := map[string]string{
		"offset": strconv.Itoa(offset),
		"limit":  strconv.Itoa(limit),
	}
	if status >= 0 {
		params["status"] = strconv.Itoa(status)
	}
	raw, err := c.execute("api/merchant/orders", params)
	if err != nil {
		return nil, err
	}
	return &OrderListResult{Orders: rawObjectArray(raw, "data"), Raw: raw}, nil
}

// Transfer 代付（转账）：POST api/transfer/submit。
func (c *clientV2) Transfer(args *TransferArgs) (*TransferResult, error) {
	if args == nil || args.Type == "" || args.Account == "" || args.Money == "" {
		return nil, errors.New("epay: transfer requires type, account and money")
	}
	params := map[string]string{
		"type":    args.Type,
		"account": args.Account,
		"money":   args.Money,
	}
	if args.Name != "" {
		params["name"] = args.Name
	}
	if args.Remark != "" {
		params["remark"] = args.Remark
	}
	if args.OutBizNo != "" {
		params["out_biz_no"] = args.OutBizNo
	}
	if args.BookID != "" {
		params["bookid"] = args.BookID
	}
	raw, err := c.execute("api/transfer/submit", params)
	if err != nil {
		return nil, err
	}
	return &TransferResult{
		Status:    fieldInt(raw, "status", 0),
		BizNo:     fieldString(raw, "biz_no"),
		OutBizNo:  fieldString(raw, "out_biz_no"),
		OrderID:   fieldString(raw, "orderid"),
		PayDate:   fieldString(raw, "paydate"),
		CostMoney: fieldString(raw, "cost_money"),
		Raw:       raw,
	}, nil
}

// TransferQuery 代付查询：POST api/transfer/query（out_biz_no / biz_no 二选一）。
func (c *clientV2) TransferQuery(outBizNo, bizNo string) (*TransferQueryResult, error) {
	if outBizNo == "" && bizNo == "" {
		return nil, errors.New("epay: transfer query requires out_biz_no or biz_no")
	}
	params := map[string]string{}
	if outBizNo != "" {
		params["out_biz_no"] = outBizNo
	}
	if bizNo != "" {
		params["biz_no"] = bizNo
	}
	raw, err := c.execute("api/transfer/query", params)
	if err != nil {
		return nil, err
	}
	return &TransferQueryResult{
		Status:    fieldInt(raw, "status", 0),
		ErrMsg:    fieldString(raw, "errmsg"),
		BizNo:     fieldString(raw, "biz_no"),
		OutBizNo:  fieldString(raw, "out_biz_no"),
		OrderID:   fieldString(raw, "orderid"),
		Amount:    fieldString(raw, "amount"),
		CostMoney: fieldString(raw, "cost_money"),
		PayDate:   fieldString(raw, "paydate"),
		Raw:       raw,
	}, nil
}

// Balance 商户余额：POST api/transfer/balance。
func (c *clientV2) Balance() (*BalanceResult, error) {
	raw, err := c.execute("api/transfer/balance", map[string]string{})
	if err != nil {
		return nil, err
	}
	return &BalanceResult{
		AvailableMoney: fieldString(raw, "available_money"),
		TransferRate:   fieldString(raw, "transfer_rate"),
		Raw:            raw,
	}, nil
}
