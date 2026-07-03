package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countCommissions 返回当前 commissions 表总行数（用于断言"无分润产生"的增量为 0）。
func countCommissions(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, DB.Model(&Commission{}).Count(&n).Error)
	return n
}

// confirmPaymentComplianceForTest 打开合规确认开关(分佣入账的前置闸门)并在用例结束后还原。
func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	ps := operation_setting.GetPaymentSetting()
	origConfirmed, origVersion := ps.ComplianceConfirmed, ps.ComplianceTermsVersion
	ps.ComplianceConfirmed = true
	ps.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		ps.ComplianceConfirmed = origConfirmed
		ps.ComplianceTermsVersion = origVersion
	})
}

// TestRecordAgentCommission 验证核心金额不变量：下级消费 → 上级代理按 usage_profit_rate 累计分润，
// 且写入流水、余额与历史累计一致、可多次累加。
func TestRecordAgentCommission(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	agent := &User{
		Username:        "agent_comm_main",
		AffCode:         "jzlhcomm1",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.1,
	}
	require.NoError(t, DB.Create(agent).Error)
	downstream := &User{
		Username:  "down_comm_main",
		AffCode:   "jzlhcomm2",
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
	}
	require.NoError(t, DB.Create(downstream).Error)

	// 消费 1000 → 分润 1000 * 0.1 = 100
	RecordAgentCommission(downstream.Id, 1000, "")

	var reloaded User
	require.NoError(t, DB.First(&reloaded, agent.Id).Error)
	assert.Equal(t, 100, reloaded.CommissionQuota, "commission balance")
	assert.Equal(t, 100, reloaded.CommissionHistoryQuota, "commission history")

	var records []Commission
	require.NoError(t, DB.Where("agent_id = ? AND from_user_id = ?", agent.Id, downstream.Id).Find(&records).Error)
	require.Len(t, records, 1)
	assert.Equal(t, 100, records[0].Quota)

	// 再消费 500 → +50，累加到 150
	RecordAgentCommission(downstream.Id, 500, "")
	require.NoError(t, DB.First(&reloaded, agent.Id).Error)
	assert.Equal(t, 150, reloaded.CommissionQuota)
	assert.Equal(t, 150, reloaded.CommissionHistoryQuota)
}

// TestRecordAgentCommission_NoOp 验证不该产生分润的场景全部为空操作：
// 上级非代理 / 代理分润率为 0 / 无上级。防止误发分润这一金额安全问题。
func TestRecordAgentCommission_NoOp(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	plainInviter := &User{Username: "comm_plain_inviter", AffCode: "jzlhcomm3", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(plainInviter).Error)
	downNotAgent := &User{Username: "comm_down_not_agent", AffCode: "jzlhcomm4", Status: common.UserStatusEnabled, InviterId: plainInviter.Id}
	require.NoError(t, DB.Create(downNotAgent).Error)

	zeroAgent := &User{Username: "comm_zero_agent", AffCode: "jzlhcomm5", Status: common.UserStatusEnabled, AgentType: "normal", UsageProfitRate: 0}
	require.NoError(t, DB.Create(zeroAgent).Error)
	downZeroRate := &User{Username: "comm_down_zero_rate", AffCode: "jzlhcomm6", Status: common.UserStatusEnabled, InviterId: zeroAgent.Id}
	require.NoError(t, DB.Create(downZeroRate).Error)

	noInviter := &User{Username: "comm_no_inviter", AffCode: "jzlhcomm7", Status: common.UserStatusEnabled, InviterId: 0}
	require.NoError(t, DB.Create(noInviter).Error)

	cases := []struct {
		name   string
		userID int
	}{
		{"inviter-not-agent", downNotAgent.Id},
		{"agent-rate-zero", downZeroRate.Id},
		{"no-inviter", noInviter.Id},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := countCommissions(t)
			RecordAgentCommission(tc.userID, 1000, "")
			assert.Equal(t, before, countCommissions(t), "no commission row should be created")
		})
	}

	// 分润率为 0 的代理余额始终为 0
	var za User
	require.NoError(t, DB.First(&za, zeroAgent.Id).Error)
	assert.Equal(t, 0, za.CommissionQuota)

	// 自邀守卫：inviter_id 指向自己时不计佣
	selfAgent := &User{Username: "comm_self_agent", AffCode: "jzlhcomm8", Status: common.UserStatusEnabled, AgentType: "normal", UsageProfitRate: 0.5}
	require.NoError(t, DB.Create(selfAgent).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", selfAgent.Id).Update("inviter_id", selfAgent.Id).Error)
	before := countCommissions(t)
	RecordAgentCommission(selfAgent.Id, 1000, "")
	assert.Equal(t, before, countCommissions(t), "self-invite must not earn commission")
}

