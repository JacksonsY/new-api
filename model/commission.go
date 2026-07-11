package model

// jzlh-agent 代理分销：消费分润流水与代理管理（与上游解耦的独立文件，便于合并 upstream）。

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// 分润流水状态。历史行(加列前)默认为已成熟,不影响既有余额。
const (
	CommissionStatusPending     = 1 // 成熟期内,未计入可提现余额
	CommissionStatusMatured     = 2 // 已计入可提现余额
	CommissionStatusConfiscated = 3 // 反欺诈追回没收(原 pending 流水,永不结转)
)

// Commission 代理消费分润流水。
type Commission struct {
	Id         int   `json:"id" gorm:"primaryKey"`
	AgentId    int   `json:"agent_id" gorm:"index"`                  // 收益归属的代理(上级)用户 id
	FromUserId int   `json:"from_user_id" gorm:"index"`              // 产生消费的下级用户 id
	LogId      int   `json:"log_id" gorm:"default:0"`                // 关联消费日志 id（可为 0）
	Quota      int   `json:"quota"`                                  // 本次分润额度（quota 整数,负数=回冲）
	Status     int   `json:"status" gorm:"default:2;index"`          // 见 CommissionStatus*
	CreatedAt  int64 `json:"created_at" gorm:"autoCreateTime;index"` // 秒级时间戳
	// 来源幂等键(如 consume:<request_id> / task:<task_id>):同一下级的同一来源只结算一次。
	// 指针:NULL 不参与唯一约束(SQLite/MySQL/PG 行为一致),兼容无来源的旧行。
	// 注意:唯一索引不能用 gorm uniqueIndex 标签——SQLite 对已有表 ALTER ADD UNIQUE 列
	// 会直接报错,改在 EnsureCommissionSourceKeyIndex 里迁移后单独建。
	SourceKey *string `json:"source_key" gorm:"size:96"`
	// Rate 入账时的分润比例快照。退款回冲按快照比例计算，避免"消费与退款之间
	// 管理员改了费率"导致回冲金额与原分润不一致。旧行为 0(加列前)，回冲时降级用当前费率。
	Rate float64 `json:"rate" gorm:"default:0"`
	// FromUsername 产生消费的下级用户名，仅代理钱包分润明细展示用（列表查询时批量回填，不落库）。
	FromUsername string `json:"from_username,omitempty" gorm:"-"`
}

// EnsureCommissionSourceKeyIndex 迁移后单独创建 (from_user_id, source_key) 唯一索引。
// CREATE UNIQUE INDEX 语法三库一致；HasIndex 判存在避免重复创建报错。
func EnsureCommissionSourceKeyIndex() {
	if err := ensureCommissionSourceKeyIndex(DB); err != nil {
		// 唯一索引是分润幂等的最终防线，建不出来必须阻断启动，
		// 否则并发重放可无限重复入账。
		common.FatalLog("failed to create commission source_key unique index: " + err.Error())
	}
}

func ensureCommissionSourceKeyIndex(db *gorm.DB) error {
	const (
		idx       = "idx_commissions_from_source_key"
		legacyIdx = "idx_commissions_source_key"
	)
	if !db.Migrator().HasIndex(&Commission{}, idx) {
		err := db.Exec("CREATE UNIQUE INDEX " + idx + " ON commissions(from_user_id, source_key)").Error
		if err != nil && !db.Migrator().HasIndex(&Commission{}, idx) {
			return fmt.Errorf("create index %s: %w", idx, err)
		}
	}
	// Older builds made source_key globally unique, which lets one user's task
	// ID suppress another user's commission. Install the correctly scoped
	// index first so rolling upgrades never leave an unprotected write window.
	if db.Migrator().HasIndex(&Commission{}, legacyIdx) {
		if err := db.Migrator().DropIndex(&Commission{}, legacyIdx); err != nil &&
			db.Migrator().HasIndex(&Commission{}, legacyIdx) {
			return fmt.Errorf("drop legacy index %s: %w", legacyIdx, err)
		}
	}
	return nil
}

