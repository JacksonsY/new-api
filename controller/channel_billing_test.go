package controller

// 渠道计费辅助口径测试：sub2api 有效倍率推导 + 分组倍率匹配契约 + 汇率兜底。

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
)

// TestDeriveSub2apiEffectiveRatio sub2api 有效倍率推导契约（actual_cost/cost）：
// 优先当日口径、无当日消费退累计、表价低于 1 分钱不推导、四舍五入到 4 位小数
// 与写入防抖阈值配套。
func TestDeriveSub2apiEffectiveRatio(t *testing.T) {
	bucket := func(cost, actual float64) *Sub2APIUsageCostBucket {
		return &Sub2APIUsageCostBucket{Cost: cost, ActualCost: actual}
	}
	usage := func(today, total *Sub2APIUsageCostBucket) *Sub2APIUsageResponse {
		r := &Sub2APIUsageResponse{}
		r.Usage = &struct {
			Today *Sub2APIUsageCostBucket `json:"today"`
			Total *Sub2APIUsageCostBucket `json:"total"`
		}{Today: today, Total: total}
		return r
	}

	// 当日有消费：用当日口径（反映最新费率），不看累计
	got := deriveSub2apiEffectiveRatio(usage(bucket(10, 8), bucket(100, 50)))
	if assert.NotNil(t, got) {
		assert.Equal(t, 0.8, *got)
	}

	// 当日无消费：退累计口径
	got = deriveSub2apiEffectiveRatio(usage(bucket(0, 0), bucket(100, 150)))
	if assert.NotNil(t, got) {
		assert.Equal(t, 1.5, *got)
	}

	// 表价低于 1 分钱：不推导（防除小数放大噪声）
	assert.Nil(t, deriveSub2apiEffectiveRatio(usage(bucket(0.005, 0.004), bucket(0.009, 0.008))))
	// 无 usage 数据 / nil 响应
	assert.Nil(t, deriveSub2apiEffectiveRatio(&Sub2APIUsageResponse{}))
	assert.Nil(t, deriveSub2apiEffectiveRatio(nil))

	// 四位小数防抖
	got = deriveSub2apiEffectiveRatio(usage(bucket(3, 1), nil))
	if assert.NotNil(t, got) {
		assert.Equal(t, 0.3333, *got)
	}
}

// TestFindUpstreamGroupRate 分组倍率自动同步的匹配契约：精确匹配优先于大小写
// 不敏感匹配（上游可能同时有 "VIP" 和 "vip"），找不到明确返回 false 不误同步。
func TestFindUpstreamGroupRate(t *testing.T) {
	groups := []Sub2APIAdminGroup{
		{Name: "default", RateMultiplier: 1},
		{Name: "vip", RateMultiplier: 0.8},
		{Name: "VIP", RateMultiplier: 0.5},
	}
	ratio, ok := findUpstreamGroupRate(groups, "VIP")
	assert.True(t, ok)
	assert.Equal(t, 0.5, ratio, "精确匹配优先")

	ratio, ok = findUpstreamGroupRate(groups, "Default")
	assert.True(t, ok)
	assert.Equal(t, float64(1), ratio, "无精确匹配时大小写不敏感兜底")

	_, ok = findUpstreamGroupRate(groups, "svip")
	assert.False(t, ok, "不存在的组不得误同步")
}

// TestUsdExchangeRateOrDefault moonshot 换算除零兜底链的末端：汇率未配置(<=0)
// 时必须退回默认值——历史上除以 Price(可为 0)得 +Inf，这里防回归。
func TestUsdExchangeRateOrDefault(t *testing.T) {
	orig := operation_setting.USDExchangeRate
	t.Cleanup(func() { operation_setting.USDExchangeRate = orig })

	operation_setting.USDExchangeRate = 7.2
	assert.Equal(t, 7.2, usdExchangeRateOrDefault())

	operation_setting.USDExchangeRate = 0
	assert.Equal(t, 7.3, usdExchangeRateOrDefault(), "未配置时退默认，防除零")

	operation_setting.USDExchangeRate = -1
	assert.Equal(t, 7.3, usdExchangeRateOrDefault())
}

// TestUpstreamResponseLooksLikeHTML 上游未开放接口时返回登录页/404/SPA 首页（HTML）应被识别，
// 避免把 '<' 丢给 JSON 解析器得到 "invalid character '<'"；合法 JSON 与空/空白响应不误判。
func TestUpstreamResponseLooksLikeHTML(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"doctype", "<!DOCTYPE html><html></html>", true},
		{"html_tag", "<html><head></head></html>", true},
		{"leading_ws_html", "  \n\t<html>", true},
		{"json_object", `{"success":true,"group_ratio":{}}`, false},
		{"json_array", `[{"id":1}]`, false},
		{"leading_ws_json", "  \n{\"ok\":true}", false},
		{"empty", "", false},
		{"whitespace_only", "   \n\t", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, upstreamResponseLooksLikeHTML([]byte(tc.body)))
		})
	}
}