// TestRecordAgentCommissionReversal 验证退款回冲：任务失败退款按比例冲减分润，
// 余额/累计同步下调并留负数流水——否则可靠"刷失败任务"白薅佣金。
func TestRecordAgentCommissionReversal(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	agent := &User{
		Username:        "agent_comm_rev",
		AffCode:         "jzlhcomm9",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.1,
	}
	require.NoError(t, DB.Create(agent).Error)
	downstream := &User{
		Username:  "down_comm_rev",
		AffCode:   "jzlhcomm10",
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
	}
	require.NoError(t, DB.Create(downstream).Error)

	// 消费 1000 → +100；退款 400 → -40
	RecordAgentCommission(downstream.Id, 1000, "")
	RecordAgentCommissionReversal(downstream.Id, 400, "")

	var r User
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 60, r.CommissionQuota)
	assert.Equal(t, 60, r.CommissionHistoryQuota)

	var neg []Commission
	require.NoError(t, DB.Where("agent_id = ? AND quota < 0", agent.Id).Find(&neg).Error)
	require.Len(t, neg, 1)
	assert.Equal(t, -40, neg[0].Quota)

	// 全额退款可把余额冲负（欠账抵扣后续分润）
	RecordAgentCommissionReversal(downstream.Id, 1000, "")
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, -40, r.CommissionQuota)
}

// TestGetAgentDownstreamUsersFieldWhitelist 安全契约：代理查看名下用户时,
// 不得泄露下级的 email / 管理员备注 等敏感列（代理是高级客户,不是管理员）。
func TestGetAgentDownstreamUsersFieldWhitelist(t *testing.T) {
	agent := &User{Username: "agent_wl", AffCode: "jzlhcomm11", Status: common.UserStatusEnabled, AgentType: "normal"}
	require.NoError(t, DB.Create(agent).Error)
	down := &User{
		Username:  "down_wl",
		AffCode:   "jzlhcomm12",
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
		Email:     "secret@example.com",
		Remark:    "内部备注-不可泄露",
		Quota:     123,
	}
	require.NoError(t, DB.Create(down).Error)

	users, total, err := GetAgentDownstreamUsers(agent.Id, "", 0, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, users, 1)
	got := users[0]
	assert.Equal(t, "down_wl", got.Username)
	assert.Equal(t, 123, got.Quota)
	assert.Empty(t, got.Email, "email must not leak to agent")
	assert.Empty(t, got.Remark, "admin remark must not leak to agent")
	assert.Empty(t, got.Setting, "setting must not leak to agent")
}

// TestRecordAgentCommission_SourceIdempotent 来源幂等：同一 sourceKey 重复结算只入账一次，
// 防重放/重复调用刷佣。不同 key 正常各记一笔。
func TestRecordAgentCommission_SourceIdempotent(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	agent := &User{Username: "agent_comm_idem", AffCode: "jzlhcomm13", Status: common.UserStatusEnabled, AgentType: "normal", UsageProfitRate: 0.1}
	require.NoError(t, DB.Create(agent).Error)
	down := &User{Username: "down_comm_idem", AffCode: "jzlhcomm14", Status: common.UserStatusEnabled, InviterId: agent.Id}
	require.NoError(t, DB.Create(down).Error)

	RecordAgentCommission(down.Id, 1000, "consume:req-idem-1")
	RecordAgentCommission(down.Id, 1000, "consume:req-idem-1") // 重放，必须无效
	RecordAgentCommission(down.Id, 1000, "consume:req-idem-2") // 新来源，正常入账

	var r User
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 200, r.CommissionQuota, "重复来源只应结算一次")
	var n int64
	require.NoError(t, DB.Model(&Commission{}).Where("agent_id = ?", agent.Id).Count(&n).Error)
	assert.EqualValues(t, 2, n)

	// 回冲同样幂等
	RecordAgentCommissionReversal(down.Id, 1000, "task:t1:6:1000")
	RecordAgentCommissionReversal(down.Id, 1000, "task:t1:6:1000")
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 100, r.CommissionQuota, "重复回冲只应生效一次")
}