// isDuplicateKeyError 判断是否唯一约束冲突（三库错误文案不同，GORM 的
// ErrDuplicatedKey 翻译依赖 TranslateError 配置，这里同时做字符串兜底）。
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || // PostgreSQL / SQLite "UNIQUE constraint failed"
		strings.Contains(msg, "duplicate entry") || // MySQL
		strings.Contains(msg, "duplicate key") // PostgreSQL
}

// AgentTypeValid 校验代理类型。平台代理只有一种：normal（空串=非代理）。
func AgentTypeValid(t string) bool {
	return t == "normal"
}

// resolveCommissionAgent 解析下级用户的上级代理及本次应计分润额。
// 返回 (agent, commission)；不满足分润条件时 agent 为 nil。
func resolveCommissionAgent(fromUserId int, quota int) (*User, int) {
	// 合规声明未确认时不产生新分润(与上游"邀请返利需先确认合规"同一治理口径;
	// 分佣是更大额的现金激励,没有理由豁免)。回冲不受此限制:已入账的钱永远可被冲回。
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return nil, 0
	}
	if fromUserId <= 0 || quota <= 0 {
		return nil, 0
	}
	fromUser, err := GetUserById(fromUserId, false)
	if err != nil || fromUser == nil || fromUser.InviterId <= 0 {
		return nil, 0
	}
	// 自邀守卫：上级即本人时不计佣（防经数据修改后自己给自己刷分润）
	if fromUser.InviterId == fromUserId {
		return nil, 0
	}
	// 资格门槛：下级注册满 N 天其消费才开始计佣，抬高批量刷小号成本(0=关闭)。
	// fromUser 上面已加载，此检查零额外查询。
	if common.AgentInviteeMinAgeDays > 0 &&
		common.GetTimestamp()-fromUser.CreatedAt < int64(common.AgentInviteeMinAgeDays)*86400 {
		return nil, 0
	}
	agent, err := GetUserById(fromUser.InviterId, false)
	if err != nil || agent == nil {
		return nil, 0
	}
	// 被封禁/停用的代理不再产生新分润(冻结手段:封号即断佣)
	if agent.Status != common.UserStatusEnabled {
		return nil, 0
	}
	if agent.AgentType == "" || agent.UsageProfitRate <= 0 {
		return nil, 0
	}
	commission := common.QuotaFromFloat(float64(quota) * agent.UsageProfitRate)
	if commission <= 0 {
		return nil, 0
	}
	return agent, commission
}

// RecordAgentCommission 下级用户产生消费时，按上级代理的 usage_profit_rate 累计分润。
// 仅依赖 model 层，供 RecordConsumeLog / RecordTaskBillingLog 异步调用（避免 model→service 循环依赖）。
// sourceKey 非空时做来源幂等：同一来源(请求/任务)只结算一次，防重放/重复调用刷佣。
// AgentCommissionMatureMinutes > 0 时新分润先挂 pending，成熟后才进可提现余额
// （累计收益 history 立即累加，钱包里可见"待成熟"）。
func RecordAgentCommission(fromUserId int, consumedQuota int, sourceKey string) {
	if strings.HasPrefix(sourceKey, "task:") {
		_, event, _, ok := taskCommissionSourceParts(sourceKey)
		if !ok {
			return
		}
		if event != "initial" {
			if original := originalCommissionForReversal(fromUserId, sourceKey); original != nil {
				rate := commissionSnapshotRate(original)
				commission := common.QuotaFromFloat(float64(consumedQuota) * rate)
				if previousQuota, actualQuota, settlement := taskCommissionSettlementQuotas(event); settlement {
					if actualQuota <= previousQuota {
						return
					}
					commission = common.QuotaFromFloat(float64(actualQuota)*rate) -
						common.QuotaFromFloat(float64(previousQuota)*rate)
				}
				if commission > 0 {
					creditAgentCommission(original.AgentId, fromUserId, commission, rate, sourceKey)
				}
				return
			}
		}
		if strings.HasPrefix(event, "s") {
			// Settlement deltas inherit the task's initial owner. If no initial
			// positive commission exists, binding an inviter while the task is in
			// flight must not create a new ownership relationship.
			return
		}
	}
	agent, commission := resolveCommissionAgent(fromUserId, consumedQuota)
	if agent == nil {
		return
	}
	creditAgentCommission(agent.Id, fromUserId, commission, agent.UsageProfitRate, sourceKey)
}

