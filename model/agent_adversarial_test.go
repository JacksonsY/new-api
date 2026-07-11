package model

// jzlh-agent 对抗性测试：以"攻击者视角"尝试刷提现额度/双花/越权退款/重复入账,
// 断言每条攻击路径都被原子守卫挡住,且账本恒等式(可提现余额守恒)始终成立。

import (
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAdvAgent 造一个带指定可提现余额的代理,affCode 唯一避免与其他用例冲突。
func newAdvAgent(t *testing.T, name string, commission int) *User {
	t.Helper()
	u := &User{
		Username:        name,
		AffCode:         "adv_" + name,
		Status:          common.UserStatusEnabled,
		AgentType:       "normal",
		UsageProfitRate: 0.1,
		CommissionQuota: commission,
	}
	require.NoError(t, DB.Create(u).Error)
	return u
}

// TestAdv_ConcurrentWithdrawSameBalance 攻击:开 N 个并发提现,每个都想提走全部余额。
// 期望:原子守卫保证成功笔数受余额约束,预扣总额恰好=初始余额,无超取。
func TestAdv_ConcurrentWithdrawSameBalance(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0) // 关闭最低额/未决上限,纯测并发守卫

	const balance = 1000
	const perWithdraw = 400 // 最多容纳 2 笔(800<=1000),第 3 笔起必失败
	u := newAdvAgent(t, "conc_wd", balance)

	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := CreateWithdrawal(u.Id, perWithdraw, "alipay", "张三", "13800001111", "")
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	// 不变量:成功笔数 * 单笔 = 已预扣; 余额 = 初始 - 已预扣; 二者守恒且不为负。
	assert.Equal(t, 2, success, "exactly 2 withdrawals fit in balance")
	assert.Equal(t, balance-success*perWithdraw, r.CommissionQuota)
	assert.GreaterOrEqual(t, r.CommissionQuota, 0, "balance must never go negative")

	var held int64
	DB.Model(&Withdrawal{}).Where("user_id = ?", u.Id).
		Select("COALESCE(SUM(amount),0)").Row().Scan(&held)
	assert.Equal(t, int64(success*perWithdraw), held, "held total matches deducted")
}

// TestAdv_ConvertAndWithdrawNoDoubleSpend 攻击:并发把同一笔余额既转成额度又提现。
// 期望:两条出口共用同一原子守卫,总流出恰好=余额,不会两头都拿到钱。
func TestAdv_ConvertAndWithdrawNoDoubleSpend(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	const balance = 500
	u := newAdvAgent(t, "conv_wd_race", balance)
	u.Quota = 0
	require.NoError(t, DB.Model(u).Update("quota", 0).Error)

	var wg sync.WaitGroup
	wg.Add(2)
	var convErr, wdErr error
	go func() { defer wg.Done(); convErr = ConvertCommissionToQuota(u.Id, balance) }()
	go func() {
		defer wg.Done()
		_, wdErr = CreateWithdrawal(u.Id, balance, "alipay", "张三", "13800001111", "")
	}()
	wg.Wait()

	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	// 恰好一个成功:要么转成额度(quota=balance,commission=0),要么提现(commission=0,预扣一笔)。
	oneSucceeded := (convErr == nil) != (wdErr == nil)
	assert.True(t, oneSucceeded, "exactly one exit may consume the balance")
	assert.Equal(t, 0, r.CommissionQuota, "balance fully consumed once, never negative")
	if convErr == nil {
		assert.Equal(t, balance, r.Quota, "converted to api quota")
	} else {
		assert.Equal(t, 0, r.Quota, "not converted")
	}
}

// TestAdv_WithdrawCancelLoopConservesBalance 攻击:反复 提现→撤销 想凭空放大余额。
// 期望:撤销原子退回预扣,任意轮次后余额恒等于初始值,一分不多一分不少。
func TestAdv_WithdrawCancelLoopConservesBalance(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	const balance = 1000
	u := newAdvAgent(t, "wd_cancel_loop", balance)

	for i := 0; i < 20; i++ {
		w, err := CreateWithdrawal(u.Id, 700, "alipay", "张三", "13800001111", "")
		require.NoError(t, err)
		var mid User
		require.NoError(t, DB.First(&mid, u.Id).Error)
		assert.Equal(t, balance-700, mid.CommissionQuota, "held during pending")
		require.NoError(t, CancelWithdrawal(u.Id, w.Id))
		var after User
		require.NoError(t, DB.First(&after, u.Id).Error)
		assert.Equal(t, balance, after.CommissionQuota, "refunded exactly, no inflation")
	}
}