// TestAgentCommissionMaturity 成熟期：开启后新分润先挂 pending(累计收益立即累加、
// 可提现不动)，超过成熟期经 MatureAgentCommissions 结转后才可提现。
func TestAgentCommissionMaturity(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	require.NoError(t, DB.AutoMigrate(&Commission{}))
	old := common.AgentCommissionMatureMinutes
	common.AgentCommissionMatureMinutes = 60
	defer func() { common.AgentCommissionMatureMinutes = old }()

	agent := &User{Username: "agent_comm_mature", AffCode: "jzlhcomm15", Status: common.UserStatusEnabled, AgentType: "normal", UsageProfitRate: 0.1}
	require.NoError(t, DB.Create(agent).Error)
	down := &User{Username: "down_comm_mature", AffCode: "jzlhcomm16", Status: common.UserStatusEnabled, InviterId: agent.Id}
	require.NoError(t, DB.Create(down).Error)

	RecordAgentCommission(down.Id, 1000, "")

	var r User
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 0, r.CommissionQuota, "成熟期内不可提现")
	assert.Equal(t, 100, r.CommissionHistoryQuota, "累计收益立即累加")
	assert.EqualValues(t, 100, GetAgentPendingCommission(agent.Id))

	// 未到期：结转应为空操作
	MatureAgentCommissions(agent.Id)
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 0, r.CommissionQuota)

	// 把流水时间拨回 2 小时前 → 结转生效
	require.NoError(t, DB.Model(&Commission{}).
		Where("agent_id = ? AND status = ?", agent.Id, CommissionStatusPending).
		Update("created_at", time.Now().Unix()-7200).Error)
	MatureAgentCommissions(agent.Id)
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 100, r.CommissionQuota, "到期后结转进可提现余额")
	assert.EqualValues(t, 0, GetAgentPendingCommission(agent.Id))

	// 再次结转幂等
	MatureAgentCommissions(agent.Id)
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 100, r.CommissionQuota)
}

// TestRecordAgentCommission_DisabledAgent 被封禁的代理不再产生新分润（冻结即断佣）。
func TestRecordAgentCommission_DisabledAgent(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	agent := &User{Username: "agent_comm_banned", AffCode: "jzlhcomm17", Status: common.UserStatusDisabled, AgentType: "normal", UsageProfitRate: 0.5}
	require.NoError(t, DB.Create(agent).Error)
	down := &User{Username: "down_comm_banned", AffCode: "jzlhcomm18", Status: common.UserStatusEnabled, InviterId: agent.Id}
	require.NoError(t, DB.Create(down).Error)

	before := countCommissions(t)
	RecordAgentCommission(down.Id, 1000, "")
	assert.Equal(t, before, countCommissions(t), "封禁代理不得计佣")
}

// TestGetAgentCommissionsFillsFromUsername 验证分润流水查询会批量回填来源用户名
// （代理钱包「分润明细」展示契约），且用户已删除时降级为空字符串而非报错。
func TestGetAgentCommissionsFillsFromUsername(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	agent := &User{
		Username:        "agent_comm_fill",
		AffCode:         "jzlhfill1",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.1,
	}
	require.NoError(t, DB.Create(agent).Error)
	downstream := &User{
		Username:  "down_comm_fill",
		AffCode:   "jzlhfill2",
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
	}
	require.NoError(t, DB.Create(downstream).Error)

	RecordAgentCommission(downstream.Id, 1000, "")
	// 来源用户不存在的孤儿流水：用户名应降级为空，不影响其他行。
	require.NoError(t, DB.Create(&Commission{
		AgentId:    agent.Id,
		FromUserId: 999999,
		Quota:      7,
		Status:     CommissionStatusMatured,
	}).Error)

	records, total, err := GetAgentCommissions(agent.Id, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, records, 2)
	byFrom := map[int]string{}
	for _, r := range records {
		byFrom[r.FromUserId] = r.FromUsername
	}
	assert.Equal(t, "down_comm_fill", byFrom[downstream.Id])
	assert.Equal(t, "", byFrom[999999], "orphan row degrades to empty username")
}

