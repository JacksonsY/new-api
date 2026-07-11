package model

// jzlh-agent 分销反欺诈：IP 重合检测 + 告警处置（解绑/追回/误报）。
// 检测思路参考 moeacgx/new-api affiliate_fraud.go：同一人控制主号与小号时几乎必然
// 共享网络出口，取代理与其名下用户的 IP 交集作为最强被动信号；同 IP ≥1 即告警、
// 不设次数阈值（交集大小落库供人判断轻重）、不自动处罚——办公室/校园 NAT 会天然
// 重合，必须人工裁决。
// 数据源与 moeacgx 的关键差异：本仓库消费日志的 IP 受用户自选开关(RecordIpLog)控制，
// 作弊者不会打开，因此常规检测用 register_ip(注册必落) ∪ user_ip_records(登录/注册/
// API 调用无条件埋点)；深扫(deep)额外拼 logs 表——登录审计日志必带 IP，可覆盖快表
// 上线前的历史行为。

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	FraudAlertStatusDetected  = "detected"
	FraudAlertStatusResolved  = "resolved"
	FraudAlertStatusDismissed = "dismissed"
)

const (
	FraudActionUnbind   = "unbind"
	FraudActionClawback = "clawback"
	FraudActionDismiss  = "dismiss"
	FraudActionDelete   = "delete"
)

// 单个用户参与交集计算的 IP 集上限（防超长 IN 列表拖垮查询；截断留日志）。
const fraudIPSetLimit = 1000

var (
	ErrFraudAlertNotFound        = errors.New("fraud alert not found")
	ErrFraudAlertAlreadyResolved = errors.New("fraud alert already resolved")
	ErrInvalidFraudAction        = errors.New("invalid fraud action")
)

// CommissionFraudAlert 代理-下级 IP 重合告警。同一对 (agent, invitee) 最多一条
// detected：重扫更新共享 IP 集、重合消失自动删除；dismissed 后重合仍在会新建告警
// 复活（特性——防"误报放行后作弊继续"被永久忽略）。
type CommissionFraudAlert struct {
	Id             int    `json:"id" gorm:"primaryKey"`
	AgentId        int    `json:"agent_id" gorm:"index"`
	InviteeId      int    `json:"invitee_id" gorm:"index"`
	SharedIps      string `json:"shared_ips" gorm:"type:text"` // JSON 数组
	SharedIpCount  int    `json:"shared_ip_count"`
	Status         string `json:"status" gorm:"type:varchar(16);index;default:detected"`
	ResolvedAction string `json:"resolved_action" gorm:"type:varchar(16)"`
	ClawbackQuota  int    `json:"clawback_quota"`
	AdminId        int    `json:"admin_id"`
	AdminRemark    string `json:"admin_remark" gorm:"type:varchar(255)"`
	DetectedAt     int64  `json:"detected_at"`
	ResolvedAt     int64  `json:"resolved_at"`
	CreatedAt      int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt      int64  `json:"updated_at" gorm:"autoUpdateTime"`
	// 列表展示用回填，不落库
	AgentUsername   string `json:"agent_username,omitempty" gorm:"-"`
	InviteeUsername string `json:"invitee_username,omitempty" gorm:"-"`
}

func (CommissionFraudAlert) TableName() string {
	return "commission_fraud_alerts"
}

// DetectCommissionFraud 扫描全部代理与其名下用户的 IP 重合，返回新增告警数。
// deep=true 时额外扫 logs 表（大表聚合，建议低峰手动触发）。
// 单个代理失败只记日志不中断整轮（一个坏行不该挡住其他代理的检测）。
func DetectCommissionFraud(days int, deep bool) (int, error) {
	var agentIds []int
	if err := DB.Model(&User{}).Where("agent_type <> ''").Pluck("id", &agentIds).Error; err != nil {
		return 0, err
	}
	newAlerts := 0
	for _, agentId := range agentIds {
		n, err := detectFraudForAgent(agentId, days, deep)
		if err != nil {
			common.SysLog(fmt.Sprintf("fraud detection failed for agent %d: %s", agentId, err.Error()))
			continue
		}
		newAlerts += n
	}
	return newAlerts, nil
}

