package model

import (
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

// 批量用户运营操作（代理/分销管下级）：批量换分组、批量调余额。
// 逐用户独立执行、逐用户角色守卫，跳过的用户带稳定原因码返回；
// 余额调整全部走快照 CAS（不用 FOR UPDATE），避免覆盖并发消费或充值。

const (
	BatchQuotaModeAdd      = "add"
	BatchQuotaModeSubtract = "subtract"
	BatchQuotaModeOverride = "override"
	BatchQuotaModeMultiply = "multiply"

	// 跳过原因码（前端按码做 i18n 展示）
	BatchSkipReasonNotFound          = "not_found"
	BatchSkipReasonNoPermission      = "no_permission"
	BatchSkipReasonInsufficientQuota = "insufficient_quota"
	BatchSkipReasonConflict          = "conflict"
	BatchSkipReasonNoChange          = "no_change"
)

// ValidateBatchQuotaAdjustment validates the public contract for batch quota
// changes. Keep this in the model layer as defense in depth: HTTP validation is
// not the only possible caller of the balance mutation.
func ValidateBatchQuotaAdjustment(mode string, amount int, factor float64) error {
	switch mode {
	case BatchQuotaModeAdd, BatchQuotaModeSubtract:
		if amount <= 0 || amount > common.MaxQuota {
			return errors.New("invalid batch quota amount")
		}
	case BatchQuotaModeOverride:
		if amount < 0 || amount > common.MaxQuota {
			return errors.New("invalid batch quota amount")
		}
	case BatchQuotaModeMultiply:
		if math.IsNaN(factor) || math.IsInf(factor, 0) || factor <= 0 || factor > 100 {
			return errors.New("invalid batch quota factor")
		}
	default:
		return errors.New("invalid batch quota mode")
	}
	return nil
}

type BatchUserSkip struct {
	UserId int    `json:"user_id"`
	Reason string `json:"reason"`
}

type BatchUserOpResult struct {
	UpdatedCount int             `json:"updated_count"`
	Skipped      []BatchUserSkip `json:"skipped"`
}

// canOperatorManage 与 controller 的 canManageTargetRole 同一规则：root 管所有，其余只能管更低角色。
func canOperatorManage(operatorRole int, targetRole int) bool {
	return operatorRole == common.RoleRootUser || operatorRole > targetRole
}

// loadBatchTargets 按 id 加载目标用户快照，缺失的记入 skipped。
func loadBatchTargets(userIds []int, result *BatchUserOpResult) ([]*User, error) {
	var users []*User
	if err := DB.Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return nil, err
	}
	found := make(map[int]bool, len(users))
	for _, user := range users {
		found[user.Id] = true
	}
	for _, id := range userIds {
		if !found[id] {
			result.Skipped = append(result.Skipped, BatchUserSkip{UserId: id, Reason: BatchSkipReasonNotFound})
		}
	}
	return users, nil
}

// BatchUpdateUserGroup 把一批用户切换到目标分组。调用方需先校验分组存在。
func BatchUpdateUserGroup(operatorRole int, userIds []int, group string, adminInfo map[string]interface{}) (*BatchUserOpResult, error) {
	result := &BatchUserOpResult{Skipped: make([]BatchUserSkip, 0)}
	users, err := loadBatchTargets(userIds, result)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		if !canOperatorManage(operatorRole, user.Role) {
			result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonNoPermission})
			continue
		}
		if user.Group == group {
			result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonNoChange})
			continue
		}
		res := DB.Model(&User{}).
			Where(fmt.Sprintf("id = ? AND role = ? AND %s = ?", commonGroupCol), user.Id, user.Role, user.Group).
			Update("group", group)
		if res.Error != nil || res.RowsAffected == 0 {
			err := res.Error
			if err == nil {
				err = errors.New("batch group target changed concurrently")
			}
			common.SysError(fmt.Sprintf("batch group update failed: user=%d, err=%v", user.Id, err))
			result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonConflict})
			continue
		}
		if err := InvalidateUserCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", user.Id, err.Error()))
		}
		RecordLogWithAdminInfo(user.Id, LogTypeManage,
			fmt.Sprintf("管理员批量调整分组：%s → %s", user.Group, group), adminInfo)
		result.UpdatedCount++
	}
	return result, nil
}

// BatchAdjustUserQuota 批量调整用户余额。
// add/subtract 用 amount（>0）；override 用 amount（>=0）；multiply 用 factor（>0）。
// subtract 余额不足跳过不透支；所有模式以 CAS 提交，并发冲突跳过。
func BatchAdjustUserQuota(operatorRole int, userIds []int, mode string, amount int, factor float64, adminInfo map[string]interface{}) (*BatchUserOpResult, error) {
	if err := ValidateBatchQuotaAdjustment(mode, amount, factor); err != nil {
		return nil, err
	}
	result := &BatchUserOpResult{Skipped: make([]BatchUserSkip, 0)}
	users, err := loadBatchTargets(userIds, result)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		if !canOperatorManage(operatorRole, user.Role) {
			result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonNoPermission})
			continue
		}
		oldQuota := user.Quota
		var newQuota int
		var updateErr error
		applied := true
		switch mode {
		case BatchQuotaModeAdd:
			newQuota = common.QuotaFromFloat(float64(oldQuota) + float64(amount))
			if newQuota == oldQuota {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonNoChange})
				break
			}
			res := DB.Model(&User{}).Where("id = ? AND role = ? AND quota = ?", user.Id, user.Role, oldQuota).
				Update("quota", newQuota)
			updateErr = res.Error
			if updateErr == nil && res.RowsAffected == 0 {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonConflict})
			}
		case BatchQuotaModeSubtract:
			if oldQuota < amount {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonInsufficientQuota})
				break
			}
			newQuota = oldQuota - amount
			res := DB.Model(&User{}).Where("id = ? AND role = ? AND quota = ?", user.Id, user.Role, oldQuota).
				Update("quota", newQuota)
			updateErr = res.Error
			if updateErr == nil && res.RowsAffected == 0 {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonConflict})
			}
		case BatchQuotaModeOverride:
			newQuota = amount
			if newQuota == oldQuota {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonNoChange})
				break
			}
			res := DB.Model(&User{}).Where("id = ? AND role = ? AND quota = ?", user.Id, user.Role, oldQuota).
				Update("quota", newQuota)
			updateErr = res.Error
			if updateErr == nil && res.RowsAffected == 0 {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonConflict})
			}
		case BatchQuotaModeMultiply:
			newQuota = common.QuotaFromFloat(float64(oldQuota) * factor)
			if newQuota < 0 {
				newQuota = 0
			}
			if newQuota == oldQuota {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonNoChange})
				break
			}
			res := DB.Model(&User{}).Where("id = ? AND role = ? AND quota = ?", user.Id, user.Role, oldQuota).
				Update("quota", newQuota)
			updateErr = res.Error
			if updateErr == nil && res.RowsAffected == 0 {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonConflict})
			}
		}
		if updateErr != nil {
			common.SysError(fmt.Sprintf("batch quota update failed: user=%d, mode=%s, err=%v", user.Id, mode, updateErr))
			result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonConflict})
			continue
		}
		if !applied {
			continue
		}
		if err := InvalidateUserCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", user.Id, err.Error()))
		}
		RecordLogWithAdminInfo(user.Id, LogTypeManage,
			fmt.Sprintf("管理员批量调整额度：%s → %s", logger.FormatQuota(oldQuota), logger.FormatQuota(newQuota)), adminInfo)
		result.UpdatedCount++
	}
	return result, nil
}