// creditAgentCommission 分润入账的公共路径：
// 来源幂等 + 成熟期挂账 + 累计收益即时累加 + 费率快照。amount 必须为正。
func creditAgentCommission(agentId int, fromUserId int, amount int, rate float64, sourceKey string) {
	if amount <= 0 {
		return
	}
	var keyPtr *string
	if sourceKey != "" {
		if n := int64(0); DB.Model(&Commission{}).
			Where("from_user_id = ? AND source_key = ?", fromUserId, sourceKey).
			Count(&n).Error == nil && n > 0 {
			return // 已结算过该来源（预检；唯一索引兜底并发）
		}
		keyPtr = &sourceKey
	}
	status := CommissionStatusMatured
	if common.AgentCommissionMatureMinutes > 0 {
		status = CommissionStatusPending
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&Commission{
			AgentId:    agentId,
			FromUserId: fromUserId,
			Quota:      amount,
			Status:     status,
			SourceKey:  keyPtr,
			Rate:       rate,
		}).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{
			"commission_history_quota": gorm.Expr("commission_history_quota + ?", amount),
		}
		if status == CommissionStatusMatured {
			updates["commission_quota"] = gorm.Expr("commission_quota + ?", amount)
		}
		userUpdate := tx.Model(&User{}).
			Where("id = ? AND commission_history_quota <= ?", agentId, common.MaxQuota-amount)
		if status == CommissionStatusMatured {
			userUpdate = userUpdate.Where("commission_quota <= ?", common.MaxQuota-amount)
		}
		res := userUpdate.Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrQuotaOverflow
		}
		return nil
	})
	if err != nil {
		if keyPtr != nil && isDuplicateKeyError(err) {
			return // 唯一索引兜住的并发重复入账：幂等成功，静默返回
		}
		common.SysLog("failed to record agent commission: " + err.Error())
	}
}

// MatureAgentCommissions 惰性成熟结转：把超过成熟期的 pending 分润逐行翻转为
// matured 并计入可提现余额。逐行条件更新保证并发安全且三库兼容(无 FOR UPDATE 依赖)。
// 在代理读取汇总/转额度/申请提现前调用。
func MatureAgentCommissions(agentId int) {
	if common.AgentCommissionMatureMinutes <= 0 {
		return
	}
	// 风控冻结的代理暂停结转：pending 停在原地（入账不停，证据链保留），
	// 解除管制后下次结转恢复正常成熟。
	if IsCommissionAssetsFrozen(agentId) {
		return
	}
	cutoff := time.Now().Unix() - int64(common.AgentCommissionMatureMinutes)*60
	var due []Commission
	if err := DB.Where("agent_id = ? AND status = ? AND created_at <= ?",
		agentId, CommissionStatusPending, cutoff).Limit(500).Find(&due).Error; err != nil {
		common.SysLog("failed to load maturing commissions: " + err.Error())
		return
	}
	for _, row := range due {
		if row.Quota <= 0 || row.Quota > common.MaxQuota {
			common.SysLog("failed to mature agent commission: invalid quota")
			continue
		}
		err := DB.Transaction(func(tx *gorm.DB) error {
			// Risk controls serialize on the user row. Recheck after taking that
			// same lock so a concurrent freeze cannot be bypassed mid-maturity.
			var user User
			if err := lockForUpdate(tx).Select("id").Where("id = ?", agentId).First(&user).Error; err != nil {
				return err
			}
			frozen, err := isCommissionAssetsFrozenTx(tx, agentId)
			if err != nil {
				return err
			}
			if frozen {
				return nil
			}
			res := tx.Model(&Commission{}).
				Where("id = ? AND status = ?", row.Id, CommissionStatusPending).
				Update("status", CommissionStatusMatured)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil // 已被并发结转
			}
			update := tx.Model(&User{}).
				Where("id = ? AND commission_quota <= ?", agentId, common.MaxQuota-row.Quota).
				Update("commission_quota", gorm.Expr("commission_quota + ?", row.Quota))
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected == 0 {
				return ErrQuotaOverflow
			}
			return nil
		})
		if err != nil {
			common.SysLog("failed to mature agent commission: " + err.Error())
		}
	}
}

