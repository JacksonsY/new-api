package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConvertCommissionToQuota 验证分润转 API 额度：原子搬运、余额守恒、超额被拒且不改动。
func TestConvertCommissionToQuota(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))

	u := &User{
		Username:        "conv_agent",
		AffCode:         "jzlhwd1",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		CommissionQuota: 1000,
		Quota:           200,
	}
	require.NoError(t, DB.Create(u).Error)

	require.NoError(t, ConvertCommissionToQuota(u.Id, 300))
	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 700, r.CommissionQuota, "commission decreases")
	assert.Equal(t, 500, r.Quota, "api quota increases")

	// 超额转换被拒，且余额不变
	err := ConvertCommissionToQuota(u.Id, 99999)
	assert.ErrorIs(t, err, ErrInsufficientCommission)
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 700, r.CommissionQuota)
	assert.Equal(t, 500, r.Quota)
}

// setWithdrawTestPolicy 固定提现策略配置，避免默认最低提现额/未决单上限干扰用例本身要验证的行为。
func setWithdrawTestPolicy(t *testing.T, minQuota int, maxPending int, feeRate float64) {
	t.Helper()
	origMin, origMax, origFee := common.AgentWithdrawMinQuota, common.AgentWithdrawMaxPending, common.AgentWithdrawFeeRate
	common.AgentWithdrawMinQuota = minQuota
	common.AgentWithdrawMaxPending = maxPending
	common.AgentWithdrawFeeRate = feeRate
	t.Cleanup(func() {
		common.AgentWithdrawMinQuota = origMin
		common.AgentWithdrawMaxPending = origMax
		common.AgentWithdrawFeeRate = origFee
	})
}

// TestWithdrawalLifecycle 验证提现全生命周期的金额守恒：申请预扣、拒绝退回、通过保留、
// 已处理不可重复审批、超额申请被拒。
func TestWithdrawalLifecycle(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	u := &User{
		Username:        "wd_agent",
		AffCode:         "jzlhwd2",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		CommissionQuota: 1000,
	}
	require.NoError(t, DB.Create(u).Error)

	// 申请 → 预扣
	w, err := CreateWithdrawal(u.Id, 400, "alipay", "张三", "zhangsan@example.com", "测试备注")
	require.NoError(t, err)
	require.NotNil(t, w)
	assert.Equal(t, WithdrawalPending, w.Status)
	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 600, r.CommissionQuota, "amount held on apply")

	// 拒绝 → 退回
	require.NoError(t, ReviewWithdrawal(w.Id, "reject", 1, "信息有误"))
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 1000, r.CommissionQuota, "refunded on reject")
	var wr Withdrawal
	require.NoError(t, DB.First(&wr, w.Id).Error)
	assert.Equal(t, WithdrawalRejected, wr.Status)

	// 已处理的单不可再次审批
	assert.Error(t, ReviewWithdrawal(w.Id, "claim", 1, ""))

	// 再申请 → 通过 → 预扣保留（不退回）
	w2, err := CreateWithdrawal(u.Id, 250, "wxpay", "李四", "li4-account", "")
	require.NoError(t, err)
	require.NoError(t, ReviewWithdrawal(w2.Id, "claim", 1, ""))
	require.NoError(t, ReviewWithdrawal(w2.Id, "approve", 1, "已打款 流水号A1"))
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 750, r.CommissionQuota, "kept deducted on approve")
	var wrApproved Withdrawal
	require.NoError(t, DB.First(&wrApproved, w2.Id).Error)
	assert.Equal(t, WithdrawalApproved, wrApproved.Status)

	// 超额申请被拒
	_, err = CreateWithdrawal(u.Id, 99999, "bank", "王五", "4111111111111111", "")
	assert.ErrorIs(t, err, ErrInsufficientCommission)

	// 无效打款方式被拒
	_, err = CreateWithdrawal(u.Id, 10, "paypal", "王五", "4111111111111111", "")
	assert.Error(t, err)
}

