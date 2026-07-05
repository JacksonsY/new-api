package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

// 批量用户运营操作（代理/分销管下级）：批量换分组、批量调余额。
// 逐用户独立执行、逐用户角色守卫，跳过的用户带稳定原因码返回；
// 余额调整全部走条件原子 UPDATE（不用 FOR UPDATE），multiply 用 CAS 防并发覆盖。

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
		if err := DB.Model(&User{}).Where("id = ?", user.Id).Update("group", group).Error; err != nil {
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
// subtract 余额不足跳过不透支；multiply 以 CAS 提交，并发冲突跳过。
func BatchAdjustUserQuota(operatorRole int, userIds []int, mode string, amount int, factor float64, adminInfo map[string]interface{}) (*BatchUserOpResult, error) {
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
			newQuota = oldQuota + amount
			updateErr = DB.Model(&User{}).Where("id = ?", user.Id).
				Update("quota", gorm.Expr("quota + ?", amount)).Error
		case BatchQuotaModeSubtract:
			newQuota = oldQuota - amount
			res := DB.Model(&User{}).Where("id = ? AND quota >= ?", user.Id, amount).
				Update("quota", gorm.Expr("quota - ?", amount))
			updateErr = res.Error
			if updateErr == nil && res.RowsAffected == 0 {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonInsufficientQuota})
			}
		case BatchQuotaModeOverride:
			newQuota = amount
			updateErr = DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", amount).Error
		case BatchQuotaModeMultiply:
			newQuota = int(float64(oldQuota) * factor)
			if newQuota < 0 {
				newQuota = 0
			}
			res := DB.Model(&User{}).Where("id = ? AND quota = ?", user.Id, oldQuota).
				Update("quota", newQuota)
			updateErr = res.Error
			if updateErr == nil && res.RowsAffected == 0 {
				applied = false
				result.Skipped = append(result.Skipped, BatchUserSkip{UserId: user.Id, Reason: BatchSkipReasonConflict})
			}
		default:
			return nil, errors.New("invalid batch quota mode")
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