// GetAgentPendingCommission 成熟期内(未结转)的分润总和，钱包"待成熟"展示用。
func GetAgentPendingCommission(agentId int) (pending int64) {
	row := DB.Model(&Commission{}).
		Where("agent_id = ? AND status = ?", agentId, CommissionStatusPending).
		Select("COALESCE(SUM(quota),0)").Row()
	_ = row.Scan(&pending)
	return
}

// taskCommissionSourceParts parses generated suffix fields from the right
// because upstream task IDs may contain colons. The returned prefix includes
// the separator after the task ID.
func taskCommissionSourceParts(sourceKey string) (prefix string, event string, quota int, ok bool) {
	const taskPrefix = "task:"
	if !strings.HasPrefix(sourceKey, taskPrefix) {
		return "", "", 0, false
	}
	quotaSeparator := strings.LastIndex(sourceKey, ":")
	if quotaSeparator <= len(taskPrefix) || quotaSeparator == len(sourceKey)-1 {
		return "", "", 0, false
	}
	logTypeSeparator := strings.LastIndex(sourceKey[:quotaSeparator], ":")
	if logTypeSeparator <= len(taskPrefix) || logTypeSeparator == quotaSeparator-1 {
		return "", "", 0, false
	}
	quota, err := strconv.Atoi(sourceKey[quotaSeparator+1:])
	if err != nil || quota <= 0 {
		return "", "", 0, false
	}
	return sourceKey[:logTypeSeparator+1], sourceKey[logTypeSeparator+1 : quotaSeparator], quota, true
}

// TaskCommissionSettlementEvent uniquely identifies one task quota transition.
// Quotas are persisted as signed 32-bit values, so packing both uint32 bit
// patterns is collision-free and keeps the source key inside varchar(96).
func TaskCommissionSettlementEvent(previousQuota int, actualQuota int) string {
	transition := uint64(uint32(previousQuota))<<32 | uint64(uint32(actualQuota))
	return "s" + strconv.FormatUint(transition, 36)
}

// BuildTaskCommissionSourceKey preserves readable task IDs while they fit the
// varchar(96) column. Long provider IDs use a stable channel-scoped digest so
// initial, settlement, and refund events still share one ownership prefix.
func BuildTaskCommissionSourceKey(channelId int, taskID string, event string, quota int) string {
	if taskID == "" || event == "" || strings.Contains(event, ":") ||
		utf8.RuneCountInString(event) > 14 || quota <= 0 || quota > common.MaxQuota {
		return ""
	}
	taskScope := "c" + strconv.Itoa(channelId) + ":" + taskID
	sourcePrefix := "task:" + taskScope + ":"
	// Choose the identity representation independently of the current event;
	// otherwise an initial key can fit while a longer settlement key hashes,
	// splitting one task across two ownership prefixes.
	if utf8.RuneCountInString(sourcePrefix)+14+1+10 <= 96 {
		return fmt.Sprintf("%s%s:%d", sourcePrefix, event, quota)
	}
	taskIdentity := sha256.Sum256([]byte(taskScope))
	return fmt.Sprintf("task:h%x:%s:%d", taskIdentity, event, quota)
}

