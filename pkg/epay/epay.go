// Package epay 易支付客户端，同时支持两代商户协议：
//
//   - v1（MD5）：经典协议，submit.php 页面支付 / mapi.php 直付 / api.php 查单退款，
//     参数 MD5 加盐签名，回调 GET/POST 表单。
//   - v2（RSA）：新版协议，api/pay/* REST 端点，商户 RSA 私钥 SHA256 签请求、
//     平台公钥验回调与响应，timestamp ±300s 防重放。
//
// 移植自官方 SDK 新老两版，字段与签名语义逐行对齐；
// 取代功能残缺的旧客户端库（只有发起支付+回调验签）。
package epay

import (
	"errors"
	"net/url"
	"strings"
)

const (
	VersionV1 = "v1"
	VersionV2 = "v2"

	// StatusTradeSuccess 回调/查单中"已支付"的交易状态值（两代协议一致）
	StatusTradeSuccess = "TRADE_SUCCESS"
)

type DeviceType string

const (
	PC     DeviceType = "pc"
	MOBILE DeviceType = "mobile"
)

type Config struct {
	// Version 协议版本：VersionV1（默认）/ VersionV2
	Version string
	// BaseURL 平台接口地址（v1 的 apiurl / v2 的 apiurl），末尾斜杠可有可无
	BaseURL string
	// PID 商户 ID
	PID string
	// Key v1 商户密钥（MD5 加盐）
	Key string
	// PlatformPublicKey v2 平台公钥（base64 DER，可带 PEM 头尾）
	PlatformPublicKey string
	// MerchantPrivateKey v2 商户私钥（base64 DER PKCS8，可带 PEM 头尾）
	MerchantPrivateKey string
}

// PurchaseArgs 发起支付参数（页面跳转与 API 直付共用）。
type PurchaseArgs struct {
	// Type 支付类型（alipay/wxpay/qqpay 等，平台定义）
	Type string
	// ServiceTradeNo 商户订单号
	ServiceTradeNo string
	// Name 商品名称
	Name string
	// Money 金额（两位小数字符串）
	Money string
	// Device 设备类型
	Device    DeviceType
	NotifyUrl *url.URL
	ReturnUrl *url.URL
	// ClientIP 用户真实 IP，仅 API 直付（CreateOrder / mapi）需要；页面跳转可留空。
	ClientIP string
	// Method 仅 v2 API 直付用的接口类型（web/jump/jsapi/app/scan/applet）；留空默认 web
	//（web 会按 device 自动返回 二维码/跳转 URL/小程序参数）。v1 忽略此字段。
	Method string
	// AuthCode 付款码支付（method=scan）时的用户付款码；其它场景留空。
	AuthCode string
}

// CreateOrderResult API 直付（mapi / api/pay/create）结果：返回可直接渲染的支付载体，
// 供站内收银台自行展示二维码 / 跳转，而非跳到平台收银台。
//
// v1(mapi) 直接给 payurl/qrcode/urlscheme；v2(api/pay/create) 给 pay_type + pay_info，
// 我们据 pay_type 归一到 QRCode/PayURL（qrcode→QRCode，jump→PayURL），并原样保留
// PayType/PayInfo 供 jsapi/app/小程序等端侧场景使用。
type CreateOrderResult struct {
	// TradeNo 平台交易号
	TradeNo string
	// PayURL 支付跳转链接（payurl / pay_type=jump 的 pay_info，浏览器直接打开）
	PayURL string
	// QRCode 二维码内容（qrcode / pay_type=qrcode 的 pay_info，前端渲染成二维码图）
	QRCode string
	// URLScheme 小程序 / deeplink 跳转串（urlscheme，部分渠道返回）
	URLScheme string
	// PayType v2 支付载体类型（qrcode/jump/jsapi/scan/wxplugin/wxapp），v1 为空
	PayType string
	// PayInfo v2 支付载体原文：qrcode→二维码内容，jump→跳转 URL，
	// jsapi/scan/wxplugin/wxapp→一段 JSON 参数串（交端侧 SDK 使用）
	PayInfo string
	// Raw 平台原始响应（审计留底用）
	Raw map[string]any
}