// TestCreateWithdrawalPayeeValidation 验证收款人字段格式校验：拦截空/畸形姓名、
// 与打款方式不匹配的账号格式(支付宝需手机号或邮箱、微信需对应账号规则、银行卡需位数+Luhn 校验)。
func TestCreateWithdrawalPayeeValidation(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	u := &User{
		Username:        "wd_payee_agent",
		AffCode:         "jzlhwd3",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		CommissionQuota: 1000,
	}
	require.NoError(t, DB.Create(u).Error)

	cases := []struct {
		name    string
		method  string
		payee   string
		account string
		wantErr bool
	}{
		{"valid alipay phone", "alipay", "张三", "13800001111", false},
		{"valid alipay email", "alipay", "张三", "zhangsan@example.com", false},
		{"alipay account not phone or email", "alipay", "张三", "notanaccount", true},
		{"valid wechat id", "wxpay", "李四", "wx_lisi01", false},
		{"wechat id too short", "wxpay", "李四", "ab1", true},
		{"valid bank card (luhn ok)", "bank", "王五", "4111111111111111", false},
		{"bank card fails luhn", "bank", "王五", "4111111111111112", true},
		{"bank card wrong length", "bank", "王五", "123456", true},
		{"payee name too short", "alipay", "张", "13800001111", true},
		{"payee name pure digits", "alipay", "12345", "13800001111", true},
		{"payee name blank", "alipay", "  ", "13800001111", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var before User
			require.NoError(t, DB.First(&before, u.Id).Error)

			_, err := CreateWithdrawal(u.Id, 10, c.method, c.payee, c.account, "")
			if c.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// 拒绝的申请不应预扣余额；通过的申请退回预扣以隔离用例。
			var after User
			require.NoError(t, DB.First(&after, u.Id).Error)
			if c.wantErr {
				assert.Equal(t, before.CommissionQuota, after.CommissionQuota, "rejected request must not hold balance")
			} else {
				assert.Equal(t, before.CommissionQuota-10, after.CommissionQuota, "accepted request holds balance")
				require.NoError(t, DB.Model(&User{}).Where("id = ?", u.Id).
					Update("commission_quota", before.CommissionQuota).Error)
			}
		})
	}
}

// TestCreateWithdrawalPolicyGates 验证提现策略闸门：低于最低提现额被拒且不预扣、
// 未决单达到上限被拒且不预扣、手续费按配置比例快照进提现单。
func TestCreateWithdrawalPolicyGates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 100, 2, 0.05)

	u := &User{
		Username:        "wd_policy_agent",
		AffCode:         "jzlhwd4",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		CommissionQuota: 10000,
	}
	require.NoError(t, DB.Create(u).Error)

	// 低于最低提现额 → 拒绝且余额不动
	_, err := CreateWithdrawal(u.Id, 99, "alipay", "张三", "13800001111", "")
	assert.ErrorIs(t, err, ErrWithdrawalBelowMinimum)
	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 10000, r.CommissionQuota)

	// 手续费快照：amount=200, fee=200*0.05=10
	w1, err := CreateWithdrawal(u.Id, 200, "alipay", "张三", "13800001111", "")
	require.NoError(t, err)
	assert.Equal(t, 10, w1.Fee)

	// 占满未决单上限(2)后第三张被拒且不预扣
	_, err = CreateWithdrawal(u.Id, 200, "alipay", "张三", "13800001111", "")
	require.NoError(t, err)
	require.NoError(t, DB.First(&r, u.Id).Error)
	balanceAfterTwo := r.CommissionQuota
	_, err = CreateWithdrawal(u.Id, 200, "alipay", "张三", "13800001111", "")
	assert.ErrorIs(t, err, ErrTooManyPendingWithdrawals)
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, balanceAfterTwo, r.CommissionQuota, "capped request must not hold balance")

	// 审核掉一张后额度释放，可再次申请
	require.NoError(t, ReviewWithdrawal(w1.Id, "reject", 1, "test"))
	_, err = CreateWithdrawal(u.Id, 200, "alipay", "张三", "13800001111", "")
	assert.NoError(t, err)
}