func fraudDetectionSinceTimestamp(days int) int64 {
	if days <= 0 {
		return 0
	}
	return common.GetTimestamp() - int64(days)*86400
}

func detectFraudForAgent(agentId int, days int, deep bool) (int, error) {
	type inviteeRow struct {
		Id         int
		RegisterIp string
	}
	var invitees []inviteeRow
	if err := DB.Model(&User{}).Select("id", "register_ip").
		Where("inviter_id = ?", agentId).Find(&invitees).Error; err != nil {
		return 0, err
	}

	sinceTimestamp := fraudDetectionSinceTimestamp(days)
	inviterIPs, err := collectFraudUserIPs(agentId, sinceTimestamp, deep)
	if err != nil {
		return 0, err
	}
	inviterIPSet := make(map[string]bool, len(inviterIPs))
	for _, ip := range inviterIPs {
		inviterIPSet[ip] = true
	}

	// 快表来源：一次查询取全部下级的重合
	overlaps := make(map[int][]string)
	if len(invitees) > 0 && len(inviterIPs) > 0 {
		inviteeIds := make([]int, 0, len(invitees))
		for _, iv := range invitees {
			inviteeIds = append(inviteeIds, iv.Id)
		}
		batch, err := getIPOverlapBatch(inviteeIds, inviterIPs, sinceTimestamp)
		if err != nil {
			return 0, err
		}
		overlaps = batch
	}

	newAlerts := 0
	currentOverlaps := make(map[int][]string, len(invitees))
	for _, invitee := range invitees {
		shared := overlaps[invitee.Id]
		// register_ip 来源：注册 IP 命中代理 IP 集也算重合
		if ip, ok := normalizeFraudIP(invitee.RegisterIp); ok && inviterIPSet[ip] {
			shared = append(shared, ip)
		}
		// 深扫来源：logs 表（登录审计必带 IP；消费日志仅用户自开 RecordIpLog 时有）
		if deep && len(inviterIPs) > 0 {
			var logShared []string
			logQuery := LOG_DB.Model(&Log{}).
				Where("user_id = ? AND ip IN ? AND type <> ?", invitee.Id, inviterIPs, LogTypeTopup).
				Distinct("ip")
			if sinceTimestamp > 0 {
				logQuery = logQuery.Where("created_at >= ?", sinceTimestamp)
			}
			if err := logQuery.Pluck("ip", &logShared).Error; err == nil {
				shared = append(shared, logShared...)
			}
		}
		shared = filterFraudIPs(shared)
		currentOverlaps[invitee.Id] = shared
		created, err := upsertFraudAlertForPair(agentId, invitee.Id, shared)
		if err != nil {
			common.SysLog(fmt.Sprintf("fraud alert upsert failed for pair (%d,%d): %s", agentId, invitee.Id, err.Error()))
			continue
		}
		if created {
			newAlerts++
		}
	}
	// 重合已消失（或下级已解绑）的 detected 告警自动清除
	if err := refreshDetectedFraudAlertsForAgent(agentId, currentOverlaps); err != nil {
		common.SysLog(fmt.Sprintf("fraud alert refresh failed for agent %d: %s", agentId, err.Error()))
	}
	return newAlerts, nil
}

// collectFraudUserIPs 汇集一个用户在窗口内的 IP 集：register_ip ∪ 快表 (∪ 深扫 logs)。
func collectFraudUserIPs(userId int, sinceTimestamp int64, deep bool) ([]string, error) {
	ips, err := getUserIPRecordIPs(userId, sinceTimestamp, fraudIPSetLimit)
	if err != nil {
		return nil, err
	}
	var registerIp string
	if err := DB.Model(&User{}).Where("id = ?", userId).
		Pluck("register_ip", &registerIp).Error; err == nil && registerIp != "" {
		ips = append(ips, registerIp)
	}
	if deep {
		var logIPs []string
		logQuery := LOG_DB.Model(&Log{}).
			Where("user_id = ? AND ip <> '' AND type <> ?", userId, LogTypeTopup).
			Distinct("ip").Limit(fraudIPSetLimit + 1)
		if sinceTimestamp > 0 {
			logQuery = logQuery.Where("created_at >= ?", sinceTimestamp)
		}
		if err := logQuery.Pluck("ip", &logIPs).Error; err == nil {
			if len(logIPs) > fraudIPSetLimit {
				logIPs = logIPs[:fraudIPSetLimit]
				common.SysLog("fraud detection: log ip set truncated, userId=" + strconv.Itoa(userId))
			}
			ips = append(ips, logIPs...)
		}
	}
	return filterFraudIPs(ips), nil
}