// NotifyResult 异步回调验签结果（字段名与旧客户端的 VerifyRes 对齐，平替迁移零改动）
type NotifyResult struct {
	// Type 支付类型
	Type string
	// TradeNo 平台交易号
	TradeNo string
	// ServiceTradeNo 商户订单号
	ServiceTradeNo string
	// Name 商品名称
	Name string
	// Money 金额
	Money string
	// TradeStatus 交易状态（TRADE_SUCCESS=已支付）
	TradeStatus string
	// PID 平台回传的商户号（回调必须与本商户配置一致）
	PID string
	// VerifyStatus 验签通过 *且* pid 与本商户一致。
	// pid 绑定是 v2(RSA) 的关键防线：平台公钥对全平台所有商户是同一份，
	// 不校验 pid 则他人商户的合法回调可冒充本商户骗取入账。
	VerifyStatus bool
}

// OrderInfo 商户查单结果
type OrderInfo struct {
	// Found 平台是否查到该订单（v1 code=1 / v2 code=0 且有单）
	Found bool
	// Paid status==1，已支付
	Paid bool
	// TradeNo 平台交易号
	TradeNo string
	// OutTradeNo 商户订单号
	OutTradeNo string
	// PID 平台返回的商户 ID（对账时应与配置一致，防串单）
	PID string
	// Type 支付类型
	Type string
	// Money 金额（字符串，平台原样）
	Money string
	// Raw 平台原始响应（审计留底用）
	Raw map[string]any
}

// RefundArgs 退款参数。TradeNo（平台单号）与 OutTradeNo（商户单号）必传其一。
type RefundArgs struct {
	// TradeNo 平台交易号（与 OutTradeNo 二选一）
	TradeNo string
	// OutTradeNo 商户订单号（与 TradeNo 二选一）
	OutTradeNo string
	// OutRefundNo 商户退款单号（幂等标识，v2 选填、留空由平台生成；v1 不使用）
	OutRefundNo string
	// Money 退款金额（两位小数字符串）
	Money string
}

// RefundResult 退款结果（v2 api/pay/refund；v1 仅回 code/msg，其余字段可能为空）。
type RefundResult struct {
	RefundNo    string // 平台退款单号
	OutRefundNo string // 商户退款单号
	TradeNo     string // 平台订单号
	Money       string // 退款金额
	ReduceMoney string // 实际扣减余额
	Raw         map[string]any
}

// RefundQueryResult 退款查询结果（v2 api/pay/refundquery）。Success = status==1。
type RefundQueryResult struct {
	RefundNo    string
	OutRefundNo string
	TradeNo     string
	OutTradeNo  string
	Money       string
	ReduceMoney string
	Status      int    // 0 失败 / 1 成功
	Success     bool   // status==1
	AddTime     string // 退款时间
	Raw         map[string]any
}

// MerchantInfo 商户信息（v2 api/merchant/info）。
type MerchantInfo struct {
	PID               string
	Status            int    // 商户状态
	PayStatus         int    // 支付状态
	SettleStatus      int    // 结算状态
	Money             string // 商户余额（元）
	SettleType        int    // 结算方式
	SettleAccount     string // 结算账户
	SettleName        string // 结算账户姓名
	OrderNum          int    // 订单总数
	OrderNumToday     int    // 今日订单数
	OrderNumLastday   int    // 昨日订单数
	OrderMoneyToday   string // 今日订单收入
	OrderMoneyLastday string // 昨日订单收入
	Raw               map[string]any
}

// OrderListResult 商户订单列表（v2 api/merchant/orders）。Orders 为平台原始订单对象数组。
type OrderListResult struct {
	Orders []map[string]any
	Raw    map[string]any
}

// TransferArgs 代付（转账）参数（v2 api/transfer/submit）。
type TransferArgs struct {
	Type     string // alipay/wxpay/qqpay/bank
	Account  string // 收款账号（按 type 对应：账号 / openid / 银行卡号）
	Name     string // 收款人姓名（选填；填了平台会校验）
	Money    string // 金额（元，两位小数）
	Remark   string // 备注（选填）
	OutBizNo string // 商户转账单号（选填，防重）
	BookID   string // 安全转账 bookid（特定通道选填）
}

