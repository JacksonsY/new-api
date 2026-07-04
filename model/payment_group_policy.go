package model

// 蓝图C 充值→自动升级分组（VIP 等级）。判定逻辑移植 guoruqiang/new-api 的
// payment_group_policy.go（其仓库有 765 行表测背书），要点：
//   - 结算事务内原子切组：与订单置 success/加额度同一事务，不存在"钱到了组没升"的窗口；
//   - 累计充值 = 事务内 SUM 全部成功普通充值单，逐单按支付网关归一成 USD；
//   - 受控链保护：只动 {基准组 ∪ 规则目标组} 链内的用户，管理员手动设过特殊组的不碰；
//   - 订阅互斥：订阅升级组生效期间充值不覆盖分组（订阅到期回退时再解析充值档，
//     见 subscription.go 的到期处理）。
// 配置在 operation_setting.PaymentSetting 的 AutoSwitchGroup* 字段。

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"gorm.io/gorm"
)

// NormalizeTopUpValueUSD 把一笔成功的普通充值单归一成 USD 计入累计额。
// 各网关字段语义（与各 Recharge* 的入账口径逐一对应）：
//   - stripe：Money 即美元金额（入账按 Money×QuotaPerUnit）；
//   - creem：Amount 是额度整数（入账直接加 Amount），换算回美元除 QuotaPerUnit；
//   - 其余（epay/waffo/waffo_pancake/手动补单）：Amount 即美元数量（入账按 Amount×QuotaPerUnit）。
//
// 订阅关联的 TopUp 行 Amount=0，被调用方的 amount>0 过滤天然排除（订阅不计充值档）。
func NormalizeTopUpValueUSD(topUp *TopUp) float64 {
	if topUp == nil {
		return 0
	}
	switch topUp.PaymentProvider {
	case PaymentProviderStripe:
		return topUp.Money
	case PaymentProviderCreem:
		if common.QuotaPerUnit <= 0 {
			return 0
		}
		return float64(topUp.Amount) / common.QuotaPerUnit
	default:
		return float64(topUp.Amount)
	}
}

func normalizeAutoSwitchBaseGroup(baseGroup string) string {
	trimmed := strings.TrimSpace(baseGroup)
	if trimmed == "" {
		return "default"
	}
	return trimmed
}

// buildAutoSwitchGroupChainSet 受控链 = {基准组} ∪ {全部规则目标组}。
func buildAutoSwitchGroupChainSet(ps *operation_setting.PaymentSetting) map[string]struct{} {
	chainGroups := make(map[string]struct{}, len(ps.AutoSwitchGroupRules)+1)
	chainGroups[normalizeAutoSwitchBaseGroup(ps.AutoSwitchGroupBaseGroup)] = struct{}{}
	for _, rule := range ps.AutoSwitchGroupRules {
		group := strings.TrimSpace(rule.Group)
		if group == "" {
			continue
		}
		chainGroups[group] = struct{}{}
	}
	return chainGroups
}

func isAutoSwitchChainMember(group string, chainGroups map[string]struct{}) bool {
	trimmed := strings.TrimSpace(group)
	if trimmed == "" {
		return false
	}
	_, ok := chainGroups[trimmed]
	return ok
}

// autoSwitchTopUpCutoffTime OnlyNewTopups 生效时返回起算时间戳，否则 0（统计全部历史）。
func autoSwitchTopUpCutoffTime(ps *operation_setting.PaymentSetting) int64 {
	if !ps.AutoSwitchGroupOnlyNewTopups || ps.AutoSwitchGroupEnabledFrom <= 0 {
		return 0
	}
	return ps.AutoSwitchGroupEnabledFrom
}

// GetUserSuccessfulTopupTotalUSDTx 事务内统计用户累计成功充值（USD）。
// amount>0 排除订阅关联单；OnlyNewTopups 生效时只算 complete_time 在起算点之后的单。
func GetUserSuccessfulTopupTotalUSDTx(tx *gorm.DB, userId int) (float64, error) {
	if tx == nil {
		return 0, errors.New("tx is nil")
	}
	if userId <= 0 {
		return 0, errors.New("invalid user id")
	}
	query := tx.Model(&TopUp{}).
		Select("amount", "money", "payment_provider").
		Where("user_id = ? AND status = ? AND amount > 0", userId, common.TopUpStatusSuccess)
	if cutoff := autoSwitchTopUpCutoffTime(operation_setting.GetPaymentSetting()); cutoff > 0 {
		query = query.Where("complete_time >= ?", cutoff)
	}
	var topUps []TopUp
	if err := query.Find(&topUps).Error; err != nil {
		return 0, err
	}
	totalUSD := 0.0
	for i := range topUps {
		totalUSD += NormalizeTopUpValueUSD(&topUps[i])
	}
	return totalUSD, nil
}

// matchAutoSwitchGroupRule 取"已达标里阈值最高"的规则目标组，规则顺序无关；
// 无达标返回空串。
func matchAutoSwitchGroupRule(totalTopUpUSD float64, rules []operation_setting.PaymentAutoSwitchGroupRule) string {
	if totalTopUpUSD <= 0 || len(rules) == 0 {
		return ""
	}
	matchedGroup := ""
	matchedThreshold := -1.0
	for _, rule := range rules {
		group := strings.TrimSpace(rule.Group)
		if group == "" || rule.ThresholdUSD > totalTopUpUSD {
			continue
		}
		if rule.ThresholdUSD > matchedThreshold {
			matchedThreshold = rule.ThresholdUSD
			matchedGroup = group
		}
	}
	return matchedGroup
}

