package controller

// jzlh-supplier 供应商市场控制器：供应商自助（入驻/渠道提交/收益）+ 管理端（审批/审核/结算打款）。
// 收敛在本文件 + /api/supplier/* 子树，不改上游 controller/channel.go/user.go，便于合并 upstream。

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ---- 供应商自助 ----

// SupplierApply 用户申请成为供应商。
type supplierPayoutInfoRequest struct {
	Method  string `json:"method"`  // alipay/wechat/bank/usdt/other，非法值由 model 归一为 other
	Account string `json:"account"` // 收款账号
	Name    string `json:"name"`    // 户名/真实姓名
	Contact string `json:"contact"` // 联系方式(微信/QQ/Telegram/邮箱)
}

// validateSupplierPayoutInfo 校验收款/联系方式：三项必填 + 按列宽限长（rune 计，防 DB 截断）。
// 入驻与后续编辑复用；method 无需校验（model 会把非法值归一为 other）。
func validateSupplierPayoutInfo(req supplierPayoutInfoRequest) error {
	account := strings.TrimSpace(req.Account)
	name := strings.TrimSpace(req.Name)
	contact := strings.TrimSpace(req.Contact)
	if account == "" || name == "" || contact == "" {
		return errors.New("payout account, name and contact are required")
	}
	if len([]rune(account)) > 128 || len([]rune(name)) > 64 || len([]rune(contact)) > 128 {
		return errors.New("payout field too long")
	}
	return nil
}

// supplierProfileRequest 商户资料：入驻申请时填写，审核员据此沟通/展示（与收款账户解耦）。
type supplierProfileRequest struct {
	Name    string `json:"name"`    // 商户名称/品牌名
	Contact string `json:"contact"` // 联系方式(微信/Telegram/邮箱)
	Intro   string `json:"intro"`   // 商户简介
}

// validateSupplierProfile 校验商户资料：名称+联系方式必填，按列宽限长（rune 计，防 DB 截断）。
func validateSupplierProfile(req supplierProfileRequest) error {
	name := strings.TrimSpace(req.Name)
	contact := strings.TrimSpace(req.Contact)
	if name == "" || contact == "" {
		return errors.New("merchant name and contact are required")
	}
	if len([]rune(name)) > 64 || len([]rune(contact)) > 128 || len([]rune(req.Intro)) > 255 {
		return errors.New("merchant profile field too long")
	}
	return nil
}

// supplierApplyRequest 入驻申请：一次提交 = 商户资料 + 一条渠道要约（模型/url/key）。
type supplierApplyRequest struct {
	Profile supplierProfileRequest `json:"profile"`
	Channel supplierChannelRequest `json:"channel"`
}

// SupplierApply 供应商入驻申请 = 商户资料 + 一条渠道要约。首次 None→Pending 并保存商户资料；
// 已 Pending/Approved 再调则追加一条渠道要约（多次申请）；Suspended 拒；管理员拒（裁判/运动员）。
// 渠道进入待审，管理员审核时可跑检测测真伪、并定分组/报价率。收款账户走审核通过后的收款设置。
func SupplierApply(c *gin.Context) {
	// v2 P2 模块开关:关闭时停止接收新入驻(存量供应商渠道/结算不受影响)
	if !common.SupplierEnabled {
		common.ApiErrorMsg(c, "供应商入驻当前未开放")
		return
	}
	var req supplierApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateSupplierProfile(req.Profile); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := validateSupplierChannel(req.Channel); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	userId := c.GetInt("id")
	// 门控（Suspended 拒；管理员拒；None→Pending）——先于任何写入。
	if err := model.EnsureSupplierApplied(userId); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateSupplierProfile(userId, req.Profile.Name, req.Profile.Contact, req.Profile.Intro); err != nil {
		common.ApiError(c, err)
		return
	}
	channelId, err := createSupplierChannel(userId, req.Channel)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"channel_id": channelId})
}