// TransferResult 代付发起结果（v2 api/transfer/submit）。Status: 0 处理中 / 1 成功。
type TransferResult struct {
	Status    int
	BizNo     string // 平台转账单号
	OutBizNo  string // 商户转账单号
	OrderID   string // 第三方转账订单号
	PayDate   string // 完成时间
	CostMoney string // 实际扣减余额
	Raw       map[string]any
}

// TransferQueryResult 代付查询结果（v2 api/transfer/query）。Status: 0 处理中 / 1 成功 / 2 失败。
type TransferQueryResult struct {
	Status    int
	ErrMsg    string // status==2 时的失败原因
	BizNo     string
	OutBizNo  string
	OrderID   string
	Amount    string
	CostMoney string
	PayDate   string
	Raw       map[string]any
}

// BalanceResult 商户可用余额（v2 api/transfer/balance）。
type BalanceResult struct {
	AvailableMoney string // 可用余额（元）
	TransferRate   string // 代付费率
	Raw            map[string]any
}

// CapabilityStatus 单项能力的探测结论。
type CapabilityStatus struct {
	// Name 能力标识（page_pay/api_pay/verify_notify/query_order/refund）
	Name string `json:"name"`
	// Available 该能力当前是否可用
	Available bool `json:"available"`
	// Detail 结论说明（可用原因 / 不可用原因），前端直接展示
	Detail string `json:"detail"`
}

// MerchantSnapshot 能力检测时**实测**到的商户实时状态
// （来自 api/merchant/info + api/transfer/balance，均无副作用）。仅 v2 且实测成功时有值。
type MerchantSnapshot struct {
	// Status 商户状态
	Status int `json:"status"`
	// PayStatus 支付状态（1=正常开通）
	PayStatus int `json:"pay_status"`
	// SettleStatus 结算状态（1=正常）
	SettleStatus int `json:"settle_status"`
	// Balance 商户余额（元）
	Balance string `json:"balance"`
	// TransferRate 代付费率（能取到即代付通道已开通）
	TransferRate string `json:"transfer_rate"`
	// OrderNum 订单总数
	OrderNum int `json:"order_num"`
}

// CapabilityReport 商户接口能力检测报告。
type CapabilityReport struct {
	// Version 实际探测所用协议（v1/v2）
	Version string `json:"version"`
	// Reachable 平台地址可达（拿到 HTTP 响应）
	Reachable bool `json:"reachable"`
	// CredentialsValid 凭证/签名有效（v1 端点应答正常 / v2 响应验签通过）
	CredentialsValid bool `json:"credentials_valid"`
	// Capabilities 各接口能力清单
	Capabilities []CapabilityStatus `json:"capabilities"`
	// Merchant v2 实测到的商户实时状态（支付/结算状态、余额、代付费率）；
	// nil 表示未取到（v1 协议，或凭证无效未做进一步实测）。
	Merchant *MerchantSnapshot `json:"merchant,omitempty"`
	// Summary 总体结论一句话
	Summary string `json:"summary"`
}

// errUnsupportedInV1 v2(RSA) 新版专有能力在 v1(MD5) 协议下不可用。
var errUnsupportedInV1 = errors.New("epay: 该能力仅 v2(RSA) 新版协议支持，当前商户配置为 v1(MD5)")

