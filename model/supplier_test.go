package model

// jzlh-supplier 结算不变量 + 审核门回归测试（守 review R2/R4/R3）。

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// swapToFreshSupplierDB 换一个隔离的内存库并迁移所需表，返回还原函数。
func swapToFreshSupplierDB(t *testing.T, models ...interface{}) func() {
	t.Helper()
	oldDB, oldLog := DB, LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(models...))
	DB = db
	LOG_DB = db
	return func() { DB = oldDB; LOG_DB = oldLog }
}

// TestSupplierSettlementClampFilterMaturity 一次覆盖三个不变量：
//   - min(channel_quota, quota) 夹逼（R4：平台毛利非负）
//   - token_id>0 过滤渠道测试（R2：不把测渠道算成供应商收益）
//   - 成熟窗口（未够老的收益不计入 matured/payable）
func TestSupplierSettlementClampFilterMaturity(t *testing.T) {
	restore := swapToFreshSupplierDB(t, &Log{}, &SupplierLedger{})
	defer restore()

	supplierId := 42
	now := common.GetTimestamp()
	old := now - 10*86400 // 远早于成熟窗口(3天)

	seed := []Log{
		// 成熟 + min 取 channel_quota=200
		{SupplierId: supplierId, Type: LogTypeConsume, TokenId: 1, ChannelQuota: 200, Quota: 500, CreatedAt: old},
		// 成熟 + channel_quota>quota → 夹逼到 quota=500
		{SupplierId: supplierId, Type: LogTypeConsume, TokenId: 1, ChannelQuota: 800, Quota: 500, CreatedAt: old},
		// 渠道测试：token_id=0 → 排除
		{SupplierId: supplierId, Type: LogTypeConsume, TokenId: 0, ChannelQuota: 999, Quota: 999, CreatedAt: old},
		// 最近：算 gross(min=100) 但未成熟
		{SupplierId: supplierId, Type: LogTypeConsume, TokenId: 1, ChannelQuota: 100, Quota: 100, CreatedAt: now},
		// 别的供应商 → 排除
		{SupplierId: 999, Type: LogTypeConsume, TokenId: 1, ChannelQuota: 50, Quota: 50, CreatedAt: old},
		// 非消费日志 → 排除
		{SupplierId: supplierId, Type: LogTypeManage, TokenId: 1, ChannelQuota: 77, Quota: 77, CreatedAt: old},
	}
	for i := range seed {
		require.NoError(t, LOG_DB.Create(&seed[i]).Error)
	}
	require.NoError(t, (&SupplierLedger{SupplierId: supplierId, Type: SupplierLedgerTypePayout, Quota: 100}).Insert())

	s, err := GetSupplierSettlement(supplierId)
	require.NoError(t, err)

	// gross = 200 + 500 + 100(最近) = 800（token_id=0、别的供应商、非消费 全排除）
	assert.Equal(t, int64(800), s.GrossQuota)
	// matured = 200 + 500 = 700（最近 100 未成熟）
	assert.Equal(t, int64(700), s.MaturedQuota)
	assert.Equal(t, int64(100), s.PaidQuota)
	// payable = matured - paid - confiscated = 700 - 100 - 0 = 600
	assert.Equal(t, int64(600), s.PayableQuota)
}

// TestEnsureSupplierApplied 申请即渠道要约的状态机：None→Pending、pending/approved 放行
// （支持多次申请）、Suspended 拒、未知用户报错。
func TestEnsureSupplierApplied(t *testing.T) {
	restore := swapToFreshSupplierDB(t, &User{})
	defer restore()

	require.NoError(t, DB.Create(&User{Id: 1, Username: "ens1", SupplierStatus: SupplierStatusNone}).Error)

	// None → Pending
	require.NoError(t, EnsureSupplierApplied(1))
	var got User
	require.NoError(t, DB.First(&got, 1).Error)
	assert.Equal(t, SupplierStatusPending, got.SupplierStatus)

	// Pending / Approved → 放行（多次申请）
	require.NoError(t, EnsureSupplierApplied(1))
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 1).Update("supplier_status", SupplierStatusApproved).Error)
	require.NoError(t, EnsureSupplierApplied(1))

	// Suspended → 拒绝
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 1).Update("supplier_status", SupplierStatusSuspended).Error)
	assert.ErrorIs(t, EnsureSupplierApplied(1), ErrSupplierSuspended)

	// 未知用户 → 报错
	assert.Error(t, EnsureSupplierApplied(999))

	// 管理员及以上 → 拒绝（裁判/运动员），且不写入任何供应商状态
	require.NoError(t, DB.Create(&User{Id: 2, Username: "ens_admin", AffCode: "ens_admin", Role: common.RoleAdminUser, SupplierStatus: SupplierStatusNone}).Error)
	assert.ErrorIs(t, EnsureSupplierApplied(2), ErrSupplierAdminForbidden)
	var admin User
	require.NoError(t, DB.First(&admin, 2).Error)
	assert.Equal(t, SupplierStatusNone, admin.SupplierStatus)
}

