package controller

// jzlh-agent 代理分销控制器：超管管理代理 + 代理自助查看名下用户与分润。
// 全部收敛在此文件 + /api/user/agent/* 子树，不改上游 controller/user.go，便于合并 upstream。

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// parseAgentPagination 从 query 解析分页参数，带默认值与上限。
func parseAgentPagination(c *gin.Context) (page int, pageSize int) {
	page, _ = strconv.Atoi(c.Query("p"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(c.Query("page_size"))
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return
}

// agentErrorI18nKeys 把 model 层（commission.go / withdrawal.go）里代理相关的哨兵
// 错误映射到 i18n key。这些错误从 model 直接返回、未被包装，因此可以用 map 直接按
// 错误值查找。
var agentErrorI18nKeys = map[error]string{
	model.ErrInsufficientCommission:     i18n.MsgAgentInsufficientCommission,
	model.ErrInvalidWithdrawalMethod:    i18n.MsgAgentInvalidWithdrawalMethod,
	model.ErrInvalidConvertAmount:       i18n.MsgAgentInvalidConvertAmount,
	model.ErrInvalidWithdrawalAmount:    i18n.MsgAgentInvalidWithdrawalAmount,
	model.ErrPayeeInfoRequired:          i18n.MsgAgentPayeeInfoRequired,
	model.ErrPayeeNameLength:            i18n.MsgAgentPayeeNameLength,
	model.ErrPayeeNameFormat:            i18n.MsgAgentPayeeNameFormat,
	model.ErrPayeeAccountAlipayInvalid:  i18n.MsgAgentPayeeAccountAlipayInvalid,
	model.ErrPayeeAccountWechatInvalid:  i18n.MsgAgentPayeeAccountWechatInvalid,
	model.ErrPayeeAccountBankInvalid:    i18n.MsgAgentPayeeAccountBankInvalid,
	model.ErrWithdrawalAlreadyProcessed: i18n.MsgAgentWithdrawalAlreadyProcessed,
	model.ErrWithdrawalBelowMinimum:     i18n.MsgAgentWithdrawalBelowMinimum,
	model.ErrTooManyPendingWithdrawals:  i18n.MsgAgentTooManyPendingWithdrawals,
	model.ErrWithdrawalNotClaimed:       i18n.MsgAgentWithdrawalNotClaimed,
	model.ErrWithdrawalClaimedByOther:   i18n.MsgAgentWithdrawalClaimedByOther,
	model.ErrPayoutReferenceRequired:    i18n.MsgAgentPayoutReferenceRequired,
	model.ErrInvalidReviewAction:        i18n.MsgAgentInvalidReviewAction,
	// 反欺诈/风控（jzlh-agent 蓝图F）
	model.ErrCommissionAssetsFrozen:    i18n.MsgAgentAssetsFrozen,
	model.ErrFraudAlertNotFound:        i18n.MsgAgentFraudAlertNotFound,
	model.ErrFraudAlertAlreadyResolved: i18n.MsgAgentFraudAlertAlreadyResolved,
	model.ErrInvalidFraudAction:        i18n.MsgAgentInvalidFraudAction,
	model.ErrRiskUserNotFound:          i18n.MsgAgentRiskUserNotFound,
	model.ErrRiskNoActionSelected:      i18n.MsgAgentRiskNoActionSelected,
	// ErrCannotReviewOwnWithdrawal / ErrQuotaOverflow 暂无独立 i18n key，
	// 走 apiErrorAgent 的 err.Error() 兜底文案。
}

// apiErrorAgent 按用户语言返回代理相关错误；未识别的错误（如数据库异常）走原始
// err.Error() 兜底。
func apiErrorAgent(c *gin.Context, err error) {
	if key, ok := agentErrorI18nKeys[err]; ok {
		common.ApiErrorI18n(c, key)
		return
	}
	common.ApiError(c, err)
}

// ---- 超管：代理管理 ----

type setAgentRequest struct {
	UserId          int     `json:"user_id"`
	AgentType       string  `json:"agent_type"`
	UsageProfitRate float64 `json:"usage_profit_rate"`
}

// CreateAgent 超管将指定用户设为代理并设定分润比例。
func CreateAgent(c *gin.Context) {
	var req setAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	if !model.AgentTypeValid(req.AgentType) {
		common.ApiErrorI18n(c, i18n.MsgAgentInvalidAgentType)
		return
	}
	if req.UsageProfitRate < 0 || req.UsageProfitRate > 1 {
		common.ApiErrorI18n(c, i18n.MsgAgentInvalidUsageProfitRate)
		return
	}
	target, err := model.GetUserById(req.UserId, false)
	if err != nil || target == nil {
		common.ApiErrorI18n(c, i18n.MsgAgentUserNotFound)
		return
	}
	// 代理是"高级客户"而非管理员：管理员及以上（含超管）不能设为代理，
	// 避免"既当裁判又当运动员"——管理员可自审提现/改费率，与平台管理权必须隔离。
	if target.Role >= common.RoleAdminUser {
		common.ApiErrorI18n(c, i18n.MsgAgentCannotSetAdminAsAgent)
		return
	}
	if err := model.SetUserAgent(req.UserId, req.AgentType, req.UsageProfitRate); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// RevokeAgent 超管撤销用户的代理身份（保留其已累计分润余额）。
func RevokeAgent(c *gin.Context) {
	var req struct {
		UserId int `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	if err := model.RevokeUserAgent(req.UserId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// ListAgents 超管分页列出所有代理（可按用户名/显示名搜索、按状态筛选）。
func ListAgents(c *gin.Context) {
	page, pageSize := parseAgentPagination(c)
	status, _ := strconv.Atoi(c.Query("status"))
	agents, total, err := model.ListAgents(c.Query("keyword"), status, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": agents, "total": total, "page": page, "page_size": pageSize})
}

// ---- 代理自助 ----

// AgentListUsers 代理查看名下用户（inviter_id = 当前代理）。
func AgentListUsers(c *gin.Context) {
	agentId := c.GetInt("id")
	page, pageSize := parseAgentPagination(c)
	status, _ := strconv.Atoi(c.Query("status"))
	users, total, err := model.GetAgentDownstreamUsers(agentId, c.Query("keyword"), status, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	totalQuota, totalUsedQuota, err := model.GetAgentDownstreamStats(agentId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items": users, "total": total, "page": page, "page_size": pageSize,
		"total_quota":      totalQuota,
		"total_used_quota": totalUsedQuota,
	})
}

// AgentListCommissions 代理查看分润流水与余额汇总。
func AgentListCommissions(c *gin.Context) {
	agentId := c.GetInt("id")
	model.MatureAgentCommissions(agentId) // 惰性结转成熟分润
	page, pageSize := parseAgentPagination(c)
	records, total, err := model.GetAgentCommissions(agentId, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	me, err := model.GetUserById(agentId, false)
	if err != nil || me == nil {
		common.ApiErrorI18n(c, i18n.MsgAgentUserNotFound)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items":                    records,
		"total":                    total,
		"page":                     page,
		"page_size":                pageSize,
		"commission_quota":         me.CommissionQuota,
		"commission_history_quota": me.CommissionHistoryQuota,
		"commission_pending_quota": model.GetAgentPendingCommission(agentId), // 成熟期内待结转
		"agent_type":               me.AgentType,
		"usage_profit_rate":        me.UsageProfitRate,
		// 提现配置随汇总下发，前端据此展示最低提现额/预估手续费并做前置校验。
		"withdraw_min_quota": common.AgentWithdrawMinQuota,
		"withdraw_fee_rate":  common.AgentWithdrawFeeRate,
	})
}

// ---- 代理自助：分润出口（转额度 / 提现） ----

// ConvertCommission 代理把分润余额转成自己可用的 API 额度。
func ConvertCommission(c *gin.Context) {
	agentId := c.GetInt("id")
	var req struct {
		Amount int `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	model.MatureAgentCommissions(agentId)
	if err := model.ConvertCommissionToQuota(agentId, req.Amount); err != nil {
		apiErrorAgent(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

type createWithdrawalRequest struct {
	Amount       int    `json:"amount"`
	Method       string `json:"method"`
	PayeeName    string `json:"payee_name"`
	PayeeAccount string `json:"payee_account"`
	Remark       string `json:"remark"`
}

// CreateWithdrawal 代理申请现金提现（预扣分润余额，待超管审批）。
func CreateWithdrawal(c *gin.Context) {
	// 现金提现与支付/邀请返利同属资金激励，合规声明未确认时一并禁用。
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}
	agentId := c.GetInt("id")
	var req createWithdrawalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	model.MatureAgentCommissions(agentId)
	w, err := model.CreateWithdrawal(
		agentId,
		req.Amount,
		req.Method,
		req.PayeeName,
		req.PayeeAccount,
		req.Remark,
	)
	if err != nil {
		apiErrorAgent(c, err)
		return
	}
	common.ApiSuccess(c, w)
}

// AgentListWithdrawals 代理查看自己的提现记录。
func AgentListWithdrawals(c *gin.Context) {
	agentId := c.GetInt("id")
	page, pageSize := parseAgentPagination(c)
	items, total, err := model.ListUserWithdrawals(agentId, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// ---- 超管：提现审批 ----

// AdminListWithdrawals 超管分页列出提现单（可按 status 筛选）。
func AdminListWithdrawals(c *gin.Context) {
	page, pageSize := parseAgentPagination(c)
	status, _ := strconv.Atoi(c.Query("status"))
	items, total, err := model.ListAllWithdrawals(status, c.Query("keyword"), (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// ReviewWithdrawal 超管处理提现单（claim 认领 / release 释放 / approve 标记已打款 / reject 拒绝）。
func ReviewWithdrawal(c *gin.Context) {
	var req struct {
		Id          int    `json:"id"`
		Action      string `json:"action"`
		AdminRemark string `json:"admin_remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	switch req.Action {
	case "claim", "release", "approve", "reject":
	default:
		common.ApiErrorI18n(c, i18n.MsgAgentInvalidReviewAction)
		return
	}
	if err := model.ReviewWithdrawal(req.Id, req.Action, c.GetInt("id"), req.AdminRemark); err != nil {
		apiErrorAgent(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// CancelAgentWithdrawal 代理撤销自己的待审核提现单。
func CancelAgentWithdrawal(c *gin.Context) {
	agentId := c.GetInt("id")
	var req struct {
		Id int `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	if err := model.CancelWithdrawal(agentId, req.Id); err != nil {
		apiErrorAgent(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