// upsertFraudAlertForPair 维护"每对最多一条 detected"：
// 有重合 → 已有 detected 就地更新 IP 集，没有则新建；无重合 → 删除 detected。
func upsertFraudAlertForPair(agentId, inviteeId int, sharedIPs []string) (bool, error) {
	if len(sharedIPs) == 0 {
		return false, deleteDetectedFraudAlertForPair(agentId, inviteeId)
	}
	ipsJSON, err := common.Marshal(sharedIPs)
	if err != nil {
		return false, err
	}
	var alert CommissionFraudAlert
	findErr := DB.Where("agent_id = ? AND invitee_id = ? AND status = ?",
		agentId, inviteeId, FraudAlertStatusDetected).First(&alert).Error
	if findErr == nil {
		return false, DB.Model(&alert).Updates(map[string]interface{}{
			"shared_ips":      string(ipsJSON),
			"shared_ip_count": len(sharedIPs),
		}).Error
	}
	if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return false, findErr
	}
	alert = CommissionFraudAlert{
		AgentId:       agentId,
		InviteeId:     inviteeId,
		SharedIps:     string(ipsJSON),
		SharedIpCount: len(sharedIPs),
		Status:        FraudAlertStatusDetected,
		DetectedAt:    common.GetTimestamp(),
	}
	if err := DB.Create(&alert).Error; err != nil {
		return false, err
	}
	return true, nil
}

func deleteDetectedFraudAlertForPair(agentId, inviteeId int) error {
	return DB.Where("agent_id = ? AND invitee_id = ? AND status = ?",
		agentId, inviteeId, FraudAlertStatusDetected).
		Delete(&CommissionFraudAlert{}).Error
}

