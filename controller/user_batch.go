package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

const batchUserOpMaxTargets = 1000

type batchUserGroupRequest struct {
	UserIds []int  `json:"user_ids"`
	Group   string `json:"group"`
}

type batchUserQuotaRequest struct {
	UserIds []int   `json:"user_ids"`
	Mode    string  `json:"mode"`
	Amount  int     `json:"amount"`
	Factor  float64 `json:"factor"`
}

// normalizeBatchUserIds 去重并校验数量上限。
func normalizeBatchUserIds(userIds []int) []int {
	seen := make(map[int]bool, len(userIds))
	normalized := make([]int, 0, len(userIds))
	for _, id := range userIds {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		normalized = append(normalized, id)
	}
	return normalized
}

// BatchUpdateUserGroup 批量把用户切换到指定分组（代理管下级效率件）。
func BatchUpdateUserGroup(c *gin.Context) {
	var req batchUserGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req.UserIds = normalizeBatchUserIds(req.UserIds)
	if len(req.UserIds) == 0 || len(req.UserIds) > batchUserOpMaxTargets {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Group == "" || !ratio_setting.ContainsGroupRatio(req.Group) {
		common.ApiError(c, fmt.Errorf("分组 %s 不存在（未配置分组倍率）", req.Group))
		return
	}

	result, err := model.BatchUpdateUserGroup(c.GetInt("role"), req.UserIds, req.Group, auditOperatorInfo(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "user.batch_group", map[string]interface{}{
		"group":         req.Group,
		"target_count":  len(req.UserIds),
		"updated_count": result.UpdatedCount,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}

// BatchAdjustUserQuota 批量调整用户余额（add/subtract/override/multiply）。
func BatchAdjustUserQuota(c *gin.Context) {
	var req batchUserQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req.UserIds = normalizeBatchUserIds(req.UserIds)
	if len(req.UserIds) == 0 || len(req.UserIds) > batchUserOpMaxTargets {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.ValidateBatchQuotaAdjustment(req.Mode, req.Amount, req.Factor); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	result, err := model.BatchAdjustUserQuota(c.GetInt("role"), req.UserIds, req.Mode, req.Amount, req.Factor, auditOperatorInfo(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "user.batch_quota", map[string]interface{}{
		"mode":          req.Mode,
		"amount":        req.Amount,
		"factor":        req.Factor,
		"target_count":  len(req.UserIds),
		"updated_count": result.UpdatedCount,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}
