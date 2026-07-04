package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// PaymentAutoSwitchGroupRule 蓝图C 充值自动升级分组规则：累计充值(USD)达到
// ThresholdUSD 即升入 Group。多条规则取"已达标里阈值最高"的一条，顺序无关。
type PaymentAutoSwitchGroupRule struct {
	ThresholdUSD float64 `json:"threshold_usd"`
	Group        string  `json:"group"`
}

type PaymentSetting struct {
	AmountOptions  []int           `json:"amount_options"`
	AmountDiscount map[int]float64 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`

	// ---- 蓝图C 充值→自动升级分组（判定逻辑见 model/payment_group_policy.go）----
	// AutoSwitchGroupEnabled 总开关。
	AutoSwitchGroupEnabled bool `json:"auto_switch_group_enabled"`
	// AutoSwitchGroupOnlyNewTopups 只统计启用之后的新充值（EnabledFrom 由前端在
	// 打开此开关时打当前时间戳；关闭则清零表示统计全部历史充值）。
	AutoSwitchGroupOnlyNewTopups bool  `json:"auto_switch_group_only_new_topups"`
	AutoSwitchGroupEnabledFrom   int64 `json:"auto_switch_group_enabled_from"`
	// AutoSwitchGroupBaseGroup 受控链基准组（空=default）。受控链={基准组}∪{规则目标组}，
	// 当前分组在链外的用户（管理员手动设过特殊组）自动切换永不触碰。
	AutoSwitchGroupBaseGroup string                       `json:"auto_switch_group_base_group"`
	AutoSwitchGroupRules     []PaymentAutoSwitchGroupRule `json:"auto_switch_group_rules"`
}

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:            []int{10, 20, 50, 100, 200, 500},
	AmountDiscount:           map[int]float64{},
	AutoSwitchGroupBaseGroup: "default",
	AutoSwitchGroupRules:     []PaymentAutoSwitchGroupRule{},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}
