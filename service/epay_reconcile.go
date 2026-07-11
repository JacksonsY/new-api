package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

// 易支付主动对账（蓝图D）：回调是唯一到账通道，回调丢失=用户付了钱额度不到账。
// 定时用商户查单接口核对近窗口的 pending 单，平台已支付的走与回调完全相同的
// 幂等完成函数补结算；更早的漏单由管理端手动对账接口处理（dry_run 默认开）。
// 查到未支付的单不动（不自动关单）。

const (
	epayReconcileTickInterval = time.Minute
	// 自动任务只兜近窗口：太新的单用户可能正在支付页上，太老的交给手动对账
	epayReconcileAutoMinAgeSeconds = int64(60)
	epayReconcileAutoMaxAgeSeconds = int64(600)
	epayReconcileAutoBatchLimit    = 100
	// 单与单之间的查询节流，避免打爆平台接口
	epayReconcileQueryThrottle = 200 * time.Millisecond
	// 金额容差：平台可能返回不同精度的小数
	epayReconcileMoneyTolerance = 0.011
)

const (
	EpayReconcileActionCompleted     = "completed"
	EpayReconcileActionWouldComplete = "would_complete"
	EpayReconcileActionSkipped       = "skipped"

	epayOrderKindTopUp        = "topup"
	epayOrderKindSubscription = "subscription"
)

// EpayCallbackMoneyMatches 校验回调金额与本地订单金额是否一致（同对账路径容差）。
// 回调金额缺失或不可解析时返回 true（不阻断不规范但合法的回调）；仅在明确不符时返回 false。
// 回调是主到账通道，此校验让它与对账路径保持同等的金额纵深防御。
func EpayCallbackMoneyMatches(callbackMoney string, orderMoney float64) bool {
	if callbackMoney == "" {
		return true
	}
	platformMoney, err := strconv.ParseFloat(callbackMoney, 64)
	if err != nil {
		return true
	}
	if math.IsNaN(platformMoney) || math.IsInf(platformMoney, 0) ||
		math.IsNaN(orderMoney) || math.IsInf(orderMoney, 0) {
		return false
	}
	diff := platformMoney - orderMoney
	return diff <= epayReconcileMoneyTolerance && diff >= -epayReconcileMoneyTolerance
}

var (
	epayReconcileOnce    sync.Once
	epayReconcileRunning atomic.Bool
)

type EpayReconcileItem struct {
	OrderKind string  `json:"order_kind"`
	TradeNo   string  `json:"trade_no"`
	UserId    int     `json:"user_id"`
	Money     float64 `json:"money"`
	Action    string  `json:"action"`
	Reason    string  `json:"reason,omitempty"`
}

type EpayReconcileSummary struct {
	DryRun    bool                `json:"dry_run"`
	Scanned   int                 `json:"scanned"`
	Completed int                 `json:"completed"`
	Items     []EpayReconcileItem `json:"items"`
}

// StartEpayReconcileTask 启动自动对账任务（仅 master 节点；未配置易支付时每轮直接空转）。
func StartEpayReconcileTask() {
	epayReconcileOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("epay reconcile task started: tick=%s, window=[%ds,%ds]",
				epayReconcileTickInterval, epayReconcileAutoMinAgeSeconds, epayReconcileAutoMaxAgeSeconds))
			ticker := time.NewTicker(epayReconcileTickInterval)
			defer ticker.Stop()
			for range ticker.C {
				runEpayReconcileOnce()
			}
		})
	})
}

func runEpayReconcileOnce() {
	if !epayReconcileRunning.CompareAndSwap(false, true) {
		return
	}
	defer epayReconcileRunning.Store(false)

	if GetEpayClient() == nil {
		return
	}
	summary, err := ReconcileEpayOrders(epayReconcileAutoMinAgeSeconds, epayReconcileAutoMaxAgeSeconds, epayReconcileAutoBatchLimit, false)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("epay reconcile pass failed: %v", err))
		return
	}
	if summary.Completed > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("epay reconcile pass: scanned=%d, completed=%d", summary.Scanned, summary.Completed))
	}
}