// TestAdv_CancelVsRejectSingleRefund 攻击:同一提现单并发 撤销 + 拒绝,想触发双重退款。
// 期望:状态 CAS 保证只有一方赢得终态迁移,退款恰好一次,余额=初始(退回一次),不翻倍。
func TestAdv_CancelVsRejectSingleRefund(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	const balance = 1000
	const amount = 600
	for iter := 0; iter < 5; iter++ {
		u := newAdvAgent(t, fmt.Sprintf("cancel_reject_%d", iter), balance)
		w, err := CreateWithdrawal(u.Id, amount, "alipay", "张三", "13800001111", "")
		require.NoError(t, err)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = CancelWithdrawal(u.Id, w.Id) }()
		go func() { defer wg.Done(); _ = ReviewWithdrawal(w.Id, "reject", 999, "") }()
		wg.Wait()

		var r User
		require.NoError(t, DB.First(&r, u.Id).Error)
		// 无论谁赢,预扣都只退一次:余额回到初始,绝不出现 initial+amount(双退)。
		assert.Equal(t, balance, r.CommissionQuota,
			"refund must happen exactly once (no double refund)")
	}
}

// TestAdv_ConcurrentClaimSingleWinner 攻击:多个管理员并发认领同一提现单。
// 期望:只有一人认领成功,reviewer_id 落在赢家,其余得到"已处理"。
func TestAdv_ConcurrentClaimSingleWinner(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	u := newAdvAgent(t, "claim_race", 1000)
	w, err := CreateWithdrawal(u.Id, 500, "alipay", "张三", "13800001111", "")
	require.NoError(t, err)

	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	adminIds := []int{101, 102, 103, 104, 105}
	for _, aid := range adminIds {
		wg.Add(1)
		go func(adminId int) {
			defer wg.Done()
			if err := ReviewWithdrawal(w.Id, "claim", adminId, ""); err == nil {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(aid)
	}
	wg.Wait()

	assert.Equal(t, 1, winners, "exactly one admin can claim")
	var stored Withdrawal
	require.NoError(t, DB.First(&stored, w.Id).Error)
	assert.Equal(t, WithdrawalProcessing, stored.Status)
	assert.Contains(t, adminIds, stored.ReviewerId, "reviewer is one of the claimers")
}

// TestAdv_NonClaimerCannotDoublePay 攻击(核心防重复打款):A 认领并线下打款后,
// B 试图拒绝退款,造成"既打款又退回余额"。期望:B 的拒绝被挡,余额保持预扣不退。
func TestAdv_NonClaimerCannotDoublePay(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 0, 0, 0)

	const balance = 1000
	const amount = 600
	u := newAdvAgent(t, "double_pay", balance)
	w, err := CreateWithdrawal(u.Id, amount, "alipay", "张三", "13800001111", "")
	require.NoError(t, err)

	require.NoError(t, ReviewWithdrawal(w.Id, "claim", 101, "")) // A 认领
	// A 已线下转账(系统外),此刻 B 想拒绝退款 → 必须被挡,否则代理拿钱又拿回余额
	err = ReviewWithdrawal(w.Id, "reject", 102, "B恶意拒绝")
	assert.ErrorIs(t, err, ErrWithdrawalClaimedByOther)

	var r User
	require.NoError(t, DB.First(&r, u.Id).Error)
	assert.Equal(t, balance-amount, r.CommissionQuota,
		"balance stays held; must NOT be refunded while A is paying out")
	var stored Withdrawal
	require.NoError(t, DB.First(&stored, w.Id).Error)
	assert.Equal(t, WithdrawalProcessing, stored.Status, "row untouched by non-claimer reject")
}

// TestAdv_MaturityNoDoubleCredit 攻击:并发触发成熟结转,想让同一笔 pending 分润
// 被计入可提现余额两次。期望:逐行 CAS 保证只结转一次。
func TestAdv_MaturityNoDoubleCredit(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	confirmPaymentComplianceForTest(t)

	origMin := common.AgentCommissionMatureMinutes
	common.AgentCommissionMatureMinutes = 1 // 开启成熟期,让入账先挂 pending
	t.Cleanup(func() { common.AgentCommissionMatureMinutes = origMin })

	agent := newAdvAgent(t, "mature_race", 0)
	agent.CommissionQuota = 0
	require.NoError(t, DB.Model(agent).Update("commission_quota", 0).Error)
	down := &User{Username: "mature_down", AffCode: "adv_mature_down",
		Status: common.UserStatusEnabled, InviterId: agent.Id}
	require.NoError(t, DB.Create(down).Error)

	// 造一条已过成熟期的 pending 分润(created_at 拨早)
	require.NoError(t, DB.Create(&Commission{
		AgentId: agent.Id, FromUserId: down.Id, Quota: 300,
		Status: CommissionStatusPending, CreatedAt: 1,
	}).Error)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); MatureAgentCommissions(agent.Id) }()
	}
	wg.Wait()

	var r User
	require.NoError(t, DB.First(&r, agent.Id).Error)
	assert.Equal(t, 300, r.CommissionQuota, "matured exactly once, not multiplied by racers")
}