// SupplierUpdatePayoutInfo 审核通过后更新收款/联系方式（换卡、改联系方式等），不改审核状态。
func SupplierUpdatePayoutInfo(c *gin.Context) {
	var req supplierPayoutInfoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateSupplierPayoutInfo(req); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := model.UpdateSupplierPayoutInfo(c.GetInt("id"), req.Method, req.Account, req.Name, req.Contact); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// SupplierProfile 返回当前用户的供应商状态 + 结算概览（未通过审核也可查状态）。
func SupplierProfile(c *gin.Context) {
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil || user == nil {
		common.ApiError(c, err)
		return
	}
	resp := gin.H{
		"supplier_status":         user.SupplierStatus,
		"supplier_name":           user.SupplierName,
		"supplier_intro":          user.SupplierIntro,
		"supplier_contact":        user.SupplierContact,
		"supplier_payout_method":  user.SupplierPayoutMethod,
		"supplier_payout_account": user.SupplierPayoutAccount,
		"supplier_payout_name":    user.SupplierPayoutName,
	}
	if user.SupplierStatus == model.SupplierStatusApproved {
		if settlement, e := model.GetSupplierSettlement(userId); e == nil {
			resp["settlement"] = settlement
		}
	}
	common.ApiSuccess(c, resp)
}

// SupplierListChannels 列出当前供应商名下渠道（owner scope）。
func SupplierListChannels(c *gin.Context) {
	supplierId := c.GetInt("id")
	page, pageSize := parseAgentPagination(c)
	channels, total, err := model.GetSupplierChannels(supplierId, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 供应商侧不回显完整 Key（掩码），沿用列表默认（Key 字段列表查询本就不 Select 明文）。
	common.ApiSuccess(c, gin.H{"items": channels, "total": total, "page": page, "page_size": pageSize})
}

type supplierChannelRequest struct {
	Name         string  `json:"name"`
	Type         int     `json:"type"`
	Key          string  `json:"key"`
	BaseURL      string  `json:"base_url"`
	Models       string  `json:"models"`
	Note         string  `json:"note"`          // 渠道说明（存入 Remark，审核员可见；驳回时被审核原因覆盖）
	ChannelRatio float64 `json:"channel_ratio"` // 报价率（申请页不收，管理员审批时定；<=0 建库时占位为上限）
	TestModel    string  `json:"test_model"`
}

// validateSupplierChannel 校验渠道要约字段：名称 1-10 字、无逗号/引号/首尾空格（逗号会破坏
// models 的逗号分隔，引号防注入类问题）；key/models 必填；报价率封顶（申请页不传即 0，放行）。
func validateSupplierChannel(req supplierChannelRequest) error {
	if req.Name != strings.TrimSpace(req.Name) {
		return errors.New("channel name has leading/trailing spaces")
	}
	if n := len([]rune(req.Name)); n < 1 || n > 10 {
		return errors.New("channel name must be 1-10 characters")
	}
	if strings.ContainsAny(req.Name, ",\"'，“”‘’") {
		return errors.New("channel name cannot contain commas or quotes")
	}
	if req.Key == "" || req.Models == "" {
		return errors.New("key / models required")
	}
	if req.ChannelRatio < 0 || req.ChannelRatio > common.SupplierMaxRate {
		return errors.New("channel_ratio out of range")
	}
	return nil
}

// createSupplierChannel 建一条待审的供应商渠道（owner scope）。Insert() 调 AddAbilities，
// 但 audit=pending 被审核门拦截，不进路由池。返回新渠道 id。
func createSupplierChannel(supplierId int, req supplierChannelRequest) (int, error) {
	baseURL := req.BaseURL
	ratio := req.ChannelRatio
	if ratio <= 0 {
		// 申请页不收报价率，占位为上限（成本=官方价、平台毛利 0），管理员审批时再定。
		ratio = common.SupplierMaxRate
	}
	testModel := strings.TrimSpace(req.TestModel)
	if testModel == "" {
		// 未指定测试模型时取模型列表首个，方便管理员一键测活。
		testModel = strings.TrimSpace(strings.Split(req.Models, ",")[0])
	}
	channel := &model.Channel{
		UserId:       supplierId,
		AuditStatus:  model.ChannelAuditPending,
		Status:       common.ChannelStatusEnabled,
		Type:         req.Type,
		Name:         req.Name,
		Key:          req.Key,
		BaseURL:      &baseURL,
		Models:       req.Models,
		Group:        "default", // 占位，管理员审批时定实际分组
		ChannelRatio: &ratio,
		TestModel:    &testModel,
	}
	// 渠道说明存 Remark（审核员可见）；驳回时 AdminRejectChannel 会用审核原因覆盖它。
	if note := strings.TrimSpace(req.Note); note != "" {
		channel.Remark = &note
	}
	if err := channel.Insert(); err != nil {
		return 0, err
	}
	return channel.Id, nil
}

// SupplierSubmitChannel 已通过审核的供应商追加新渠道（进入待审，不进路由池）。
func SupplierSubmitChannel(c *gin.Context) {
	supplierId := c.GetInt("id")
	var req supplierChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateSupplierChannel(req); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	channelId, err := createSupplierChannel(supplierId, req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": channelId})
}

// SupplierUpdateChannel 供应商编辑名下渠道；改动关键字段（Key/BaseURL/Models）触发复审。
func SupplierUpdateChannel(c *gin.Context) {
	supplierId := c.GetInt("id")
	channelId, _ := strconv.Atoi(c.Param("id"))
	var req supplierChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ChannelRatio < 0 || req.ChannelRatio > common.SupplierMaxRate {
		common.ApiErrorMsg(c, "channel_ratio out of range")
		return
	}
	channel, err := model.GetSupplierChannelById(channelId, supplierId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	needReReview := model.MaterialChannelFieldsChanged(channel, req.Key, req.BaseURL, req.Models)
	// 部分更新语义：空字段表示本次不提交，沿用旧值（与 MaterialChannelFieldsChanged 一致）。
	// 否则只改名的请求会把 base_url/test_model 抹空、ratio 归零，并误触发复审。
	if req.Name != "" {
		channel.Name = req.Name
	}
	if req.Key != "" {
		channel.Key = req.Key
	}
	if req.BaseURL != "" {
		baseURL := req.BaseURL
		channel.BaseURL = &baseURL
	}
	if req.Models != "" {
		channel.Models = req.Models
	}
	ratePending := false
	clearPendingRate := false
	if req.ChannelRatio > 0 {
		ratio := req.ChannelRatio
		// v2 §4.2 价格变更申请流:已上线渠道涨价不再直接生效——那是"平台毛利被
		// 单方面清零"的资损口。新价写入 pending,渠道继续按现价在线运行,管理员
		// 批准后原子切换;降价对平台单向有利,即时生效并撤销在途涨价申请。
		// 未上线渠道(待审/驳回)改价自由,审批时管理员定夺。
		if channel.AuditStatus == model.ChannelAuditApproved && ratio > channel.GetChannelRatio() {
			channel.PendingChannelRatio = &ratio
			ratePending = true
		} else {
			channel.ChannelRatio = &ratio
			clearPendingRate = channel.PendingChannelRatio != nil
		}
	}
	if req.TestModel != "" {
		testModel := req.TestModel
		channel.TestModel = &testModel
	}
	// 关键字段变化 → 已通过的渠道打回待审，Update() 经审核门删 abilities 出池。
	if needReReview && channel.AuditStatus == model.ChannelAuditApproved {
		channel.AuditStatus = model.ChannelAuditPending
	}
	if err := channel.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	if clearPendingRate {
		// Updates(struct) 忽略 nil 指针,撤销在途申请须显式清列。
		if err := model.RejectChannelRateChange(channel.Id); err != nil {
			common.SysLog("failed to clear pending channel ratio: " + err.Error())
		}
	}
	if ratePending {
		model.RecordLog(supplierId, model.LogTypeManage,
			fmt.Sprintf("渠道 %d 提交报价率变更申请:%.4f -> %.4f(现价继续生效,待平台确认)",
				channel.Id, channel.GetChannelRatio(), req.ChannelRatio))
	}
	common.ApiSuccess(c, gin.H{
		"id":           channel.Id,
		"audit_status": channel.AuditStatus,
		"rate_pending": ratePending,
	})
}

// SupplierEarningsDaily 按渠道×按天收益明细(v2 §4.3 经营透明:供应商不再只能
// 相信平台报的汇总数)。口径与结算一致。
func SupplierEarningsDaily(c *gin.Context) {
	supplierId := c.GetInt("id")
	days, _ := strconv.Atoi(c.Query("days"))
	rows, err := model.GetSupplierDailyEarnings(supplierId, days)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": rows})
}

// AdminListRateChanges 管理端:在途报价率变更申请列表。
func AdminListRateChanges(c *gin.Context) {
	page, pageSize := parseAgentPagination(c)
	channels, total, err := model.ListPendingRateChangeChannels((page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items": channels, "total": total, "page": page, "page_size": pageSize,
	})
}

// AdminReviewRateChange 管理端:批准/拒绝报价率变更。批准=切价+清 pending,
// 渠道全程在线(不动 AuditStatus);拒绝=仅清 pending。
func AdminReviewRateChange(c *gin.Context) {
	var req struct {
		ChannelId int    `json:"channel_id"`
		Action    string `json:"action"` // approve | reject
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ChannelId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	switch req.Action {
	case "approve":
		newRate, err := model.ApproveChannelRateChange(req.ChannelId)
		if err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		model.RecordLog(c.GetInt("id"), model.LogTypeManage,
			fmt.Sprintf("批准渠道 %d 报价率变更,新价 %.4f 生效", req.ChannelId, newRate))
	case "reject":
		if err := model.RejectChannelRateChange(req.ChannelId); err != nil {
			common.ApiError(c, err)
			return
		}
		model.RecordLog(c.GetInt("id"), model.LogTypeManage,
			fmt.Sprintf("拒绝渠道 %d 的报价率变更申请", req.ChannelId))
	default:
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	common.ApiSuccess(c, nil)
}

// SupplierEarnings 当前供应商收益 + 打款/没收台账。
func SupplierEarnings(c *gin.Context) {
	supplierId := c.GetInt("id")
	settlement, err := model.GetSupplierSettlement(supplierId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page, pageSize := parseAgentPagination(c)
	ledger, total, err := model.ListSupplierLedger(supplierId, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"settlement": settlement,
		"ledger":     ledger,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
	})
}

// ---- 管理端 ----

// AdminListSuppliers 列出供应商（?status= 过滤，缺省列全部非 None）。
func AdminListSuppliers(c *gin.Context) {
	status := -1
	if s := c.Query("status"); s != "" {
		status, _ = strconv.Atoi(s)
	}
	page, pageSize := parseAgentPagination(c)
	suppliers, total, err := model.ListSuppliers(status, c.Query("keyword"), (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": suppliers, "total": total, "page": page, "page_size": pageSize})
}

type reviewSupplierRequest struct {
	UserId int `json:"user_id"`
	Status int `json:"status"` // 见 model.SupplierStatus*
}

// AdminReviewSupplier 审批/停用供应商。
func AdminReviewSupplier(c *gin.Context) {
	var req reviewSupplierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorMsg(c, "invalid user_id")
		return
	}
	if req.Status < model.SupplierStatusNone || req.Status > model.SupplierStatusSuspended {
		common.ApiErrorMsg(c, "invalid status")
		return
	}
	target, err := model.GetUserById(req.UserId, false)
	if err != nil || target == nil {
		common.ApiError(c, err)
		return
	}
	// 管理员及以上不能作为供应商（裁判/运动员）。对管理员账号的任何审核一律归零（移出供应商体系）：
	// 既堵住"设为 Approved"，也让「恢复」按钮把被暂停的管理员解开到 None，而非死锁在 Suspended。
	if target.Role >= common.RoleAdminUser {
		if err := model.SetSupplierStatus(req.UserId, model.SupplierStatusNone); err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, nil)
		return
	}
	if err := model.SetSupplierStatus(req.UserId, req.Status); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminListPendingChannels 列出待审的供应商渠道。
func AdminListPendingChannels(c *gin.Context) {
	page, pageSize := parseAgentPagination(c)
	channels, total, err := model.ListPendingSupplierChannels((page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": channels, "total": total, "page": page, "page_size": pageSize})
}

type approveChannelRequest struct {
	ChannelId    int     `json:"channel_id"`
	Group        string  `json:"group"`
	Priority     int64   `json:"priority"`
	Weight       uint    `json:"weight"`
	ChannelRatio float64 `json:"channel_ratio"`
}

// AdminApproveSupplierChannel 审批通过：定分组/优先级/权重、封顶报价率，进池。
func AdminApproveSupplierChannel(c *gin.Context) {
	var req approveChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ChannelId <= 0 {
		common.ApiErrorMsg(c, "invalid channel_id")
		return
	}
	if err := model.AdminApproveChannel(req.ChannelId, req.Group, req.Priority, req.Weight, req.ChannelRatio); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

type rejectChannelRequest struct {
	ChannelId int    `json:"channel_id"`
	Remark    string `json:"remark"`
}

// AdminRejectSupplierChannel 驳回供应商渠道。
func AdminRejectSupplierChannel(c *gin.Context) {
	var req rejectChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ChannelId <= 0 {
		common.ApiErrorMsg(c, "invalid channel_id")
		return
	}
	if err := model.AdminRejectChannel(req.ChannelId, req.Remark); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminSupplierSettlement 返回单个供应商的结算详情 + 台账（?user_id=）。
func AdminSupplierSettlement(c *gin.Context) {
	supplierId, _ := strconv.Atoi(c.Query("user_id"))
	if supplierId <= 0 {
		common.ApiErrorMsg(c, "invalid user_id")
		return
	}
	settlement, err := model.GetSupplierSettlement(supplierId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page, pageSize := parseAgentPagination(c)
	ledger, total, err := model.ListSupplierLedger(supplierId, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"settlement": settlement, "ledger": ledger, "total": total, "page": page, "page_size": pageSize})
}

type paySupplierRequest struct {
	UserId  int    `json:"user_id"`
	Amount  int64  `json:"amount"`
	Voucher string `json:"voucher"`
	Remark  string `json:"remark"`
}

// AdminPaySupplier 记一条人工打款台账（管理员线下转账后回标）。
func AdminPaySupplier(c *gin.Context) {
	var req paySupplierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorMsg(c, "invalid user_id")
		return
	}
	if err := model.RecordSupplierPayout(req.UserId, req.Amount, req.Voucher, req.Remark, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminConfiscateSupplier 风控没收供应商未打款收益。
func AdminConfiscateSupplier(c *gin.Context) {
	var req paySupplierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorMsg(c, "invalid user_id")
		return
	}
	if err := model.ConfiscateSupplier(req.UserId, req.Amount, req.Remark, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
