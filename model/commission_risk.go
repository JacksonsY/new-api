package model

// jzlh-agent 分销风控管制：对疑似作弊代理的"调查期间中间态"，比封号轻、比放任重。
//   - freeze_assets    冻结分润资产：提现/转额度入口拦截、pending 分润暂停结转、
//     现存待审核提现单批量拒绝并退回（余额本身已被冻，退回也出不去）。
//     入账不停：钱照记但被锁住，保留证据链（与 moeacgx"结算进 risk_frozen 桶"同思路，
//     我们无多桶，用"停结转+封出口"等价实现）。
//   - block_invite_code 封邀请码：aff_code 解析直接失效，新注册不再绑定该代理
//     （注册与 OAuth 两条绑定路径都经 GetUserIdByAffCode，天然全覆盖）。
//
// 每用户一行（user_id 唯一），解除后 status=removed，再次管制原行复活。
// 所有处置动作写 CommissionRiskEvent 全量留痕——动的是钱，必须可审计。

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	CommissionRiskStatusActive  = "active"
	CommissionRiskStatusRemoved = "removed"
)

const (
	RiskEventApply  = "apply"
	RiskEventRemove = "remove"
)

var (
	ErrCommissionAssetsFrozen = errors.New("commission assets frozen")
	ErrRiskUserNotFound       = errors.New("risk user not found")
	ErrRiskNoActionSelected   = errors.New("no risk action selected")
)

type CommissionRiskUser struct {
	Id              int    `json:"id" gorm:"primaryKey"`
	UserId          int    `json:"user_id" gorm:"uniqueIndex"`
	Status          string `json:"status" gorm:"type:varchar(16);index;default:active"`
	FreezeAssets    bool   `json:"freeze_assets"`
	BlockInviteCode bool   `json:"block_invite_code"`
	Reason          string `json:"reason" gorm:"type:varchar(255)"`
	AdminId         int    `json:"admin_id"`
	RemovedBy       int    `json:"removed_by"`
	RemoveRemark    string `json:"remove_remark" gorm:"type:varchar(255)"`
	RemovedAt       int64  `json:"removed_at"`
	CreatedAt       int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       int64  `json:"updated_at" gorm:"autoUpdateTime"`
	// 列表展示用回填，不落库
	Username string `json:"username,omitempty" gorm:"-"`
}

func (CommissionRiskUser) TableName() string {
	return "commission_risk_users"
}

// CommissionRiskEvent 风控/反欺诈处置事件留痕（apply/remove/unbind/clawback/dismiss）。
type CommissionRiskEvent struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	UserId    int    `json:"user_id" gorm:"index"` // 被处置的代理
	Action    string `json:"action" gorm:"type:varchar(32);index"`
	AdminId   int    `json:"admin_id"`
	Detail    string `json:"detail" gorm:"type:text"` // JSON
	CreatedAt int64  `json:"created_at" gorm:"autoCreateTime;index"`
}

func (CommissionRiskEvent) TableName() string {
	return "commission_risk_events"
}

func createCommissionRiskEventTx(tx *gorm.DB, userId int, adminId int, action string, detail map[string]interface{}) error {
	detailText := ""
	if detail != nil {
		bytes, err := common.Marshal(detail)
		if err != nil {
			return err
		}
		detailText = string(bytes)
	}
	return tx.Create(&CommissionRiskEvent{
		UserId:  userId,
		Action:  action,
		AdminId: adminId,
		Detail:  detailText,
	}).Error
}

// IsCommissionAssetsFrozen 分润资产是否被风控冻结（提现/转额度/结转入口用）。
func IsCommissionAssetsFrozen(userId int) bool {
	if userId <= 0 {
		return false
	}
	frozen, err := isCommissionAssetsFrozenTx(DB, userId)
	return err == nil && frozen
}

func isCommissionAssetsFrozenTx(tx *gorm.DB, userId int) (bool, error) {
	var count int64
	err := tx.Model(&CommissionRiskUser{}).
		Where("user_id = ? AND status = ? AND freeze_assets = ?", userId, CommissionRiskStatusActive, true).
		Count(&count).Error
	return count > 0, err
}

