package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEpayPlatform 按商户单号返回预设查单响应（v1 协议 api.php?act=order）。
func fakeEpayPlatform(t *testing.T, orders map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "order", r.URL.Query().Get("act"))
		outTradeNo := r.URL.Query().Get("out_trade_no")
		response, ok := orders[outTradeNo]
		if !ok {
			response = `{"code":-1,"msg":"order not found"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
}

func setupEpayReconcileTest(t *testing.T, serverURL string) {
	t.Helper()
	origAddress, origId, origKey, origVersion := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.EpayApiVersion
	operation_setting.PayAddress = serverURL
	operation_setting.EpayId = "1000"
	operation_setting.EpayKey = "SECRET"
	operation_setting.EpayApiVersion = "v1"
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, operation_setting.EpayApiVersion = origAddress, origId, origKey, origVersion
	})
}

func seedReconcileTopUp(t *testing.T, tradeNo string, userId int, money float64, provider string) *model.TopUp {
	t.Helper()
	topUp := &model.TopUp{
		UserId:          userId,
		Amount:          10,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentProvider: provider,
		PaymentMethod:   "alipay",
		CreateTime:      common.GetTimestamp() - 120,
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(topUp).Error)
	t.Cleanup(func() { model.DB.Unscoped().Delete(&model.TopUp{}, topUp.Id) })
	return topUp
}

func seedReconcileSubscription(t *testing.T, tradeNo string, userId int, money float64, provider string) *model.SubscriptionOrder {
	t.Helper()
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          1,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentProvider: provider,
		PaymentMethod:   "alipay",
		CreateTime:      common.GetTimestamp() - 120,
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(order).Error)
	t.Cleanup(func() { model.DB.Unscoped().Delete(&model.SubscriptionOrder{}, order.Id) })
	return order
}

func TestReconcileEpayOrdersSharesLimitAcrossOrderKinds(t *testing.T) {
	user := &model.User{
		Username: "epay-limit-" + common.GetUUID()[:8], Password: "x",
		Role: common.RoleCommonUser, Group: "default", Status: common.UserStatusEnabled,
		AffCode: common.GetUUID()[:8],
	}
	require.NoError(t, model.DB.Create(user).Error)
	t.Cleanup(func() { model.DB.Unscoped().Delete(&model.User{}, user.Id) })

	topUp := seedReconcileTopUp(t, "REC-LIMIT-TOPUP", user.Id, 1, model.PaymentProviderEpay)
	order := seedReconcileSubscription(t, "REC-LIMIT-SUB", user.Id, 1, model.PaymentProviderEpay)
	server := fakeEpayPlatform(t, map[string]string{
		topUp.TradeNo: `{"code":1,"trade_no":"P-LIMIT-1","out_trade_no":"REC-LIMIT-TOPUP","pid":"1000","type":"alipay","money":"1.00","status":0}`,
		order.TradeNo: `{"code":1,"trade_no":"P-LIMIT-2","out_trade_no":"REC-LIMIT-SUB","pid":"1000","type":"alipay","money":"1.00","status":0}`,
	})
	defer server.Close()
	setupEpayReconcileTest(t, server.URL)

	summary, err := ReconcileEpayOrders(0, 3600, 1, true)
	require.NoError(t, err)
	assert.Len(t, summary.Items, 1, "limit is the total batch budget, not a per-table limit")
}

// 对账是资金操作的最后防线：平台确认已支付且五重校验通过的漏单必须补上账，
// 金额不符/未支付/非 epay 单一律不动。
func TestReconcileEpayOrdersEndToEnd(t *testing.T) {
	user := &model.User{Username: fmt.Sprintf("epay-rec-%s", common.GetUUID()[:8]), Password: "x", Role: common.RoleCommonUser, Quota: 0, Group: "default", Status: common.UserStatusEnabled, AffCode: common.GetUUID()[:8]}
	require.NoError(t, model.DB.Create(user).Error)
	t.Cleanup(func() { model.DB.Unscoped().Delete(&model.User{}, user.Id) })

	paid := seedReconcileTopUp(t, "REC-PAID-1", user.Id, 1.00, model.PaymentProviderEpay)
	wrongMoney := seedReconcileTopUp(t, "REC-MONEY-1", user.Id, 2.00, model.PaymentProviderEpay)
	nonFiniteMoney := seedReconcileTopUp(t, "REC-NAN-1", user.Id, 1.00, model.PaymentProviderEpay)
	unpaid := seedReconcileTopUp(t, "REC-UNPAID-1", user.Id, 3.00, model.PaymentProviderEpay)
	stripeOrder := seedReconcileTopUp(t, "REC-STRIPE-1", user.Id, 4.00, model.PaymentProviderStripe)

	server := fakeEpayPlatform(t, map[string]string{
		"REC-PAID-1":   `{"code":1,"trade_no":"P1","out_trade_no":"REC-PAID-1","pid":"1000","type":"alipay","money":"1.00","status":1}`,
		"REC-MONEY-1":  `{"code":1,"trade_no":"P2","out_trade_no":"REC-MONEY-1","pid":"1000","type":"alipay","money":"99.00","status":1}`,
		"REC-NAN-1":    `{"code":1,"trade_no":"P4","out_trade_no":"REC-NAN-1","pid":"1000","type":"alipay","money":"NaN","status":1}`,
		"REC-UNPAID-1": `{"code":1,"trade_no":"P3","out_trade_no":"REC-UNPAID-1","pid":"1000","type":"alipay","money":"3.00","status":0}`,
	})
	defer server.Close()
	setupEpayReconcileTest(t, server.URL)

	// dry_run：只报告，分文不动
	drySummary, err := ReconcileEpayOrders(0, 3600, 100, true)
	require.NoError(t, err)
	assert.Equal(t, 4, drySummary.Scanned, "只扫 epay 单，stripe 单不进对账")
	assert.Equal(t, 0, drySummary.Completed)
	actions := map[string]string{}
	for _, item := range drySummary.Items {
		actions[item.TradeNo] = item.Action
	}
	assert.Equal(t, EpayReconcileActionWouldComplete, actions["REC-PAID-1"])
	assert.Equal(t, EpayReconcileActionSkipped, actions["REC-MONEY-1"])
	assert.Equal(t, EpayReconcileActionSkipped, actions["REC-NAN-1"])
	assert.Equal(t, EpayReconcileActionSkipped, actions["REC-UNPAID-1"])
	var afterDry model.TopUp
	require.NoError(t, model.DB.First(&afterDry, paid.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, afterDry.Status, "dry_run 不得动单")

	// 真对账：已支付且校验全过的补上账
	summary, err := ReconcileEpayOrders(0, 3600, 100, false)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Completed)

	var completed model.TopUp
	require.NoError(t, model.DB.First(&completed, paid.Id).Error)
	assert.Equal(t, common.TopUpStatusSuccess, completed.Status, "漏单必须被补成 success")

	var reloadedUser model.User
	require.NoError(t, model.DB.First(&reloadedUser, user.Id).Error)
	assert.Equal(t, int(10*common.QuotaPerUnit), reloadedUser.Quota, "额度必须真实入账")

	var untouchedWrong model.TopUp
	require.NoError(t, model.DB.First(&untouchedWrong, wrongMoney.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, untouchedWrong.Status, "金额不符必须分文不动")
	var untouchedNonFinite model.TopUp
	require.NoError(t, model.DB.First(&untouchedNonFinite, nonFiniteMoney.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, untouchedNonFinite.Status, "非有限金额必须分文不动")
	var untouchedUnpaid model.TopUp
	require.NoError(t, model.DB.First(&untouchedUnpaid, unpaid.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, untouchedUnpaid.Status)
	var untouchedStripe model.TopUp
	require.NoError(t, model.DB.First(&untouchedStripe, stripeOrder.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, untouchedStripe.Status)

	// 已尝试的单在短租约内不会被立即重复查询。
	again, err := ReconcileEpayOrders(0, 3600, 100, false)
	require.NoError(t, err)
	assert.Equal(t, 0, again.Scanned)
	assert.Equal(t, 0, again.Completed)

	// 工作进程中断或租约过期后，pending 单必须恢复可扫描。
	expiredLease := time.Now().Add(-time.Second).UnixNano()
	require.NoError(t, model.DB.Model(&model.TopUp{}).
		Where("id IN ?", []int{wrongMoney.Id, nonFiniteMoney.Id, unpaid.Id}).
		Update("reconcile_time", expiredLease).Error)
	again, err = ReconcileEpayOrders(0, 3600, 100, false)
	require.NoError(t, err)
	assert.Equal(t, 3, again.Scanned)
}

// M1 纵深防御：回调金额与订单一致才放行；明确不符必须拒；缺失/不可解析不阻断合法回调。
func TestEpayCallbackMoneyMatches(t *testing.T) {
	assert.True(t, EpayCallbackMoneyMatches("1.00", 1.00), "金额一致必须放行")
	assert.True(t, EpayCallbackMoneyMatches("1.005", 1.00), "容差内视为一致")
	assert.False(t, EpayCallbackMoneyMatches("100.00", 1.00), "金额明显不符必须拒绝")
	assert.False(t, EpayCallbackMoneyMatches("0.50", 1.00), "少付必须拒绝")
	assert.True(t, EpayCallbackMoneyMatches("", 1.00), "回调无金额时不阻断（兼容不规范平台）")
	assert.True(t, EpayCallbackMoneyMatches("abc", 1.00), "金额不可解析时不阻断")
	assert.False(t, EpayCallbackMoneyMatches("NaN", 1.00), "非有限金额不得绕过一致性校验")
}

func TestReconcileEpayOrdersRejectsMissingPlatformIdentity(t *testing.T) {
	user := &model.User{
		Username: fmt.Sprintf("epay-identity-%s", common.GetUUID()[:8]),
		Password: "x", Role: common.RoleCommonUser, Group: "default",
		Status: common.UserStatusEnabled, AffCode: common.GetUUID()[:8],
	}
	require.NoError(t, model.DB.Create(user).Error)
	t.Cleanup(func() { model.DB.Unscoped().Delete(&model.User{}, user.Id) })
	order := seedReconcileTopUp(t, "REC-MISSING-IDENTITY", user.Id, 1, model.PaymentProviderEpay)

	server := fakeEpayPlatform(t, map[string]string{
		"REC-MISSING-IDENTITY": `{"code":1,"trade_no":"P5","type":"alipay","money":"1.00","status":1}`,
	})
	defer server.Close()
	setupEpayReconcileTest(t, server.URL)

	summary, err := ReconcileEpayOrders(0, 3600, 1, false)
	require.NoError(t, err)
	require.Len(t, summary.Items, 1)
	assert.Equal(t, EpayReconcileActionSkipped, summary.Items[0].Action)

	var stored model.TopUp
	require.NoError(t, model.DB.First(&stored, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestReconcileEpayOrdersValidatesWindow(t *testing.T) {
	server := fakeEpayPlatform(t, nil)
	defer server.Close()
	setupEpayReconcileTest(t, server.URL)

	_, err := ReconcileEpayOrders(600, 60, 100, true)
	assert.Error(t, err, "min>=max 的窗口必须报错")
}
