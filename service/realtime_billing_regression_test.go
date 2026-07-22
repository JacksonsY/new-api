package service

// realtime WSS 计费回归：中间轮扣款必须经 BillingSession.Reserve 累进预留，
// 收尾 SettleBilling(整场量) 只结差额——整场只收一次钱。
// 修复前 PreWssConsumeQuota 逐轮绕过会话直扣钱包，收尾又按整场量 Settle，
// 按 token 计费的 realtime 会话被实测 100% 双扣。

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/model"
)

func TestRealtimeSessionSettleNoDoubleCharge(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)

	const startQuota = 1_000_000
	u := makeBillingUser(t, 0, startQuota, -1)

	relayInfo := newSubRelayInfo(u.Id, 0) // 普通用户；IsPlayground 跳过令牌侧，聚焦资金
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	// ① 会话建立预扣 50（controller/relay.go PreConsumeBilling）
	apiErr := PreConsumeBilling(c, 50, relayInfo)
	require.Nil(t, apiErr)
	require.NotNil(t, relayInfo.Billing)

	// ② 两个 response.done 轮次各 100：修复后 PreWssConsumeQuota 走 Reserve 累进
	require.NoError(t, relayInfo.Billing.Reserve(relayInfo.Billing.GetPreConsumedQuota()+100))
	require.NoError(t, relayInfo.Billing.Reserve(relayInfo.Billing.GetPreConsumedQuota()+100))

	// ③ 会话结束：整场 quota=200，SettleBilling 结差额（应退回多预留的 50）
	require.NoError(t, SettleBilling(c, relayInfo, 200))

	q, err := model.GetUserQuota(u.Id, false)
	require.NoError(t, err)
	assert.Equal(t, startQuota-200, q, "整场消耗 200 只应扣 200——预扣+轮次预留全部在结算中对冲")
}
