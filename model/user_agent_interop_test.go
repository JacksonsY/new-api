package model

// jzlh-fix 回归测试：aff 系统(注册返佣/转移邀请额度)与代理分佣共用 users 行,
// 上游原实现的"读整行→Save 整行"会覆盖并发的原子字段更新导致丢数。
// 本文件锁定修复后的契约：这两个操作只原子地改动自己负责的列。

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInviteUserAtomicIncrements 验证注册返佣只递增 aff 三字段，
// 不触碰同一行上的其他余额列（分佣余额/额度）。
func TestInviteUserAtomicIncrements(t *testing.T) {
	origQuota := common.QuotaForInviter
	common.QuotaForInviter = 100
	t.Cleanup(func() { common.QuotaForInviter = origQuota })

	u := &User{
		Username:        "aff_atomic_user",
		AffCode:         "jzlhaff1",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		CommissionQuota: 12345,
		Quota:           777,
	}
	require.NoError(t, DB.Create(u).Error)

	require.NoError(t, inviteUser(u.Id))
	require.NoError(t, inviteUser(u.Id))

	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 2, r.AffCount)
	assert.Equal(t, 200, r.AffQuota)
	assert.Equal(t, 200, r.AffHistoryQuota)
	assert.Equal(t, 12345, r.CommissionQuota, "commission balance untouched")
	assert.Equal(t, 777, r.Quota, "quota untouched")
}

// TestTransferAffQuotaToQuotaAtomic 验证转移邀请额度的条件原子更新：
// 余额守卫拒绝超额、成功路径两列此消彼长、失败路径分文不动。
func TestTransferAffQuotaToQuotaAtomic(t *testing.T) {
	amount := int(common.QuotaPerUnit) // 最小可转额度

	u := &User{
		Username:        "aff_transfer_user",
		AffCode:         "jzlhaff2",
		Status:          common.UserStatusEnabled,
		AffQuota:        amount,
		Quota:           100,
		CommissionQuota: 999,
	}
	require.NoError(t, DB.Create(u).Error)

	// 低于最小额度被拒
	assert.Error(t, u.TransferAffQuotaToQuota(1))

	// 成功转移
	require.NoError(t, u.TransferAffQuotaToQuota(amount))
	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 0, r.AffQuota)
	assert.Equal(t, 100+amount, r.Quota)
	assert.Equal(t, 999, r.CommissionQuota, "commission balance untouched")

	// 余额不足被拒且不改动
	err := u.TransferAffQuotaToQuota(amount)
	assert.Error(t, err)
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, 0, r.AffQuota)
	assert.Equal(t, 100+amount, r.Quota)
}