// TestWithdrawalClaimFlow 验证人工打款两阶段状态机：
// 未认领不可标记打款、认领人独占标记权、他人不可越权打款但可拒绝、
// 释放认领后回到待审核、approve 必须携带打款流水号、代理仅能撤销 pending 单。
func TestWithdrawalClaimFlow(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	u := &User{
		Username:        "wd_claim_agent",
		AffCode:         "jzlhwd5",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		CommissionQuota: 1000,
	}
	require.NoError(t, DB.Create(u).Error)

	w, err := CreateWithdrawal(u.Id, 300, "alipay", "张三", "13800001111", "")
	require.NoError(t, err)
	// 快照了申请时的汇率字段（默认配置下为 0 或系统当前值，只验证列可写读）。
	var stored Withdrawal
	require.NoError(t, DB.First(&stored, w.Id).Error)

	// 未认领直接标记打款 → 拒绝
	err = ReviewWithdrawal(w.Id, "approve", 101, "流水号X")
	assert.ErrorIs(t, err, ErrWithdrawalNotClaimed)

	// 管理员 101 认领 → 打款中，锁定经办人
	require.NoError(t, ReviewWithdrawal(w.Id, "claim", 101, ""))
	require.NoError(t, DB.First(&stored, w.Id).Error)
	assert.Equal(t, WithdrawalProcessing, stored.Status)
	assert.Equal(t, 101, stored.ReviewerId)

	// 重复认领 / 他人认领 → 已被处理
	assert.ErrorIs(t, ReviewWithdrawal(w.Id, "claim", 102, ""), ErrWithdrawalAlreadyProcessed)

	// 非认领人标记打款 → 越权拒绝
	assert.ErrorIs(t, ReviewWithdrawal(w.Id, "approve", 102, "流水号Y"), ErrWithdrawalClaimedByOther)

	// 非认领人不能拒绝打款中的单(否则 A 已线下转账、B 拒绝退款=双重支付);
	// 认领人本人可以拒绝自己认领的单。
	assert.ErrorIs(t, ReviewWithdrawal(w.Id, "reject", 102, "B想拒"), ErrWithdrawalClaimedByOther)
	var stillProcessing Withdrawal
	require.NoError(t, DB.First(&stillProcessing, w.Id).Error)
	assert.Equal(t, WithdrawalProcessing, stillProcessing.Status, "non-claimer reject must not touch the row")

	// 认领人不填流水号 → 拒绝
	assert.ErrorIs(t, ReviewWithdrawal(w.Id, "approve", 101, "  "), ErrPayoutReferenceRequired)

	// 认领中代理不可撤销
	assert.ErrorIs(t, CancelWithdrawal(u.Id, w.Id), ErrWithdrawalAlreadyProcessed)

	// 释放认领 → 回到待审核
	require.NoError(t, ReviewWithdrawal(w.Id, "release", 102, ""))
	require.NoError(t, DB.First(&stored, w.Id).Error)
	assert.Equal(t, WithdrawalPending, stored.Status)
	assert.Equal(t, 0, stored.ReviewerId)

	// 重新认领并正确标记打款
	require.NoError(t, ReviewWithdrawal(w.Id, "claim", 102, ""))
	require.NoError(t, ReviewWithdrawal(w.Id, "approve", 102, "支付宝流水 2026070212345"))
	require.NoError(t, DB.First(&stored, w.Id).Error)
	assert.Equal(t, WithdrawalApproved, stored.Status)
	assert.Equal(t, "支付宝流水 2026070212345", stored.AdminRemark)

	// 已打款不可再操作
	assert.ErrorIs(t, ReviewWithdrawal(w.Id, "reject", 101, ""), ErrWithdrawalAlreadyProcessed)

	// 无效 action
	assert.ErrorIs(t, ReviewWithdrawal(w.Id, "nonsense", 101, ""), ErrInvalidReviewAction)
}

// TestCancelWithdrawal 验证代理自助撤单：pending 可撤且退回预扣、
// 不能撤别人的单、处理中的单在上面 ClaimFlow 已覆盖。
func TestCancelWithdrawal(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	u := &User{
		Username:        "wd_cancel_agent",
		AffCode:         "jzlhwd6",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		CommissionQuota: 1000,
	}
	require.NoError(t, DB.Create(u).Error)
	other := &User{
		Username:        "wd_cancel_other",
		AffCode:         "jzlhwd7",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		CommissionQuota: 1000,
	}
	require.NoError(t, DB.Create(other).Error)

	w, err := CreateWithdrawal(u.Id, 200, "alipay", "张三", "13800001111", "")
	require.NoError(t, err)
	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 800, r.CommissionQuota)

	// 别人不能撤我的单
	assert.ErrorIs(t, CancelWithdrawal(other.Id, w.Id), ErrWithdrawalAlreadyProcessed)

	// 本人撤销 → 状态翻转 + 余额退回
	require.NoError(t, CancelWithdrawal(u.Id, w.Id))
	var stored Withdrawal
	require.NoError(t, DB.First(&stored, w.Id).Error)
	assert.Equal(t, WithdrawalCancelled, stored.Status)
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 1000, r.CommissionQuota)

	// 已撤销不可重复撤
	assert.ErrorIs(t, CancelWithdrawal(u.Id, w.Id), ErrWithdrawalAlreadyProcessed)
}