// TestRecordSupplierPayoutRejectsOverpay 打款不得超过可打款额。
func TestRecordSupplierPayoutRejectsOverpay(t *testing.T) {
	restore := swapToFreshSupplierDB(t, &Log{}, &SupplierLedger{}, &User{})
	defer restore()

	supplierId := 7
	// 打款/罚没现在锁供应商 User 行以串行化，故 fixture 需存在该用户。
	require.NoError(t, DB.Create(&User{Id: supplierId, Username: "sup7", SupplierStatus: SupplierStatusApproved}).Error)
	old := common.GetTimestamp() - 10*86400
	require.NoError(t, LOG_DB.Create(&Log{
		SupplierId: supplierId, Type: LogTypeConsume, TokenId: 1,
		ChannelQuota: 300, Quota: 500, CreatedAt: old,
	}).Error)

	// 可打款 = 300。超额打款应拒绝。
	require.Error(t, RecordSupplierPayout(supplierId, 301, "", "", 1))
	// 等额打款应成功。
	require.NoError(t, RecordSupplierPayout(supplierId, 300, "v1", "ok", 1))
	// 再打款 1 应拒绝（已打满）。
	require.Error(t, RecordSupplierPayout(supplierId, 1, "", "", 1))
}

// TestRecordSupplierPayoutConcurrentNoOverpay 攻击：开 N 个并发满额打款，期望原子守卫
// （锁供应商行 + 事务内重算）保证只成功 1 笔、已打款总额不超过 payable，不出现 TOCTOU 超付。
// 用全局测试库（TestMain：DB==LOG_DB、MaxOpenConns=1），与代理并发对抗测试同法；
// swapToFreshSupplierDB 的私有 :memory: 连接池不适合并发。
func TestRecordSupplierPayoutConcurrentNoOverpay(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&SupplierLedger{})) // User/Log 已由 TestMain 迁移
	// 完整清理前置：全局库跨 -count 重跑时，若 auto-increment id 被复用，旧 log/ledger 会
	// 在同一 supplier_id 下累积。先删旧用户的 log+ledger 再硬删用户，保证每次从零开始。
	var prior User
	if DB.Unscoped().Where("username = ?", "sup_conc_payout").First(&prior).Error == nil {
		LOG_DB.Where("supplier_id = ?", prior.Id).Delete(&Log{})
		DB.Where("supplier_id = ?", prior.Id).Delete(&SupplierLedger{})
		DB.Unscoped().Delete(&User{}, prior.Id)
	}
	u := &User{Username: "sup_conc_payout", AffCode: "sup_conc_payout", SupplierStatus: SupplierStatusApproved}
	require.NoError(t, DB.Create(u).Error)
	old := common.GetTimestamp() - 10*86400
	require.NoError(t, LOG_DB.Create(&Log{
		SupplierId: u.Id, Type: LogTypeConsume, TokenId: 1,
		ChannelQuota: 500, Quota: 1000, CreatedAt: old,
	}).Error) // payable = min(500,1000) = 500，已成熟

	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := RecordSupplierPayout(u.Id, 500, "v", "", 1); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, success, "并发满额打款只应成功 1 笔")
	paid, err := GetSupplierPaidQuota(u.Id)
	require.NoError(t, err)
	assert.LessOrEqual(t, paid, int64(500), "已打款总额不得超过 payable")
}

// TestAddAbilitiesRespectsAuditGate 审核门：未过审渠道不进路由池。
func TestAddAbilitiesRespectsAuditGate(t *testing.T) {
	restore := swapToFreshSupplierDB(t, &Ability{})
	defer restore()

	p := int64(0)
	w := uint(0)
	ch := &Channel{
		Id: 1, Models: "gpt-4", Group: "default",
		Status: common.ChannelStatusEnabled, Priority: &p, Weight: &w,
		AuditStatus: ChannelAuditPending,
	}
	require.NoError(t, ch.AddAbilities(nil))
	var count int64
	DB.Model(&Ability{}).Where("channel_id = ?", 1).Count(&count)
	assert.Equal(t, int64(0), count, "pending 渠道不应有 abilities")

	ch.AuditStatus = ChannelAuditApproved
	require.NoError(t, ch.AddAbilities(nil))
	DB.Model(&Ability{}).Where("channel_id = ?", 1).Count(&count)
	assert.Equal(t, int64(1), count, "approved 渠道应生成 abilities")
}

// TestMaterialChannelFieldsChanged 改动复审触发条件（R3）。
func TestMaterialChannelFieldsChanged(t *testing.T) {
	base := "https://a.com"
	old := &Channel{Key: "k1", BaseURL: &base, Models: "gpt-4"}
	cases := []struct {
		name             string
		key, url, models string
		want             bool
	}{
		{"空 key + 同 url/models = 不变", "", "https://a.com", "gpt-4", false},
		{"同 key 显式 + 同 url/models = 不变", "k1", "https://a.com", "gpt-4", false},
		{"key 变", "k2", "https://a.com", "gpt-4", true},
		{"base_url 变", "", "https://b.com", "gpt-4", true},
		{"models 变", "", "https://a.com", "gpt-4,gpt-5", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, MaterialChannelFieldsChanged(old, tc.key, tc.url, tc.models))
		})
	}
}