func taskCommissionSettlementQuotas(event string) (previousQuota int, actualQuota int, ok bool) {
	if len(event) < 2 || event[0] != 's' {
		return 0, 0, false
	}
	transition, err := strconv.ParseUint(event[1:], 36, 64)
	if err != nil {
		return 0, 0, false
	}
	previous := uint32(transition >> 32)
	actual := uint32(transition)
	if previous > 1<<31-1 || actual > 1<<31-1 {
		return 0, 0, false
	}
	return int(previous), int(actual), true
}

// originalCommissionForReversal finds the first positive commission generated
// by the same task. It is the ownership/rate snapshot for every later delta and
// refund even if the invitee is rebound or the agent's rate changes.
// sourceKey 形如 task:<task_id>:<log_type>:<quota>，同任务的消费与退款键仅后两段不同，
// 因此按 task:<task_id>: 前缀反查原始正数流水。
func originalCommissionForReversal(fromUserId int, sourceKey string) *Commission {
	sourcePrefix, _, _, ok := taskCommissionSourceParts(sourceKey)
	if !ok {
		return nil
	}
	var orig Commission
	result := DB.Where("from_user_id = ? AND quota > 0 AND SUBSTR(source_key, 1, ?) = ?",
		fromUserId, utf8.RuneCountInString(sourcePrefix), sourcePrefix).
		Order("id asc").Limit(1).Find(&orig)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil
	}
	return &orig
}

func commissionSnapshotRate(original *Commission) float64 {
	if original == nil {
		return 0
	}
	if original.Rate > 0 {
		return original.Rate
	}
	if original.SourceKey != nil {
		if _, _, consumedQuota, ok := taskCommissionSourceParts(*original.SourceKey); ok && original.Quota > 0 {
			return float64(original.Quota) / float64(consumedQuota)
		}
	}
	if agent, err := GetUserById(original.AgentId, false); err == nil && agent != nil {
		return agent.UsageProfitRate
	}
	return 0
}

func commissionReversalTarget(fromUserId int, sourceKey string) (int, float64) {
	if strings.HasPrefix(sourceKey, "task:") {
		orig := originalCommissionForReversal(fromUserId, sourceKey)
		if orig == nil {
			// A task refund without a matching positive commission has nothing to
			// reverse. Charging the current inviter here can debit an unrelated
			// agent after rebinding or when the original credit was ineligible.
			return 0, 0
		}
		return orig.AgentId, commissionSnapshotRate(orig)
	}

	// Legacy/no-source reversals have no persisted ownership snapshot. Preserve
	// their historical fallback to the invitee's current inviter and rate.
	fromUser, err := GetUserById(fromUserId, false)
	if err != nil || fromUser == nil || fromUser.InviterId <= 0 || fromUser.InviterId == fromUserId {
		return 0, 0
	}
	agent, err := GetUserById(fromUser.InviterId, false)
	if err != nil || agent == nil {
		return 0, 0
	}
	return agent.Id, agent.UsageProfitRate
}

