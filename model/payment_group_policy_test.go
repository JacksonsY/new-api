package model

// 蓝图C 充值自动升级分组测试（核心用例对齐 guoruqiang 的表测）：
// 归一口径、规则匹配、受控链保护、订阅互斥、OnlyNewTopups 截断、到期充值档覆盖。

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// enableAutoSwitchForTest 配置升级规则并在用例结束后还原。
func enableAutoSwitchForTest(t *testing.T, rules []operation_setting.PaymentAutoSwitchGroupRule, baseGroup string) {
	t.Helper()
	ps := operation_setting.GetPaymentSetting()
	origEnabled, origRules := ps.AutoSwitchGroupEnabled, ps.AutoSwitchGroupRules
	origBase, origOnly, origFrom := ps.AutoSwitchGroupBaseGroup, ps.AutoSwitchGroupOnlyNewTopups, ps.AutoSwitchGroupEnabledFrom
	ps.AutoSwitchGroupEnabled = true
	ps.AutoSwitchGroupRules = rules
	ps.AutoSwitchGroupBaseGroup = baseGroup
	ps.AutoSwitchGroupOnlyNewTopups = false
	ps.AutoSwitchGroupEnabledFrom = 0
	t.Cleanup(func() {
		ps.AutoSwitchGroupEnabled = origEnabled
		ps.AutoSwitchGroupRules = origRules
		ps.AutoSwitchGroupBaseGroup = origBase
		ps.AutoSwitchGroupOnlyNewTopups = origOnly
		ps.AutoSwitchGroupEnabledFrom = origFrom
	})
}

func seedSuccessTopUp(t *testing.T, userId int, provider string, amount int64, money float64, completeTime int64) {
	t.Helper()
	require.NoError(t, DB.Create(&TopUp{
		UserId: userId, Amount: amount, Money: money,
		TradeNo:         fmt.Sprintf("test-%d-%d-%d", userId, completeTime, amount),
		PaymentMethod:   provider,
		PaymentProvider: provider,
		CreateTime:      completeTime, CompleteTime: completeTime,
		Status: common.TopUpStatusSuccess,
	}).Error)
}

// TestNormalizeTopUpValueUSD 各网关归一口径与入账口径一一对应：
// stripe 按 Money、creem 按 Amount/QuotaPerUnit、其余按 Amount；订阅关联单 Amount=0。
func TestNormalizeTopUpValueUSD(t *testing.T) {
	assert.Equal(t, 25.0, NormalizeTopUpValueUSD(&TopUp{PaymentProvider: PaymentProviderStripe, Money: 25, Amount: 175}))
	assert.Equal(t, 2.0, NormalizeTopUpValueUSD(&TopUp{PaymentProvider: PaymentProviderCreem, Amount: int64(2 * common.QuotaPerUnit), Money: 15}))
	assert.Equal(t, 10.0, NormalizeTopUpValueUSD(&TopUp{PaymentProvider: PaymentProviderEpay, Amount: 10, Money: 70}))
	assert.Equal(t, 5.0, NormalizeTopUpValueUSD(&TopUp{PaymentProvider: PaymentProviderWaffo, Amount: 5, Money: 35}))
	assert.Equal(t, 0.0, NormalizeTopUpValueUSD(nil))
}

// TestMatchAutoSwitchGroupRule 乱序规则取"已达标里阈值最高"档；无达标返回空。
func TestMatchAutoSwitchGroupRule(t *testing.T) {
	rules := []operation_setting.PaymentAutoSwitchGroupRule{
		{ThresholdUSD: 500, Group: "svip"},
		{ThresholdUSD: 50, Group: "vip"},
		{ThresholdUSD: 200, Group: "pro"},
	}
	assert.Equal(t, "", matchAutoSwitchGroupRule(30, rules))
	assert.Equal(t, "vip", matchAutoSwitchGroupRule(50, rules))
	assert.Equal(t, "pro", matchAutoSwitchGroupRule(499, rules))
	assert.Equal(t, "svip", matchAutoSwitchGroupRule(1000, rules))
	assert.Equal(t, "", matchAutoSwitchGroupRule(0, rules))
	assert.Equal(t, "", matchAutoSwitchGroupRule(100, nil))
}

