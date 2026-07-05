// Package epay 彩虹易支付客户端，同时支持两代商户协议：
//
//   - v1（MD5）：经典协议，submit.php 页面支付 / mapi.php 直付 / api.php 查单退款，
//     参数 MD5 加盐签名，回调 GET/POST 表单。
//   - v2（RSA）：新版协议，api/pay/* REST 端点，商户 RSA 私钥 SHA256 签请求、
//     平台公钥验回调与响应，timestamp ±300s 防重放。
//
// 移植自官方 PHP SDK（EpayCore.class.php 新老两版），字段与签名语义逐行对齐；
// 取代功能残缺的 github.com/Calcium-Ion/go-epay（只有发起支付+回调验签）。
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
}

// CreateOrderResult API 直付（mapi / api/pay/create）结果：返回可直接渲染的支付载体，
// 供站内收银台自行展示二维码 / 跳转，而非跳到平台收银台。
type CreateOrderResult struct {
	// TradeNo 平台交易号
	TradeNo string
	// PayURL 支付跳转链接（payurl，浏览器直接打开）
	PayURL string
	// QRCode 二维码内容（qrcode，前端自行渲染成二维码图）
	QRCode string
	// URLScheme 小程序 / deeplink 跳转串（urlscheme，部分渠道返回）
	URLScheme string
	// Raw 平台原始响应（审计留底用）
	Raw map[string]any
}

// NotifyResult 异步回调验签结果（字段名与旧 go-epay 的 VerifyRes 对齐，平替迁移零改动）
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

// RefundArgs 退款参数
type RefundArgs struct {
	// TradeNo 平台交易号
	TradeNo string
	// OutRefundNo 商户退款单号（仅 v2 需要，幂等标识）
	OutRefundNo string
	// Money 退款金额（两位小数字符串）
	Money string
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
	// Summary 总体结论一句话
	Summary string `json:"summary"`
}

// Client 两代协议的统一门面。
type Client interface {
	// Purchase 生成页面跳转支付的 (提交地址, 已签名表单参数)
	Purchase(args *PurchaseArgs) (string, map[string]string, error)
	// CreateOrder API 直付：服务端下单，返回可站内渲染的支付载体（二维码/跳转链接）
	CreateOrder(args *PurchaseArgs) (*CreateOrderResult, error)
	// VerifyNotify 验证异步回调参数并解出订单信息
	VerifyNotify(params map[string]string) (*NotifyResult, error)
	// QueryOrderByOutTradeNo 按商户订单号查单（主动对账用）
	QueryOrderByOutTradeNo(outTradeNo string) (*OrderInfo, error)
	// Refund 订单退款，返回平台原始响应
	Refund(args *RefundArgs) (map[string]any, error)
	// ProbeCapabilities 用商户凭证做一次无副作用探测（查一个随机不存在的订单号），
	// 据此判断平台可达性、凭证有效性，并推断各接口能力是否可用。
	ProbeCapabilities() *CapabilityReport
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