// refreshDetectedFraudAlertsForAgent 对照本轮交集结果清理陈旧 detected：
// 下级已解绑（不在 overlaps 里）或重合已消失的告警删除。
func refreshDetectedFraudAlertsForAgent(agentId int, overlaps map[int][]string) error {
	var alerts []CommissionFraudAlert
	if err := DB.Where("agent_id = ? AND status = ?", agentId, FraudAlertStatusDetected).
		Find(&alerts).Error; err != nil {
		return err
	}
	for _, alert := range alerts {
		if len(overlaps[alert.InviteeId]) == 0 {
			if err := deleteDetectedFraudAlertForPair(alert.AgentId, alert.InviteeId); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReviewCommissionFraudAlert 处置告警。action：
//   - unbind   解绑（断未来）：下级 inviter_id 清零，不再产生新分润
//   - clawback 解绑并追回（断过去+未来）：额外没收该对下级产生的全部分润
//   - dismiss  误报放行：状态置 dismissed（重合仍在时下轮扫描会新建告警复活）
//   - delete   删除记录（detected 删了下轮扫描会回来；主要用于清理历史单）
//
// unbind/clawback 用"detected→resolved 条件更新"做并发闸门（与提现审批同一模式），
// 只有赢得状态迁移的调用执行资金处置，天然防重复追回。
func ReviewCommissionFraudAlert(id int, action string, adminId int, remark string) error {
	switch action {
	case FraudActionDismiss:
		res := DB.Model(&CommissionFraudAlert{}).
			Where("id = ? AND status = ?", id, FraudAlertStatusDetected).
			Updates(map[string]interface{}{
				"status":          FraudAlertStatusDismissed,
				"resolved_action": FraudActionDismiss,
				"admin_id":        adminId,
				"admin_remark":    remark,
				"resolved_at":     common.GetTimestamp(),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fraudAlertReviewFailureReason(id)
		}
		return nil
	case FraudActionDelete:
		res := DB.Delete(&CommissionFraudAlert{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrFraudAlertNotFound
		}
		return nil
	case FraudActionUnbind, FraudActionClawback:
		return resolveFraudAlert(id, action, adminId, remark)
	}
	return ErrInvalidFraudAction
}

func fraudAlertReviewFailureReason(id int) error {
	var alert CommissionFraudAlert
	if err := DB.First(&alert, id).Error; err != nil {
		return ErrFraudAlertNotFound
	}
	return ErrFraudAlertAlreadyResolved
}

func resolveFraudAlert(id int, action string, adminId int, remark string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var alert CommissionFraudAlert
		if err := tx.First(&alert, id).Error; err != nil {
			return ErrFraudAlertNotFound
		}
		// Every commission asset exit takes this same user-row lock; clawback's
		// fund aggregation must too, or a concurrent MatureAgentCommissions can
		// flip pending rows to matured between the SUM snapshot and the UPDATE,
		// letting that money escape confiscation while history is still debited.
		var agent User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", alert.AgentId).First(&agent).Error; err != nil {
			return err
		}
		// 并发闸门：只有把 detected 翻成 resolved 的那次调用执行处置
		res := tx.Model(&CommissionFraudAlert{}).
			Where("id = ? AND status = ?", id, FraudAlertStatusDetected).
			Updates(map[string]interface{}{
				"status":          FraudAlertStatusResolved,
				"resolved_action": action,
				"admin_id":        adminId,
				"admin_remark":    remark,
				"resolved_at":     common.GetTimestamp(),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrFraudAlertAlreadyResolved
		}

		clawbackTotal := 0
		if action == FraudActionClawback {
			total, err := confiscatePairCommissionsTx(tx, alert.AgentId, alert.InviteeId, alert.Id)
			if err != nil {
				return err
			}
			clawbackTotal = total
			if err := tx.Model(&CommissionFraudAlert{}).Where("id = ?", id).
				Update("clawback_quota", clawbackTotal).Error; err != nil {
				return err
			}
		}

		// 解绑：仅当下级仍绑定在该代理名下时清零（可能已被手动改绑）
		unbind := tx.Model(&User{}).
			Where("id = ? AND inviter_id = ?", alert.InviteeId, alert.AgentId).
			Update("inviter_id", 0)
		if unbind.Error != nil {
			return unbind.Error
		}
		if unbind.RowsAffected > 0 {
			if err := tx.Model(&User{}).Where("id = ? AND aff_count > 0", alert.AgentId).
				Update("aff_count", gorm.Expr("aff_count - 1")).Error; err != nil {
				return err
			}
		}

		return createCommissionRiskEventTx(tx, alert.AgentId, adminId, action, map[string]interface{}{
			"alert_id":       alert.Id,
			"invitee_id":     alert.InviteeId,
			"clawback_quota": clawbackTotal,
			"remark":         remark,
		})
	})
}

// confiscatePairCommissionsTx 没收某代理从某下级获得的全部分润：
//   - pending 流水翻为 confiscated（永不结转），从累计收益里扣除；
//   - matured 净额（入账正数与既有回冲负数相抵）走负记录冲销——与退款回冲同一契约：
//     负数已成熟流水 + 可提现余额允许冲负（欠账抵扣后续分润），保住完整审计链，
//     不学 moeacgx 物理删流水。
//
// 返回没收总额。
func confiscatePairCommissionsTx(tx *gorm.DB, agentId, inviteeId, alertId int) (int, error) {
	var pendingSum int64
	if err := tx.Model(&Commission{}).
		Where("agent_id = ? AND from_user_id = ? AND status = ?", agentId, inviteeId, CommissionStatusPending).
		Select("COALESCE(SUM(quota),0)").Row().Scan(&pendingSum); err != nil {
		return 0, err
	}
	if err := tx.Model(&Commission{}).
		Where("agent_id = ? AND from_user_id = ? AND status = ?", agentId, inviteeId, CommissionStatusPending).
		Update("status", CommissionStatusConfiscated).Error; err != nil {
		return 0, err
	}

	var maturedNet int64
	if err := tx.Model(&Commission{}).
		Where("agent_id = ? AND from_user_id = ? AND status = ?", agentId, inviteeId, CommissionStatusMatured).
		Select("COALESCE(SUM(quota),0)").Row().Scan(&maturedNet); err != nil {
		return 0, err
	}
	if maturedNet < 0 {
		// 已被回冲超过入账（欠账状态）：没什么可追，也不能反向退钱给作弊者
		maturedNet = 0
	}
	if maturedNet > 0 {
		sourceKey := fmt.Sprintf("clawback:alert:%d", alertId)
		if err := tx.Create(&Commission{
			AgentId:    agentId,
			FromUserId: inviteeId,
			Quota:      -int(maturedNet),
			Status:     CommissionStatusMatured,
			SourceKey:  &sourceKey,
			Rate:       0,
		}).Error; err != nil {
			return 0, err
		}
	}

	total := int(pendingSum + maturedNet)
	if total > 0 {
		updates := map[string]interface{}{
			// pending 与 matured 入账时都累加过 history，一并扣回
			"commission_history_quota": gorm.Expr("commission_history_quota - ?", total),
		}
		if maturedNet > 0 {
			updates["commission_quota"] = gorm.Expr("commission_quota - ?", maturedNet)
		}
		if err := tx.Model(&User{}).Where("id = ?", agentId).Updates(updates).Error; err != nil {
			return 0, err
		}
	}
	return total, nil
}

// SearchCommissionFraudAlerts 分页查询告警（status/keyword 过滤），回填双方用户名。
func SearchCommissionFraudAlerts(status string, keyword string, startIdx int, num int) ([]*CommissionFraudAlert, int64, error) {
	query := DB.Model(&CommissionFraudAlert{})
	if status = strings.TrimSpace(status); status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		userIds, err := findFraudMatchedUserIds(keyword)
		if err != nil {
			return nil, 0, err
		}
		if len(userIds) == 0 {
			return []*CommissionFraudAlert{}, 0, nil
		}
		query = query.Where("agent_id IN ? OR invitee_id IN ?", userIds, userIds)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var alerts []*CommissionFraudAlert
	if err := query.Order("detected_at desc, id desc").
		Offset(startIdx).Limit(num).Find(&alerts).Error; err != nil {
		return nil, 0, err
	}
	fillFraudAlertUsernames(alerts)
	return alerts, total, nil
}

func findFraudMatchedUserIds(keyword string) ([]int, error) {
	var userIds []int
	likeCondition := "username LIKE ? OR display_name LIKE ?"
	likeArgs := []interface{}{"%" + keyword + "%", "%" + keyword + "%"}
	if keywordInt, err := strconv.Atoi(keyword); err == nil {
		likeCondition = "id = ? OR " + likeCondition
		likeArgs = append([]interface{}{keywordInt}, likeArgs...)
	}
	err := DB.Model(&User{}).Unscoped().Where("("+likeCondition+")", likeArgs...).
		Limit(200).Pluck("id", &userIds).Error
	return userIds, err
}

func fillFraudAlertUsernames(alerts []*CommissionFraudAlert) {
	if len(alerts) == 0 {
		return
	}
	idSet := make(map[int]bool, len(alerts)*2)
	ids := make([]int, 0, len(alerts)*2)
	for _, a := range alerts {
		for _, id := range []int{a.AgentId, a.InviteeId} {
			if id > 0 && !idSet[id] {
				idSet[id] = true
				ids = append(ids, id)
			}
		}
	}
	type row struct {
		Id       int
		Username string
	}
	var rows []row
	if err := DB.Model(&User{}).Unscoped().Select("id", "username").
		Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return
	}
	nameById := make(map[int]string, len(rows))
	for _, r := range rows {
		nameById[r.Id] = r.Username
	}
	for _, a := range alerts {
		a.AgentUsername = nameById[a.AgentId]
		a.InviteeUsername = nameById[a.InviteeId]
	}
}
