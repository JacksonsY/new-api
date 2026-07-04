package service

// 蓝图A 渠道余额告警（移植自 feitianbubu/new-api，见 new-api-fork二开功能图鉴.md §十一）。
// 不查上游：实时余额 = balance - (used_quota - balance_snapshot)/QuotaPerUnit 本地推算；
// 消耗取双口径——①最近 7 个活跃日的日均、②近 24h 滑窗，urgency 取两者剩余天数的
// 较小值，低于阈值的渠道按紧急度排序取 Top3 生成多行卡片，走 NotifyRootUser
// （email/webhook 全通道，零适配）。
// 与 fork 的两点差异：调度挂本仓库 system task 框架（多主 DB 租约去重 + 运行历史，
// 替代它的 env+robfig/cron）；消耗按渠道成本倍率(channel_ratio)折算成上游成本口径，
// 与成本统计二开对齐——注意 used_quota 增量本身已是折算后的值，日志聚合是原始值，
// 因此仅对日志聚合结果补乘当前倍率。

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const (
	channelBalanceAlertNotifyType = "channel_balance_days_alert"
	channelBalanceAlertTopN       = 3
)

// formatBalanceAmount 按站点额度展示货币渲染 USD 金额，向上取整到分，
// 避免小额正余额显示成 0。
func formatBalanceAmount(usd float64) string {
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return fmt.Sprintf("%.0f", usd*common.QuotaPerUnit)
	}
	amount := math.Ceil(usd*operation_setting.GetUsdToCurrencyRate(operation_setting.USDExchangeRate)*100) / 100
	return fmt.Sprintf("%s%.2f", operation_setting.GetCurrencySymbol(), amount)
}

type channelDaysRemaining struct {
	id       int
	name     string
	balance  float64
	avgDaily float64
	avgDays  int
	last24h  float64
}

func (c channelDaysRemaining) daysByAvg() float64 { return daysRemaining(c.balance, c.avgDaily) }
func (c channelDaysRemaining) daysBy24h() float64 { return daysRemaining(c.balance, c.last24h) }
func (c channelDaysRemaining) urgency() float64   { return min(c.daysByAvg(), c.daysBy24h()) }

// daysRemaining 无消耗返回 +Inf——"没人用"不该触发告警。
func daysRemaining(balance, dailyUsd float64) float64 {
	if dailyUsd <= 0 {
		return math.Inf(1)
	}
	return balance / dailyUsd
}

func formatDaysRemaining(days float64) string {
	switch {
	case days < 1:
		return "不足 1 天"
	case days > 999:
		return "超过 999 天"
	default:
		return fmt.Sprintf("约剩 %.1f 天", days)
	}
}

// formatChannelBalanceAlert 渲染多行卡片通知；正文以 \n 换行，交由发送层处理。
func formatChannelBalanceAlert(below []channelDaysRemaining, threshold int, now time.Time) (string, string) {
	sort.Slice(below, func(i, j int) bool { return below[i].urgency() < below[j].urgency() })
	shown := below[:min(len(below), channelBalanceAlertTopN)]

	lines := []string{fmt.Sprintf("⚠️ 渠道余额预警 · %d 个通道", len(below))}
	for _, c := range shown {
		last24hLine := "近24h 无消费"
		if c.last24h > 0 {
			last24hLine = fmt.Sprintf("近24h %s · %s", formatBalanceAmount(c.last24h), formatDaysRemaining(c.daysBy24h()))
		}
		lines = append(lines,
			"",
			fmt.Sprintf("%s（#%d） 余额 %s", c.name, c.id, formatBalanceAmount(c.balance)),
			fmt.Sprintf("%d日均 %s · %s", c.avgDays, formatBalanceAmount(c.avgDaily), formatDaysRemaining(c.daysByAvg())),
			last24hLine,
		)
	}
	if len(below) > len(shown) {
		lines = append(lines, "", fmt.Sprintf("……共 %d 个通道低于阈值，仅展示最紧急的 %d 个", len(below), len(shown)))
	}
	lines = append(lines, "", fmt.Sprintf("阈值 %d 天 · %s", threshold, now.Format("2006-01-02 15:04")))

	subject := fmt.Sprintf("渠道余额预警：%d 个通道预计剩余不足 %d 天", len(below), threshold)
	return subject, strings.Join(lines, "\n")
}

// CheckChannelBalanceDaysOnce 执行一轮余额检查，返回低于阈值的渠道数。
// 由 channel_balance_alert system task 调度（也可手动触发一次性任务）。
func CheckChannelBalanceDaysOnce(threshold int) (int, error) {
	var channels []*model.Channel
	err := model.DB.Select("id", "name", "balance", "balance_snapshot", "used_quota", "channel_ratio").
		Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error
	if err != nil {
		return 0, fmt.Errorf("query channels: %w", err)
	}
	channelIds := make([]int, 0, len(channels))
	for _, channel := range channels {
		channelIds = append(channelIds, channel.Id)
	}
	now := common.GetTimestamp()
	usageMap, err := model.GetChannelsRecentUsage(channelIds, now-model.ChannelRecentUsageLookbackDays*86400, model.ChannelRecentUsageActiveDays)
	if err != nil {
		return 0, fmt.Errorf("query recent usage: %w", err)
	}
	quota24hMap, err := model.GetChannelsQuotaSince(channelIds, now-86400)
	if err != nil {
		return 0, fmt.Errorf("query 24h usage: %w", err)
	}

	var below []channelDaysRemaining
	for _, channel := range channels {
		usage, ok := usageMap[channel.Id]
		if !ok || usage.Quota <= 0 {
			continue
		}
		liveBalance := channel.Balance
		if channel.BalanceSnapshot != nil {
			liveBalance -= float64(channel.UsedQuota-*channel.BalanceSnapshot) / common.QuotaPerUnit
		}
		if liveBalance <= 0 {
			continue
		}
		// 日志聚合是用户侧原始额度，补乘当前成本倍率换算成上游成本口径，
		// 与 liveBalance 里 used_quota 增量（入账时已折算）保持同一口径。
		ratio := channel.GetChannelRatio()
		avgDailyUsd := float64(usage.Quota) * ratio / float64(max(usage.ActiveDays, 1)) / common.QuotaPerUnit
		last24hUsd := float64(quota24hMap[channel.Id]) * ratio / common.QuotaPerUnit
		c := channelDaysRemaining{
			id: channel.Id, name: channel.Name, balance: liveBalance,
			avgDaily: avgDailyUsd, avgDays: usage.ActiveDays,
			last24h: last24hUsd,
		}
		if c.urgency() <= float64(threshold) {
			below = append(below, c)
		}
	}
	if len(below) == 0 {
		return 0, nil
	}

	subject, content := formatChannelBalanceAlert(below, threshold, time.Unix(now, 0))
	NotifyRootUser(channelBalanceAlertNotifyType, subject, content)
	return len(below), nil
}