// TestApplyTopUpAutoSwitchGroup 累计跨网关求和 → 升档；受控链外用户不碰；
// 订阅关联单(Amount=0)不计入。
func TestApplyTopUpAutoSwitchGroup(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &UserSubscription{}))
	enableAutoSwitchForTest(t, []operation_setting.PaymentAutoSwitchGroupRule{
		{ThresholdUSD: 50, Group: "vip"},
		{ThresholdUSD: 200, Group: "pro"},
	}, "default")

	user := &User{Username: "autogrp_u1", AffCode: "ag_u1", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(user).Error)
	now := common.GetTimestamp()

	// 跨网关累计：stripe $30 + epay $25 = $55 → vip
	seedSuccessTopUp(t, user.Id, PaymentProviderStripe, 210, 30, now-3600)
	seedSuccessTopUp(t, user.Id, PaymentProviderEpay, 25, 175, now-1800)
	// 订阅关联单 Amount=0 不计
	seedSuccessTopUp(t, user.Id, PaymentProviderEpay, 0, 999, now-1200)

	var switched string
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		switched, err = applyTopUpAutoSwitchGroupTx(tx, user.Id)
		return err
	}))
	assert.Equal(t, "vip", switched)
	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, "vip", reloaded.Group)

	// 再充 $150 → 累计 $205 → pro
	seedSuccessTopUp(t, user.Id, PaymentProviderWaffo, 150, 1050, now-600)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		switched, err = applyTopUpAutoSwitchGroupTx(tx, user.Id)
		return err
	}))
	assert.Equal(t, "pro", switched)

	// 受控链保护：管理员手动设的特殊组不碰
	special := &User{Username: "autogrp_u2", AffCode: "ag_u2", Status: common.UserStatusEnabled, Group: "enterprise"}
	require.NoError(t, DB.Create(special).Error)
	seedSuccessTopUp(t, special.Id, PaymentProviderEpay, 500, 3500, now-300)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		switched, err = applyTopUpAutoSwitchGroupTx(tx, special.Id)
		return err
	}))
	assert.Equal(t, "", switched, "链外分组不得被自动切换")
	var reloadedSpecial User // 不复用已带主键的结构体：GORM First 会把旧主键拼进条件
	require.NoError(t, DB.First(&reloadedSpecial, special.Id).Error)
	assert.Equal(t, "enterprise", reloadedSpecial.Group)
}

// TestAutoSwitchSubscriptionMutex 订阅升级组生效期间充值不覆盖分组；
// 订阅到期回退时按累计充值落到应处档位而非原组。
func TestAutoSwitchSubscriptionMutex(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &UserSubscription{}))
	enableAutoSwitchForTest(t, []operation_setting.PaymentAutoSwitchGroupRule{
		{ThresholdUSD: 50, Group: "vip"},
	}, "default")

	user := &User{Username: "autogrp_sub", AffCode: "ag_sub", Status: common.UserStatusEnabled, Group: "plus"}
	require.NoError(t, DB.Create(user).Error)
	now := common.GetTimestamp()

	// 生效中的订阅升级组 plus
	require.NoError(t, DB.Create(&UserSubscription{
		UserId: user.Id, PlanId: 1, Status: "active",
		StartTime: now - 3600, EndTime: now + 86400,
		UpgradeGroup: "plus", PrevUserGroup: "default",
		CreatedAt: now - 3600, UpdatedAt: now - 3600,
	}).Error)
	seedSuccessTopUp(t, user.Id, PaymentProviderEpay, 100, 700, now-60)

	var switched string
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		switched, err = applyTopUpAutoSwitchGroupTx(tx, user.Id)
		return err
	}))
	assert.Equal(t, "", switched, "订阅生效期间充值不得覆盖分组")
	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, "plus", reloaded.Group)

	// 订阅到期 → 回退解析应落充值档 vip（而非 PrevUserGroup 的 default）
	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ?", user.Id).Update("end_time", now-10).Error)
	_, err := ExpireDueSubscriptions(50)
	require.NoError(t, err)
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, "vip", reloaded.Group, "到期回退应落充值档")
}

// TestAutoSwitchOnlyNewTopups 只算启用后的新充值：启用前的历史单不计入累计。
func TestAutoSwitchOnlyNewTopups(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &UserSubscription{}))
	enableAutoSwitchForTest(t, []operation_setting.PaymentAutoSwitchGroupRule{
		{ThresholdUSD: 50, Group: "vip"},
	}, "default")
	ps := operation_setting.GetPaymentSetting()
	now := common.GetTimestamp()
	ps.AutoSwitchGroupOnlyNewTopups = true
	ps.AutoSwitchGroupEnabledFrom = now - 100

	user := &User{Username: "autogrp_cut", AffCode: "ag_cut", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(user).Error)
	seedSuccessTopUp(t, user.Id, PaymentProviderEpay, 100, 700, now-500) // 启用前
	seedSuccessTopUp(t, user.Id, PaymentProviderEpay, 30, 210, now-50)   // 启用后 $30 < 50

	var switched string
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		switched, err = applyTopUpAutoSwitchGroupTx(tx, user.Id)
		return err
	}))
	assert.Equal(t, "", switched, "启用前的历史充值不得计入")

	seedSuccessTopUp(t, user.Id, PaymentProviderEpay, 25, 175, now-10) // 启用后累计 $55
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		switched, err = applyTopUpAutoSwitchGroupTx(tx, user.Id)
		return err
	}))
	assert.Equal(t, "vip", switched)
}