// TestAdv_ReversalReplayIdempotent 攻击:用同一 source_key 重复回冲,想把代理余额冲穿。
// 期望:source_key 幂等,重复回冲无效果。
func TestAdv_ReversalReplayIdempotent(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}))
	confirmPaymentComplianceForTest(t)

	agent := newAdvAgent(t, "rev_replay", 1000)
	down := &User{Username: "rev_replay_down", AffCode: "adv_rev_down",
		Status: common.UserStatusEnabled, InviterId: agent.Id}
	require.NoError(t, DB.Create(down).Error)

	RecordAgentCommission(down.Id, 1000, "task:replaytask:2:1000")
	key := "task:replaytask:3:1000"
	for i := 0; i < 5; i++ {
		RecordAgentCommissionReversal(down.Id, 1000, key)
	}
	var r User
	require.NoError(t, DB.First(&r, agent.Id).Error)
	// 原入账 +100，单次回冲 -100；5 次重放后仍回到初始余额。
	assert.Equal(t, 1000, r.CommissionQuota, "replayed reversal applies exactly once")
}

// TestAdv_WithdrawInputBoundaries 防呆边界:零/负/低于最低额/恰好最低额/超余额。
func TestAdv_WithdrawInputBoundaries(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}, &Withdrawal{}))
	setWithdrawTestPolicy(t, 100, 0, 0) // 最低额 100

	u := newAdvAgent(t, "boundaries", 1000)

	cases := []struct {
		name    string
		amount  int
		wantErr error
	}{
		{"zero", 0, ErrInvalidWithdrawalAmount},
		{"negative", -50, ErrInvalidWithdrawalAmount},
		{"below minimum", 99, ErrWithdrawalBelowMinimum},
		{"exactly minimum", 100, nil},
		{"above balance", 100000, ErrInsufficientCommission},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var before User
			require.NoError(t, DB.First(&before, u.Id).Error)
			_, err := CreateWithdrawal(u.Id, c.amount, "alipay", "张三", "13800001111", "")
			if c.wantErr == nil {
				require.NoError(t, err)
				// 退回预扣以隔离后续用例
				require.NoError(t, DB.Model(&User{}).Where("id = ?", u.Id).
					Update("commission_quota", before.CommissionQuota).Error)
			} else {
				assert.ErrorIs(t, err, c.wantErr)
				var after User
				require.NoError(t, DB.First(&after, u.Id).Error)
				assert.Equal(t, before.CommissionQuota, after.CommissionQuota,
					"rejected input must not move balance")
			}
		})
	}
}

// TestAdv_SelfAndUnboundNoCommission 防刷:无上级/自邀不产生分润。
func TestAdv_SelfAndUnboundNoCommission(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Commission{}))
	confirmPaymentComplianceForTest(t)

	// 无上级用户消费 → 无分润
	lone := &User{Username: "lone_spender", AffCode: "adv_lone",
		Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(lone).Error)
	before := countCommissions(t)
	RecordAgentCommission(lone.Id, 100000, "consume:x1")
	assert.Equal(t, before, countCommissions(t), "no inviter → no commission")

	// 自邀(inviter=self)→ 无分润
	selfAgent := &User{Username: "self_agent", AffCode: "adv_self",
		Status: common.UserStatusEnabled, AgentType: "normal", UsageProfitRate: 0.5}
	require.NoError(t, DB.Create(selfAgent).Error)
	require.NoError(t, DB.Model(selfAgent).Update("inviter_id", selfAgent.Id).Error)
	before = countCommissions(t)
	RecordAgentCommission(selfAgent.Id, 100000, "consume:x2")
	assert.Equal(t, before, countCommissions(t), "self-referral → no commission")
}