// IsInviteCodeBlocked 邀请码是否被风控封禁（aff_code 解析入口用）。
func IsInviteCodeBlocked(userId int) bool {
	if userId <= 0 {
		return false
	}
	var count int64
	_ = DB.Model(&CommissionRiskUser{}).
		Where("user_id = ? AND status = ? AND block_invite_code = ?", userId, CommissionRiskStatusActive, true).
		Count(&count).Error
	return count > 0
}

// ApplyCommissionRiskControls 施加风控管制（幂等 OR 叠加：已 freeze 再 block 不会互相清掉）。
// freeze 生效时批量拒绝该用户全部待审核提现单并退回预扣余额——余额出口已封，退回出不去；
// 已被认领(打款中)的单不动，线下转账可能已发生，交给审批人经既有流程裁决。
// 返回本次自动拒绝的提现单数。
func ApplyCommissionRiskControls(userId int, adminId int, freeze bool, block bool, reason string) (int, error) {
	if !freeze && !block {
		return 0, ErrRiskNoActionSelected
	}
	rejected := 0
	refundDeferred := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Every commission asset exit takes this same user-row lock before
		// checking risk state. This gives freeze a single linearization point:
		// exits committed first are included in the pending-withdrawal sweep;
		// exits ordered after it observe the active freeze and fail closed.
		var user User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", userId).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRiskUserNotFound
			}
			return err
		}

		var risk CommissionRiskUser
		err := tx.Where("user_id = ?", userId).First(&risk).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			risk = CommissionRiskUser{
				UserId:          userId,
				Status:          CommissionRiskStatusActive,
				FreezeAssets:    freeze,
				BlockInviteCode: block,
				Reason:          strings.TrimSpace(reason),
				AdminId:         adminId,
			}
			if err := tx.Create(&risk).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			// 已 active 的行做 OR 叠加（先 freeze 再 block 不互相清掉）；
			// removed 的行复活时旧 flags 不继承，按本次请求从零开始。
			newFreeze, newBlock := freeze, block
			if risk.Status == CommissionRiskStatusActive {
				newFreeze = risk.FreezeAssets || freeze
				newBlock = risk.BlockInviteCode || block
			}
			if err := tx.Model(&risk).Updates(map[string]interface{}{
				"status":            CommissionRiskStatusActive,
				"freeze_assets":     newFreeze,
				"block_invite_code": newBlock,
				"reason":            strings.TrimSpace(reason),
				"admin_id":          adminId,
				"removed_by":        0,
				"remove_remark":     "",
				"removed_at":        0,
			}).Error; err != nil {
				return err
			}
		}

		if freeze {
			n, deferred, err := rejectPendingWithdrawalsTx(tx, userId, adminId)
			if err != nil {
				return err
			}
			rejected = n
			refundDeferred = deferred
		}

		return createCommissionRiskEventTx(tx, userId, adminId, RiskEventApply, map[string]interface{}{
			"freeze_assets":        freeze,
			"block_invite_code":    block,
			"reason":               strings.TrimSpace(reason),
			"rejected_withdrawals": rejected,
			"refund_deferred":      refundDeferred,
		})
	})
	if err != nil {
		return 0, err
	}
	return rejected, nil
}