// getTopUpAutoSwitchTargetGroupTx 按当前累计充值解析应处档位组（开关关闭返回空）。
func getTopUpAutoSwitchTargetGroupTx(tx *gorm.DB, userId int) (string, error) {
	if tx == nil {
		return "", errors.New("tx is nil")
	}
	if userId <= 0 {
		return "", errors.New("invalid user id")
	}
	ps := operation_setting.GetPaymentSetting()
	if !ps.AutoSwitchGroupEnabled {
		return "", nil
	}
	totalTopUpUSD, err := GetUserSuccessfulTopupTotalUSDTx(tx, userId)
	if err != nil {
		return "", err
	}
	return matchAutoSwitchGroupRule(totalTopUpUSD, ps.AutoSwitchGroupRules), nil
}

// getActiveSubscriptionUpgradeGroupTx 用户当前是否有生效中的订阅升级组
// （active、未到期、upgrade_group 非空的最近一条）。
// now<=0 时用应用时钟兜底——绝不能在事务内调 GetDBTimestamp()：它走全局 DB
// 连接，SQLite 单写锁下会与外层事务互等死锁。
func getActiveSubscriptionUpgradeGroupTx(tx *gorm.DB, userId int, now int64) (string, error) {
	if tx == nil {
		return "", errors.New("tx is nil")
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	var activeSub UserSubscription
	query := tx.Where("user_id = ? AND status = ? AND end_time > ? AND upgrade_group <> ''",
		userId, "active", now).
		Order("end_time desc, id desc").
		Limit(1).
		Find(&activeSub)
	if query.Error != nil {
		return "", query.Error
	}
	if query.RowsAffected == 0 {
		return "", nil
	}
	return strings.TrimSpace(activeSub.UpgradeGroup), nil
}

func updateUserGroupTx(tx *gorm.DB, userId int, targetGroup string) error {
	targetGroup = strings.TrimSpace(targetGroup)
	if targetGroup == "" {
		return nil
	}
	return tx.Model(&User{}).Where("id = ?", userId).
		Update("group", targetGroup).Error
}

// applyTopUpAutoSwitchGroupTx 结算事务内的自动切组入口，各充值完成路径在
// 入账后调用。返回切换后的组（未切换返回空串），调用方在事务提交后据此刷新
// 用户分组缓存（UpdateUserGroupCache）。
// 失败会使整个结算事务回滚——切组与入账是同一笔业务，宁可回调重试也不允许
// "钱到了组没升"的中间态。
func applyTopUpAutoSwitchGroupTx(tx *gorm.DB, userId int) (string, error) {
	if tx == nil {
		return "", errors.New("tx is nil")
	}
	if userId <= 0 {
		return "", errors.New("invalid user id")
	}
	ps := operation_setting.GetPaymentSetting()
	if !ps.AutoSwitchGroupEnabled {
		return "", nil
	}

	currentGroup, err := getUserGroupByIdTx(tx, userId)
	if err != nil {
		return "", err
	}

	// 订阅升级组生效期间，普通充值不覆盖当前分组（订阅优先级更高；
	// 若当前组被手动改离了订阅组，这里顺带纠正回订阅组）。
	// 传 0 让 helper 用应用时钟——事务内禁用 GetDBTimestamp（见 helper 注释）。
	activeUpgradeGroup, err := getActiveSubscriptionUpgradeGroupTx(tx, userId, 0)
	if err != nil {
		return "", err
	}
	if activeUpgradeGroup != "" {
		if currentGroup == activeUpgradeGroup {
			return "", nil
		}
		if err := updateUserGroupTx(tx, userId, activeUpgradeGroup); err != nil {
			return "", err
		}
		return activeUpgradeGroup, nil
	}

	// 受控链保护：当前组在链外（管理员手动设的特殊组）不碰
	chainGroups := buildAutoSwitchGroupChainSet(ps)
	if !isAutoSwitchChainMember(currentGroup, chainGroups) {
		return "", nil
	}

	targetGroup, err := getTopUpAutoSwitchTargetGroupTx(tx, userId)
	if err != nil {
		return "", err
	}
	if targetGroup == "" || currentGroup == targetGroup {
		return "", nil
	}
	if err := updateUserGroupTx(tx, userId, targetGroup); err != nil {
		return "", err
	}
	return targetGroup, nil
}

// resolveTopUpTierOverrideTx 订阅到期回退时的充值档覆盖：回退目标组若在受控
// 链内，且用户按累计充值应处更高档，则回退到充值档而非原组。目标组在链外
// （显式 downgrade 到特殊组等）时不干预，原样返回。
func resolveTopUpTierOverrideTx(tx *gorm.DB, userId int, fallbackGroup string) (string, error) {
	ps := operation_setting.GetPaymentSetting()
	if !ps.AutoSwitchGroupEnabled {
		return fallbackGroup, nil
	}
	if !isAutoSwitchChainMember(fallbackGroup, buildAutoSwitchGroupChainSet(ps)) {
		return fallbackGroup, nil
	}
	tierGroup, err := getTopUpAutoSwitchTargetGroupTx(tx, userId)
	if err != nil {
		return "", err
	}
	if tierGroup != "" {
		return tierGroup, nil
	}
	return fallbackGroup, nil
}