// ReconcileEpayOrders 对窗口内的 pending 易支付单（充值+订阅）逐单查平台状态并补结算。
// dryRun=true 只报告不动账。自动任务与管理端手动对账共用此入口。
func ReconcileEpayOrders(minAgeSeconds int64, maxAgeSeconds int64, limit int, dryRun bool) (*EpayReconcileSummary, error) {
	settings := operation_setting.GetEpaySettingSnapshot()
	client, err := buildEpayClientFromSnapshot(settings)
	if err != nil {
		return nil, fmt.Errorf("易支付未配置")
	}
	if minAgeSeconds < 0 || maxAgeSeconds <= minAgeSeconds {
		return nil, fmt.Errorf("非法的对账时间窗口")
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	summary := &EpayReconcileSummary{DryRun: dryRun, Items: make([]EpayReconcileItem, 0)}

	topUpCount := 0
	if dryRun {
		topUps, err := model.GetRecentPendingTopUps(model.PaymentProviderEpay, minAgeSeconds, maxAgeSeconds, limit)
		if err != nil {
			return nil, err
		}
		for i, topUp := range topUps {
			if i > 0 {
				time.Sleep(epayReconcileQueryThrottle)
			}
			item := reconcileOneEpayOrder(client, settings.MerchantID, epayOrderKindTopUp, topUp.TradeNo, topUp.UserId, topUp.Money, true)
			summary.Items = append(summary.Items, item)
		}
		topUpCount = len(topUps)
	} else {
		claimedTopUpIDs := make([]int, 0, limit)
		for topUpCount < limit {
			if topUpCount > 0 {
				time.Sleep(epayReconcileQueryThrottle)
			}
			claimed, err := model.ClaimRecentPendingTopUps(
				model.PaymentProviderEpay, minAgeSeconds, maxAgeSeconds, 1, claimedTopUpIDs...,
			)
			if err != nil {
				return nil, err
			}
			if len(claimed) == 0 {
				break
			}
			topUp := claimed[0]
			claimedTopUpIDs = append(claimedTopUpIDs, topUp.Id)
			item := reconcileOneEpayOrder(client, settings.MerchantID, epayOrderKindTopUp, topUp.TradeNo, topUp.UserId, topUp.Money, false)
			summary.Items = append(summary.Items, item)
			topUpCount++
		}
	}

	remaining := limit - topUpCount
	if dryRun && remaining > 0 {
		subOrders, err := model.GetRecentPendingSubscriptionOrders(model.PaymentProviderEpay, minAgeSeconds, maxAgeSeconds, remaining)
		if err != nil {
			return nil, err
		}
		for i, order := range subOrders {
			if topUpCount > 0 || i > 0 {
				time.Sleep(epayReconcileQueryThrottle)
			}
			item := reconcileOneEpayOrder(client, settings.MerchantID, epayOrderKindSubscription, order.TradeNo, order.UserId, order.Money, true)
			summary.Items = append(summary.Items, item)
		}
	} else if remaining > 0 {
		claimedOrderIDs := make([]int, 0, remaining)
		for subscriptionCount := 0; subscriptionCount < remaining; subscriptionCount++ {
			if topUpCount > 0 || subscriptionCount > 0 {
				time.Sleep(epayReconcileQueryThrottle)
			}
			claimed, err := model.ClaimRecentPendingSubscriptionOrders(
				model.PaymentProviderEpay, minAgeSeconds, maxAgeSeconds, 1, claimedOrderIDs...,
			)
			if err != nil {
				return nil, err
			}
			if len(claimed) == 0 {
				break
			}
			order := claimed[0]
			claimedOrderIDs = append(claimedOrderIDs, order.Id)
			item := reconcileOneEpayOrder(client, settings.MerchantID, epayOrderKindSubscription, order.TradeNo, order.UserId, order.Money, false)
			summary.Items = append(summary.Items, item)
		}
	}

	summary.Scanned = len(summary.Items)
	for _, item := range summary.Items {
		if item.Action == EpayReconcileActionCompleted {
			summary.Completed++
		}
	}
	return summary, nil
}

// reconcileOneEpayOrder 查单 + 五重校验 + 补结算。
// 五重校验（全过才补账，防伪造/串单骗补）：平台查到订单、商户单号一致、
// 商户号一致、金额容差内一致、状态=已支付。
func reconcileOneEpayOrder(client epay.Client, merchantID string, kind string, tradeNo string, userId int, expectMoney float64, dryRun bool) EpayReconcileItem {
	item := EpayReconcileItem{OrderKind: kind, TradeNo: tradeNo, UserId: userId, Money: expectMoney, Action: EpayReconcileActionSkipped}
	ctx := context.Background()

	info, err := client.QueryOrderByOutTradeNo(tradeNo)
	if err != nil {
		// 非 JSON 错误页 = 平台大概率只认 trade_no 查单,按商户单号的自动对账
		// 在该平台形同虚设。失败方向安全(不会错误入账),但必须显式告警,
		// 否则"回调丢失→用户付钱不到账"的兜底静默失效,只能等用户投诉。
		var nonJSON *epay.NonJSONResponseError
		if errors.As(err, &nonJSON) {
			item.Reason = "查单失败: 平台不支持按商户单号(out_trade_no)查单,自动对账对该平台无效,请人工核对: " + err.Error()
			common.SysError("epay reconcile: platform rejected out_trade_no query (v2 query likely requires trade_no), automatic reconcile is ineffective for this platform, trade_no=" + tradeNo)
			return item
		}
		item.Reason = "查单失败: " + err.Error()
		return item
	}
	if !info.Found {
		item.Reason = "平台无此订单"
		return item
	}
	if !info.Paid {
		item.Reason = "平台侧未支付"
		return item
	}
	if info.OutTradeNo != tradeNo {
		item.Reason = "商户单号不一致"
		logger.LogError(ctx, fmt.Sprintf("epay reconcile out_trade_no mismatch: local=%s, platform=%s", tradeNo, info.OutTradeNo))
		return item
	}
	if info.PID != merchantID {
		item.Reason = "商户号不一致"
		logger.LogError(ctx, fmt.Sprintf("epay reconcile pid mismatch: trade_no=%s, platform_pid=%s", tradeNo, info.PID))
		return item
	}
	platformMoney, err := strconv.ParseFloat(info.Money, 64)
	if err != nil {
		item.Reason = "平台金额不可解析: " + info.Money
		return item
	}
	if math.IsNaN(platformMoney) || math.IsInf(platformMoney, 0) ||
		math.IsNaN(expectMoney) || math.IsInf(expectMoney, 0) {
		item.Reason = "平台或本地金额不是有限数值"
		return item
	}
	if math.Abs(platformMoney-expectMoney) > epayReconcileMoneyTolerance {
		item.Reason = fmt.Sprintf("金额不一致: 本地 %.2f, 平台 %.2f", expectMoney, platformMoney)
		logger.LogError(ctx, fmt.Sprintf("epay reconcile money mismatch: trade_no=%s, local=%.2f, platform=%.2f", tradeNo, expectMoney, platformMoney))
		return item
	}

	if dryRun {
		item.Action = EpayReconcileActionWouldComplete
		return item
	}

	switch kind {
	case epayOrderKindTopUp:
		completedUserId, quotaToAdd, money, switchedGroup, err := model.CompleteEpayTopUp(tradeNo, info.Type)
		if err != nil {
			item.Reason = "补结算失败: " + err.Error()
			return item
		}
		if quotaToAdd > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("epay reconcile completed topup: trade_no=%s, user_id=%d, quota=%d, money=%.2f, switched_group=%q",
				tradeNo, completedUserId, quotaToAdd, money, switchedGroup))
			model.RecordTopupLog(completedUserId, fmt.Sprintf("对账补单成功（回调丢失），充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), money), "reconcile", info.Type, "epay")
		}
	case epayOrderKindSubscription:
		rawPayload := ""
		if payloadBytes, marshalErr := common.Marshal(info.Raw); marshalErr == nil {
			rawPayload = string(payloadBytes)
		}
		if err := model.CompleteSubscriptionOrder(tradeNo, rawPayload, model.PaymentProviderEpay, info.Type); err != nil {
			item.Reason = "补结算失败: " + err.Error()
			return item
		}
		logger.LogInfo(ctx, fmt.Sprintf("epay reconcile completed subscription order: trade_no=%s, user_id=%d", tradeNo, userId))
	default:
		item.Reason = "未知订单类型"
		return item
	}
	item.Action = EpayReconcileActionCompleted
	return item
}
