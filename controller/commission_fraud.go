package controller

// jzlh-agent 分销反欺诈超管端点：IP 重合告警的扫描/列表/处置 + 风控管制。
// 与 controller/agent.go 同一子树(/api/user/agent/*)、同一错误映射与分页约定。

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
)

// AdminListFraudAlerts 超管分页列出 IP 重合告警（可按 status/keyword 筛选）。
func AdminListFraudAlerts(c *gin.Context) {
	page, pageSize := parseAgentPagination(c)
	alerts, total, err := model.SearchCommissionFraudAlerts(
		c.Query("status"), c.Query("keyword"), (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": alerts, "total": total, "page": page, "page_size": pageSize})
}

type scanFraudRequest struct {
	Days int  `json:"days"`
	Deep bool `json:"deep"`
}

// AdminScanFraud 超管触发全量 IP 重合扫描。deep=true 额外扫 logs 大表（建议低峰）。
func AdminScanFraud(c *gin.Context) {
	var req scanFraudRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Days <= 0 {
		req.Days = 30
	}
	if req.Days > 365 {
		req.Days = 365
	}
	newAlerts, err := model.DetectCommissionFraud(req.Days, req.Deep)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"new_alerts": newAlerts})
}

// AdminReviewFraudAlert 超管处置告警（unbind 解绑 / clawback 解绑并追回 /
// dismiss 误报放行 / delete 删除记录）。与提现审批同一"单端点 + action"风格。
func AdminReviewFraudAlert(c *gin.Context) {
	var req struct {
		Id     int    `json:"id"`
		Action string `json:"action"`
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	if err := model.ReviewCommissionFraudAlert(req.Id, req.Action, c.GetInt("id"), req.Remark); err != nil {
		apiErrorAgent(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminListRiskUsers 超管分页列出风控管制用户（默认仅 active，status=removed/all 可选）。
func AdminListRiskUsers(c *gin.Context) {
	page, pageSize := parseAgentPagination(c)
	items, total, err := model.ListCommissionRiskUsers(
		c.Query("keyword"), c.Query("status"), (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// AdminApplyRiskControls 超管施加风控管制（freeze_assets 冻结分润资产 /
// block_invite_code 封邀请码，可叠加）。freeze 生效时自动拒绝待审核提现单。
func AdminApplyRiskControls(c *gin.Context) {
	var req struct {
		UserId          int    `json:"user_id"`
		FreezeAssets    bool   `json:"freeze_assets"`
		BlockInviteCode bool   `json:"block_invite_code"`
		Reason          string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	rejected, err := model.ApplyCommissionRiskControls(
		req.UserId, c.GetInt("id"), req.FreezeAssets, req.BlockInviteCode, req.Reason)
	if err != nil {
		apiErrorAgent(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"rejected_withdrawals": rejected})
}

// AdminRemoveRiskControls 超管解除风控管制（flags 清零，pending 分润恢复正常结转）。
func AdminRemoveRiskControls(c *gin.Context) {
	var req struct {
		UserId int    `json:"user_id"`
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	if err := model.RemoveCommissionRiskControls(req.UserId, c.GetInt("id"), req.Remark); err != nil {
		apiErrorAgent(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