// TestReversalUsesOriginalRateSnapshot 验证退款回冲按原始入账费率快照计算：
// 消费与退款之间费率被改动、甚至代理被撤销/封禁，回冲金额仍与原分润一致。
func TestReversalUsesOriginalRateSnapshot(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	agent := &User{
		Username:        "agent_rev_snap",
		AffCode:         "jzlhrev1",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.2,
	}
	require.NoError(t, DB.Create(agent).Error)
	down := &User{
		Username:  "down_rev_snap",
		AffCode:   "jzlhrev2",
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
	}
	require.NoError(t, DB.Create(down).Error)

	// 任务消费 1000 @ 20% → +200
	RecordAgentCommission(down.Id, 1000, "task:snap1:2:1000")
	var r User
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 200, r.CommissionQuota)

	// 期间费率改为 5%，且代理被撤销 + 封禁
	require.NoError(t, DB.Model(&User{}).Where("id = ?", agent.Id).Updates(map[string]interface{}{
		"usage_profit_rate": 0.05,
		"agent_type":        "",
		"status":            common.UserStatusDisabled,
	}).Error)

	// 全额退款 → 应按原始 20% 回冲 200，而非当前 5% 的 50；且撤销/封禁不豁免回冲
	RecordAgentCommissionReversal(down.Id, 1000, "task:snap1:3:1000")
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 0, r.CommissionQuota, "reversal must use snapshot rate and ignore revocation")
	assert.Equal(t, 0, r.CommissionHistoryQuota)

	// 幂等：同一退款键重复回冲无效果
	RecordAgentCommissionReversal(down.Id, 1000, "task:snap1:3:1000")
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 0, r.CommissionQuota)
}

// TestReversalFallbackToCurrentRate 验证无任务来源键(非任务退款/旧数据)时，
// 回冲降级用代理当前费率。
func TestReversalFallbackToCurrentRate(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	agent := &User{
		Username:        "agent_rev_fb",
		AffCode:         "jzlhrev3",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.1,
		CommissionQuota: 500,
	}
	require.NoError(t, DB.Create(agent).Error)
	down := &User{
		Username:  "down_rev_fb",
		AffCode:   "jzlhrev4",
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
	}
	require.NoError(t, DB.Create(down).Error)

	RecordAgentCommissionReversal(down.Id, 1000, "")
	var r User
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 400, r.CommissionQuota, "fallback to current rate 0.1 → -100")
}

// TestCommissionComplianceGate 验证合规声明未确认时不产生新分润（与上游邀请返利
// 同一治理口径），确认后恢复入账；回冲不受合规开关限制（欠账永远可冲回）。
func TestCommissionComplianceGate(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}))

	agent := &User{
		Username:        "agent_compliance",
		AffCode:         "jzlhcp1",
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.1,
		CommissionQuota: 500,
	}
	require.NoError(t, DB.Create(agent).Error)
	down := &User{
		Username:  "down_compliance",
		AffCode:   "jzlhcp2",
		Status:    common.UserStatusEnabled,
		InviterId: agent.Id,
	}
	require.NoError(t, DB.Create(down).Error)

	// 未确认合规 → 消费不计佣
	ps := operation_setting.GetPaymentSetting()
	origConfirmed, origVersion := ps.ComplianceConfirmed, ps.ComplianceTermsVersion
	ps.ComplianceConfirmed = false
	t.Cleanup(func() {
		ps.ComplianceConfirmed = origConfirmed
		ps.ComplianceTermsVersion = origVersion
	})

	RecordAgentCommission(down.Id, 1000, "")
	var r User
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 500, r.CommissionQuota, "no accrual while compliance unconfirmed")

	// 回冲不受合规开关限制
	RecordAgentCommissionReversal(down.Id, 1000, "")
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 400, r.CommissionQuota, "reversal must work regardless of compliance flag")

	// 确认合规后恢复入账
	ps.ComplianceConfirmed = true
	ps.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	RecordAgentCommission(down.Id, 1000, "")
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 500, r.CommissionQuota)
}