// RecordAgentCommissionReversal 下级消费发生退款（任务失败退款/差额下调）时，
// 按原始入账比例回冲上级代理的分润，否则代理可靠"提交必失败的任务"白刷分润。
// 与入账不同，回冲不检查代理当前状态/身份：代理被封禁或撤销后，已入账的分润
// 依然要为退款负责，否则"先刷佣再退出"可白留佣金。
// 回冲立即生效（负数已成熟流水），可提现余额允许被冲负（作为欠账抵扣后续分润）；
// sourceKey 非空时同样幂等，防止同一退款重复回冲。
func RecordAgentCommissionReversal(fromUserId int, refundedQuota int, sourceKey string) {
	if fromUserId <= 0 || refundedQuota <= 0 {
		return
	}
	agentId, rate := commissionReversalTarget(fromUserId, sourceKey)
	if agentId <= 0 || rate <= 0 {
		return
	}
	commission := common.QuotaFromFloat(float64(refundedQuota) * rate)
	if strings.HasPrefix(sourceKey, "task:") {
		sourcePrefix, event, _, ok := taskCommissionSourceParts(sourceKey)
		if !ok {
			return
		}
		if previousQuota, actualQuota, settlement := taskCommissionSettlementQuotas(event); settlement {
			if actualQuota >= previousQuota {
				return
			}
			commission = common.QuotaFromFloat(float64(previousQuota)*rate) -
				common.QuotaFromFloat(float64(actualQuota)*rate)
		} else if event == "final" {
			var outstanding int64
			err := DB.Model(&Commission{}).
				Where("agent_id = ? AND from_user_id = ? AND SUBSTR(source_key, 1, ?) = ?",
					agentId, fromUserId, utf8.RuneCountInString(sourcePrefix), sourcePrefix).
				Select("COALESCE(SUM(quota), 0)").Scan(&outstanding).Error
			if err != nil {
				common.SysLog("failed to calculate outstanding task commission: " + err.Error())
				return
			}
			if outstanding <= 0 || outstanding > 1<<31-1 {
				return
			}
			commission = int(outstanding)
		}
	}
	if commission <= 0 {
		return
	}
	var keyPtr *string
	if sourceKey != "" {
		if n := int64(0); DB.Model(&Commission{}).
			Where("from_user_id = ? AND source_key = ?", fromUserId, sourceKey).
			Count(&n).Error == nil && n > 0 {
			return
		}
		keyPtr = &sourceKey
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 回冲不设非负下限：这是二开原作者有测试背书的设计契约
		// （TestRecordAgentCommissionReversal 断言全额退款可把余额冲负，作为欠账抵扣
		// 后续分润；TestReversalFallbackToCurrentRate / TestCommissionComplianceGate
		// 断言无来源键时降级用当前费率、回冲不受合规限制）。故按原始比例无条件扣减。
		if err := tx.Create(&Commission{
			AgentId:    agentId,
			FromUserId: fromUserId,
			Quota:      -commission, // 负数流水 = 回冲
			Status:     CommissionStatusMatured,
			SourceKey:  keyPtr,
			Rate:       rate,
		}).Error; err != nil {
			return err
		}
		update := tx.Model(&User{}).
			Where("id = ? AND commission_quota >= ? AND commission_history_quota >= ?",
				agentId, common.MinQuota+commission, common.MinQuota+commission).
			Updates(map[string]interface{}{
				"commission_quota":         gorm.Expr("commission_quota - ?", commission),
				"commission_history_quota": gorm.Expr("commission_history_quota - ?", commission),
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return ErrQuotaOverflow
		}
		return nil
	})
	if err != nil {
		if keyPtr != nil && isDuplicateKeyError(err) {
			return // 唯一索引兜住的并发重复回冲：幂等成功，静默返回
		}
		common.SysLog("failed to record agent commission reversal: " + err.Error())
	}
}

// ---- 超管：代理管理 ----

// SetUserAgent 将用户设为代理并设定分润比例。
func SetUserAgent(userId int, agentType string, usageProfitRate float64) error {
	return DB.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"agent_type":        agentType,
		"usage_profit_rate": usageProfitRate,
	}).Error
}

// RevokeUserAgent 撤销代理身份（保留已累计分润余额）。
func RevokeUserAgent(userId int) error {
	return DB.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"agent_type":        "",
		"usage_profit_rate": 0,
	}).Error
}