// Client 两代协议的统一门面。
//
// 前 6 个方法两代都支持（支付/查单/回调/退款）；其后为 v2(RSA) 新版专有能力
// （退款查询、关单、商户信息、订单列表、代付三件套），v1(MD5) 下返回 errUnsupportedInV1。
type Client interface {
	// Purchase 生成页面跳转支付的 (提交地址, 已签名表单参数)
	Purchase(args *PurchaseArgs) (string, map[string]string, error)
	// CreateOrder API 直付：服务端下单，返回可站内渲染的支付载体（二维码/跳转链接）
	CreateOrder(args *PurchaseArgs) (*CreateOrderResult, error)
	// VerifyNotify 验证异步回调参数并解出订单信息
	VerifyNotify(params map[string]string) (*NotifyResult, error)
	// QueryOrderByOutTradeNo 按商户订单号查单（主动对账用）
	QueryOrderByOutTradeNo(outTradeNo string) (*OrderInfo, error)
	// Refund 订单退款（TradeNo/OutTradeNo 二选一）
	Refund(args *RefundArgs) (*RefundResult, error)
	// ProbeCapabilities 用商户凭证做一次无副作用探测（查一个随机不存在的订单号），
	// 据此判断平台可达性、凭证有效性，并推断各接口能力是否可用。
	ProbeCapabilities() *CapabilityReport

	// —— 以下为 v2(RSA) 新版专有能力，v1 返回 errUnsupportedInV1 ——

	// RefundQuery 退款查询（商户退款单号 OutRefundNo 与平台退款单号 RefundNo 二选一）
	RefundQuery(outRefundNo, refundNo string) (*RefundQueryResult, error)
	// CloseOrder 关闭订单（OutTradeNo 与 TradeNo 二选一）
	CloseOrder(outTradeNo, tradeNo string) error
	// MerchantInfoQuery 查询商户信息（余额、结算、订单统计等）
	MerchantInfoQuery() (*MerchantInfo, error)
	// ListOrders 分页拉取商户订单（offset 从 0 起，limit≤50，status<0 表示不过滤）
	ListOrders(offset, limit, status int) (*OrderListResult, error)
	// Transfer 发起代付（转账）
	Transfer(args *TransferArgs) (*TransferResult, error)
	// TransferQuery 代付查询（商户转账单号 OutBizNo 与平台转账单号 BizNo 二选一）
	TransferQuery(outBizNo, bizNo string) (*TransferQueryResult, error)
	// Balance 查询商户可用余额
	Balance() (*BalanceResult, error)
}

// NewClient 按 Config.Version 构建对应协议的客户端；Version 为空按 v1。
func NewClient(config *Config) (Client, error) {
	if config == nil {
		return nil, errors.New("epay: config is nil")
	}
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil {
		return nil, err
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, errors.New("epay: invalid base url")
	}
	if config.PID == "" {
		return nil, errors.New("epay: pid is required")
	}
	switch config.Version {
	case "", VersionV1:
		if config.Key == "" {
			return nil, errors.New("epay: v1 requires merchant key")
		}
		return &clientV1{pid: config.PID, key: config.Key, baseURL: baseURL}, nil
	case VersionV2:
		return newClientV2(config, baseURL)
	default:
		return nil, errors.New("epay: unsupported version: " + config.Version)
	}
}

// buildPurchaseParams 组装页面支付的业务参数（两代协议共用的字段集）。
func buildPurchaseParams(pid string, args *PurchaseArgs) map[string]string {
	params := map[string]string{
		"pid":          pid,
		"type":         args.Type,
		"out_trade_no": args.ServiceTradeNo,
		"name":         args.Name,
		"money":        args.Money,
	}
	if args.Device != "" {
		params["device"] = string(args.Device)
	}
	if args.NotifyUrl != nil {
		params["notify_url"] = args.NotifyUrl.String()
	}
	if args.ReturnUrl != nil {
		params["return_url"] = args.ReturnUrl.String()
	}
	if args.ClientIP != "" {
		params["clientip"] = args.ClientIP
	}
	return params
}

// createOrderResultFromRaw 从 API 直付响应中解出支付载体（两代协议字段名一致）。
func createOrderResultFromRaw(raw map[string]any) *CreateOrderResult {
	return &CreateOrderResult{
		TradeNo:   fieldString(raw, "trade_no"),
		PayURL:    fieldString(raw, "payurl"),
		QRCode:    fieldString(raw, "qrcode"),
		URLScheme: fieldString(raw, "urlscheme"),
		Raw:       raw,
	}
}

// notifyResultFromParams 从回调参数中解出订单字段（不含验签结论）。
func notifyResultFromParams(params map[string]string) *NotifyResult {
	return &NotifyResult{
		Type:           params["type"],
		TradeNo:        params["trade_no"],
		ServiceTradeNo: params["out_trade_no"],
		Name:           params["name"],
		Money:          params["money"],
		TradeStatus:    params["trade_status"],
		PID:            params["pid"],
	}
}