// rejectPendingWithdrawalsTx 逐单条件更新拒绝待审核提现并退回预扣余额（与人工 reject
// 同一并发闸门：只有赢得 pending→rejected 迁移的更新执行退款，天然只退一次）。
// 若退款会超过余额上限，则保留 pending 单供余额腾出空间后处理；冻结本身不能因此回滚。
func rejectPendingWithdrawalsTx(tx *gorm.DB, userId int, adminId int) (int, int, error) {
	var pending []Withdrawal
	if err := lockForUpdate(tx).
		Where("user_id = ? AND status = ?", userId, WithdrawalPending).
		Order("id asc").Find(&pending).Error; err != nil {
		return 0, 0, err
	}
	var user User
	if err := tx.Select("id", "commission_quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return 0, 0, err
	}
	rejected := 0
	deferred := 0
	for _, w := range pending {
		if !commissionRefundFits(user.CommissionQuota, w.Amount) {
			deferred++
			common.SysLog("risk freeze deferred withdrawal refund due to quota overflow: user=" +
				strconv.Itoa(userId) + " withdrawal=" + strconv.Itoa(w.Id))
			continue
		}
		res := tx.Model(&Withdrawal{}).
			Where("id = ? AND status = ?", w.Id, WithdrawalPending).
			Updates(map[string]interface{}{
				"status":       WithdrawalRejected,
				"admin_remark": "风控冻结，自动拒绝",
				"reviewer_id":  adminId,
			})
		if res.Error != nil {
			return rejected, deferred, res.Error
		}
		if res.RowsAffected == 0 {
			continue // 已被并发处理
		}
		refund := tx.Model(&User{}).
			Where("id = ? AND commission_quota <= ?", w.UserId, maxQuotaBalance-w.Amount).
			Update("commission_quota", gorm.Expr("commission_quota + ?", w.Amount))
		if refund.Error != nil {
			return rejected, deferred, refund.Error
		}
		if refund.RowsAffected == 0 {
			return rejected, deferred, ErrQuotaOverflow
		}
		user.CommissionQuota += w.Amount
		rejected++
	}
	return rejected, deferred, nil
}

// RemoveCommissionRiskControls 解除风控管制：flags 清零、状态置 removed，留痕。
// pending 分润在下次结转时恢复正常成熟，无需补偿动作。
func RemoveCommissionRiskControls(userId int, adminId int, remark string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", userId).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRiskUserNotFound
			}
			return err
		}
		res := tx.Model(&CommissionRiskUser{}).
			Where("user_id = ? AND status = ?", userId, CommissionRiskStatusActive).
			Updates(map[string]interface{}{
				"status":            CommissionRiskStatusRemoved,
				"freeze_assets":     false,
				"block_invite_code": false,
				"removed_by":        adminId,
				"remove_remark":     strings.TrimSpace(remark),
				"removed_at":        common.GetTimestamp(),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrRiskUserNotFound
		}
		return createCommissionRiskEventTx(tx, userId, adminId, RiskEventRemove, map[string]interface{}{
			"remark": strings.TrimSpace(remark),
		})
	})
}

// ListCommissionRiskUsers 分页列出风控用户（默认仅 active；status="all" 含已解除）。
func ListCommissionRiskUsers(keyword string, status string, startIdx int, num int) ([]*CommissionRiskUser, int64, error) {
	query := DB.Model(&CommissionRiskUser{})
	switch strings.TrimSpace(status) {
	case "", CommissionRiskStatusActive:
		query = query.Where("status = ?", CommissionRiskStatusActive)
	case CommissionRiskStatusRemoved:
		query = query.Where("status = ?", CommissionRiskStatusRemoved)
	case "all":
	default:
		query = query.Where("status = ?", CommissionRiskStatusActive)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		userIds, err := findFraudMatchedUserIds(keyword)
		if err != nil {
			return nil, 0, err
		}
		if len(userIds) == 0 {
			return []*CommissionRiskUser{}, 0, nil
		}
		query = query.Where("user_id IN ?", userIds)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var risks []*CommissionRiskUser
	if err := query.Order("id desc").Offset(startIdx).Limit(num).Find(&risks).Error; err != nil {
		return nil, 0, err
	}
	fillRiskUsernames(risks)
	return risks, total, nil
}

func fillRiskUsernames(risks []*CommissionRiskUser) {
	if len(risks) == 0 {
		return
	}
	ids := make([]int, 0, len(risks))
	for _, r := range risks {
		ids = append(ids, r.UserId)
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
	for _, r := range risks {
		r.Username = nameById[r.UserId]
	}
}

// ListCommissionRiskEvents 分页列出处置事件（可按用户过滤，0=全部）。
func ListCommissionRiskEvents(userId int, startIdx int, num int) ([]*CommissionRiskEvent, int64, error) {
	query := DB.Model(&CommissionRiskEvent{})
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []*CommissionRiskEvent
	if err := query.Order("id desc").Offset(startIdx).Limit(num).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}
