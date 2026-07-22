package service

// jzlh-sub 子账号计费端到端：验证子号请求扣主号钱包(共享池)、子号个人钱包不参与、
// 三档额度触顶拦截且不扣款。是 §5.2 付款人解析 + trySubAccount 的资金归属护栏。

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func makeBillingUser(t *testing.T, parentId, quota, dayLimit int) *model.User {
	t.Helper()
	s := common.GetRandomString(8)
	u := &model.User{
		Username:        "bu_" + s,
		Email:           "bu_" + s + "@sub.local",
		AffCode:         "aff_" + s,
		ParentId:        parentId,
		Quota:           quota,
		Status:          common.UserStatusEnabled,
		Role:            common.RoleCommonUser,
		TotalQuotaLimit: -1,
		MonthQuotaLimit: -1,
		DayQuotaLimit:   dayLimit,
	}
	require.NoError(t, model.DB.Create(u).Error)
	return u
}

func newSubRelayInfo(subId, parentId int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:       subId,
		ParentId:     parentId,
		IsPlayground: true, // 跳过令牌预扣，聚焦资金归属
		RequestId:    "test-sub-" + common.GetRandomString(6),
	}
}

// 子号请求扣主号钱包，子号个人钱包不变。
func TestSubAccountBillingChargesParentWallet(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	parent := makeBillingUser(t, 0, 1000, -1)
	sub := makeBillingUser(t, parent.Id, 0, -1)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	session, apiErr := NewBillingSession(c, newSubRelayInfo(sub.Id, parent.Id), 100)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	assert.Equal(t, BillingSourceWallet, session.funding.Source())

	pq, err := model.GetUserQuota(parent.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 900, pq, "子号请求应扣主号钱包 100")
	sq, err := model.GetUserQuota(sub.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 0, sq, "子号个人钱包不参与，应保持 0")
}

// 子号三档额度触顶：预扣被拦，主号钱包不被扣。
func TestSubAccountBillingTierBlocksAndNoCharge(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	parent := makeBillingUser(t, 0, 1000, -1)
	sub := makeBillingUser(t, parent.Id, 0, 50) // 日额度上限 50

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	session, apiErr := NewBillingSession(c, newSubRelayInfo(sub.Id, parent.Id), 100) // 100 > 日 50
	require.NotNil(t, apiErr, "超日额度应被拦截")
	assert.Nil(t, session)

	pq, err := model.GetUserQuota(parent.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 1000, pq, "被拦截时主号钱包不应被扣")
}

// 共享池预扣必须原子：strict WalletFunding 超额时原子拒绝、余额不变；
// 非 strict(个人钱包旧口径)可透支——防并发子号把主号池扣成负(fork 审计 MEDIUM)。
func TestWalletFundingStrictRejectsOverdraw(t *testing.T) {
	truncate(t)

	u := makeBillingUser(t, 0, 50, -1)
	strict := &WalletFunding{userId: u.Id, strict: true}
	require.Error(t, strict.PreConsume(100), "strict 超额预扣应被原子拒绝")
	q, err := model.GetUserQuota(u.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 50, q, "strict 拒绝后余额不变")

	u2 := makeBillingUser(t, 0, 50, -1)
	loose := &WalletFunding{userId: u2.Id}
	require.NoError(t, loose.PreConsume(100), "非 strict 沿用旧口径,不拒绝")
	q2, err := model.GetUserQuota(u2.Id, false)
	require.NoError(t, err)
	assert.Equal(t, -50, q2, "非 strict 可透支(记录差异)")
}

// 主号被封禁后子号计费必须同步止血：否则封禁主号形同虚设，子号可继续烧被封主号的钱包。
func TestSubAccountBillingBlockedWhenParentDisabled(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	parent := makeBillingUser(t, 0, 1000, -1)
	sub := makeBillingUser(t, parent.Id, 0, -1)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", parent.Id).
		Update("status", common.UserStatusDisabled).Error)
	require.NoError(t, model.InvalidateUserCache(parent.Id))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	session, apiErr := NewBillingSession(c, newSubRelayInfo(sub.Id, parent.Id), 100)
	require.NotNil(t, apiErr, "主号被禁用后子号计费应被拦截")
	assert.Nil(t, session)

	pq, err := model.GetUserQuota(parent.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 1000, pq, "被拦截时不应扣款")
}

// 主号钱包不足：子号请求被拦，返回主账号额度不足。
func TestSubAccountBillingParentInsufficient(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	parent := makeBillingUser(t, 0, 50, -1) // 主号池仅 50
	sub := makeBillingUser(t, parent.Id, 0, -1)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	session, apiErr := NewBillingSession(c, newSubRelayInfo(sub.Id, parent.Id), 100)
	require.NotNil(t, apiErr, "主号池不足应被拦截")
	assert.Nil(t, session)
}