// ListAgents 分页列出所有代理（agent_type 非空），可按用户名/显示名搜索、按状态筛选（status<=0 不筛选），并回填名下用户数。
func ListAgents(keyword string, status int, offset int, limit int) (agents []*User, total int64, err error) {
	tx := DB.Model(&User{}).Where("agent_type <> ?", "")
	if keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where("username LIKE ? OR display_name LIKE ?", like, like)
	}
	if status > 0 {
		tx = tx.Where("status = ?", status)
	}
	if err = tx.Count(&total).Error; err != nil {
		return
	}
	if err = tx.Order("id desc").Limit(limit).Offset(offset).Omit("password").Find(&agents).Error; err != nil {
		return
	}
	// 一次 GROUP BY 回填每个代理绑定的下级用户数（inviter_id 归属）。
	if len(agents) > 0 {
		ids := make([]int, 0, len(agents))
		for _, a := range agents {
			ids = append(ids, a.Id)
		}
		var rows []struct {
			InviterId int
			Cnt       int64
		}
		if cErr := DB.Model(&User{}).
			Select("inviter_id, COUNT(*) AS cnt").
			Where("inviter_id IN ?", ids).
			Group("inviter_id").
			Scan(&rows).Error; cErr != nil {
			common.SysLog("failed to fill agent downstream counts: " + cErr.Error())
			return
		}
		cntById := make(map[int]int64, len(rows))
		for _, r := range rows {
			cntById[r.InviterId] = r.Cnt
		}
		for _, a := range agents {
			a.DownstreamCount = cntById[a.Id]
		}
	}
	return
}

// ---- 代理自助 ----

// GetAgentDownstreamUsers 代理名下用户（inviter_id = agentId）。
// 代理只是"高级客户"而非管理员：只暴露展示所需的白名单字段，
// 不能把下级的 email/OAuth 绑定/管理员备注/setting 等敏感列泄给代理。
func GetAgentDownstreamUsers(agentId int, keyword string, status int, offset int, limit int) (users []*User, total int64, err error) {
	tx := DB.Model(&User{}).Where("inviter_id = ?", agentId)
	if keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where("username LIKE ? OR display_name LIKE ?", like, like)
	}
	if status > 0 {
		tx = tx.Where("status = ?", status)
	}
	if err = tx.Count(&total).Error; err != nil {
		return
	}
	err = tx.Order("id desc").Limit(limit).Offset(offset).
		Select([]string{"id", "username", "display_name", commonGroupCol, "quota", "used_quota", "request_count", "status", "created_at", "last_login_at"}).
		Find(&users).Error
	return
}

// GetAgentDownstreamStats 代理名下用户的聚合统计（用户数由分页 total 提供，
// 这里补余额与消耗总和，供「我的用户」页顶部统计卡展示）。
func GetAgentDownstreamStats(agentId int) (totalQuota int64, totalUsedQuota int64, err error) {
	row := DB.Model(&User{}).Where("inviter_id = ?", agentId).
		Select("COALESCE(SUM(quota),0), COALESCE(SUM(used_quota),0)").Row()
	err = row.Scan(&totalQuota, &totalUsedQuota)
	return
}

// GetAgentCommissions 代理分润流水（agent_id = agentId），并批量回填来源用户名
// （一次 IN 查询，失败仅降级为不显示用户名，不报错）。
func GetAgentCommissions(agentId int, offset int, limit int) (records []*Commission, total int64, err error) {
	tx := DB.Model(&Commission{}).Where("agent_id = ?", agentId)
	if err = tx.Count(&total).Error; err != nil {
		return
	}
	if err = tx.Order("id desc").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return
	}
	if len(records) == 0 {
		return
	}
	idSet := make(map[int]struct{}, len(records))
	ids := make([]int, 0, len(records))
	for _, r := range records {
		if _, ok := idSet[r.FromUserId]; !ok {
			idSet[r.FromUserId] = struct{}{}
			ids = append(ids, r.FromUserId)
		}
	}
	var users []struct {
		Id       int
		Username string
	}
	if qerr := DB.Model(&User{}).Select("id", "username").Where("id IN ?", ids).Find(&users).Error; qerr != nil {
		common.SysLog("failed to fill commission usernames: " + qerr.Error())
		return
	}
	nameById := make(map[int]string, len(users))
	for _, u := range users {
		nameById[u.Id] = u.Username
	}
	for _, r := range records {
		r.FromUsername = nameById[r.FromUserId]
	}
	return
}

// ---- 代理自助：我的用户 ----
// 代理与普通用户同权:对名下用户只有只读可见性,无任何管理操作
// (启停用户属管理员权限,历史版本曾开放给代理,已按治理决策移除)。
