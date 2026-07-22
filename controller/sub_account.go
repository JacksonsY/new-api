package controller

// jzlh-sub 子账号管理控制器（主号 = 所有者 Owner / 有 team_management 的管理员子号可代管普通子号）。
// 计费/额度/权限的模型层在 model/sub_account.go；本文件做鉴权、美元↔quota 换算、请求校验。

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// ResolveTopupUserId 解析充值/兑换的入账目标：子账号(parent_id>0)入账到主号钱包(共享池)，
// 主号/普通用户入账到自己。仅 wallet 管理员子号能走到这里（路由挂 SubPermission("wallet")）。
// 自行按 id 解析主号——不能读 user_parent_id 上下文键(authHelper 不写它，恒 0)。jzlh-sub。
func ResolveTopupUserId(c *gin.Context) int {
	if base, err := model.GetUserCache(c.GetInt("id")); err == nil && base != nil && base.ParentId != 0 {
		return base.ParentId
	}
	return c.GetInt("id")
}

// ---- 美元 ↔ 内部 quota 换算（前端按美元，后端存 quota；-1=无限）----

type subLimitInput struct {
	Unlimited bool    `json:"unlimited"`
	Value     float64 `json:"value"` // 美元
}

// toQuota 把额度输入换算成内部 quota：无限=-1；否则 USD*QuotaPerUnit（截断，饱和防溢出）。
// 负数必须先经 valid() 拒绝——静默转无限会把"想限额"反转成"全放开"。
func (l subLimitInput) toQuota() int {
	if l.Unlimited || l.Value == 0 {
		return -1 // 未填按无限
	}
	return common.QuotaFromFloat(l.Value * common.QuotaPerUnit)
}

// valid 报告额度输入是否合法：负数美元是笔误而非"无限"，直接拒绝。
func (l subLimitInput) valid() bool {
	return l.Unlimited || l.Value >= 0
}

func quotaToUSD(quota int) float64 {
	if quota < 0 || common.QuotaPerUnit == 0 {
		return -1
	}
	return float64(quota) / common.QuotaPerUnit
}

// ---- 鉴权：Owner 或 有 team_management 的管理员子号 ----

// loadSubMgmtActor 加载当前操作者并校验其有权管理子号（主号 Owner，或 team_management 管理员子号）。
func loadSubMgmtActor(c *gin.Context) (*model.User, error) {
	actor, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		return nil, err
	}
	if actor.ParentId == 0 { // 主号 Owner
		return actor, nil
	}
	if actor.IsSubAdmin() && actor.HasSubPermission(dto.SubPermTeamManagement) {
		return actor, nil
	}
	return nil, errors.New("no permission to manage sub-accounts")
}

// actorMainAccountId 返回操作者所属主号 id（Owner=自己，管理员子号=其 parent）。
func actorMainAccountId(actor *model.User) int {
	if actor.ParentId == 0 {
		return actor.Id
	}
	return actor.ParentId
}

// isOwner 报告操作者是否为主号 Owner（任命/管理管理员子号是 Owner 专属）。
func isOwner(actor *model.User) bool { return actor.ParentId == 0 }

// sanitizeSubPermissions 归一子号权限：普通预设剥离 wallet/team_management 高权限。
func sanitizeSubPermissions(preset string, in map[string]bool) map[string]bool {
	perms := map[string]bool{}
	if in != nil {
		for k, v := range in {
			perms[k] = v
		}
	} else {
		perms[dto.SubPermPlayground] = true
		perms[dto.SubPermApiKeys] = true
		perms[dto.SubPermUsageLogs] = true
	}
	if preset != dto.SubRolePresetAdmin {
		delete(perms, dto.SubPermWallet)
		delete(perms, dto.SubPermTeamManagement)
	}
	return perms
}

// ---- 视图 ----

type subAccountView struct {
	Id              int     `json:"id"`
	Email           string  `json:"email"`
	Username        string  `json:"username"` // display_name（对齐 302「用户名」）
	Note            string  `json:"note"`
	RolePreset      string  `json:"role_preset"`
	Status          int     `json:"status"`
	CreatedAt       int64   `json:"created_at"`
	InitialPassword string  `json:"initial_password,omitempty"`
	TotalUsedUSD    float64 `json:"total_used_usd"`
	MonthUsedUSD    float64 `json:"month_used_usd"`
	DayUsedUSD      float64 `json:"day_used_usd"`
	TotalLimitUSD   float64 `json:"total_limit_usd"` // -1=无限
	MonthLimitUSD   float64 `json:"month_limit_usd"`
	DayLimitUSD     float64 `json:"day_limit_usd"`

	Permissions map[string]bool `json:"permissions"`
}

func newSubAccountView(u *model.User) subAccountView {
	sa := u.GetSetting().SubAccount
	v := subAccountView{
		Id:            u.Id,
		Email:         u.Email,
		Username:      u.DisplayName,
		Status:        u.Status,
		CreatedAt:     u.CreatedAt,
		TotalUsedUSD:  quotaToUSD(u.UsedQuota),
		MonthUsedUSD:  quotaToUSD(u.MonthUsedQuota),
		DayUsedUSD:    quotaToUSD(u.DayUsedQuota),
		TotalLimitUSD: quotaToUSD(u.TotalQuotaLimit),
		MonthLimitUSD: quotaToUSD(u.MonthQuotaLimit),
		DayLimitUSD:   quotaToUSD(u.DayQuotaLimit),
	}
	if sa != nil {
		v.Note = sa.Note
		v.RolePreset = sa.RolePreset
		v.Permissions = sa.Permissions
		if model.SubAccountShowInitialPassword {
			v.InitialPassword = model.GetSubAccountInitialPassword(u)
		}
	}
	return v
}

// ---- 建号（批量）----

type createSubAccountsRequest struct {
	Prefix      string          `json:"prefix"`
	Count       int             `json:"count"`
	RolePreset  string          `json:"role_preset"`
	Permissions map[string]bool `json:"permissions"`
	Note        string          `json:"note"`
	TotalLimit  subLimitInput   `json:"total_limit"`
	MonthLimit  subLimitInput   `json:"month_limit"`
	DayLimit    subLimitInput   `json:"day_limit"`
}

func CreateSubAccounts(c *gin.Context) {
	if !model.SubAccountEnabled {
		common.ApiErrorMsg(c, "sub-account feature is disabled")
		return
	}
	actor, err := loadSubMgmtActor(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	var req createSubAccountsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Prefix == "" || req.Count <= 0 || req.Count > 100 {
		common.ApiErrorMsg(c, "invalid prefix or count (1-100)")
		return
	}
	if !req.TotalLimit.valid() || !req.MonthLimit.valid() || !req.DayLimit.valid() {
		common.ApiErrorMsg(c, "quota limit must not be negative")
		return
	}
	preset := req.RolePreset
	if preset != dto.SubRolePresetAdmin {
		preset = dto.SubRolePresetUser
	}
	// 任命管理员子号是 Owner 专属：管理员子号只能建普通子号。
	if preset == dto.SubRolePresetAdmin && !isOwner(actor) {
		common.ApiErrorMsg(c, "only the owner can create admin sub-accounts")
		return
	}
	mainId := actorMainAccountId(actor)
	// MaxSubAccounts 配额校验
	if model.MaxSubAccounts > 0 {
		_, existing, e := model.ListSubAccounts(mainId, "", 0, 0, 0, 1)
		if e == nil && int(existing)+req.Count > model.MaxSubAccounts {
			common.ApiErrorMsg(c, "exceeds max sub-accounts limit")
			return
		}
	}
	created, err := model.CreateSubAccounts(model.CreateSubAccountsParams{
		ParentId:    mainId,
		Prefix:      req.Prefix,
		Count:       req.Count,
		RolePreset:  preset,
		Permissions: sanitizeSubPermissions(preset, req.Permissions),
		Note:        req.Note,
		TotalLimit:  req.TotalLimit.toQuota(),
		MonthLimit:  req.MonthLimit.toQuota(),
		DayLimit:    req.DayLimit.toQuota(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]subAccountView, 0, len(created))
	for _, ct := range created {
		v := newSubAccountView(ct.User)
		v.InitialPassword = ct.InitialPassword // 建号时无条件返回明文供复制
		views = append(views, v)
	}
	common.ApiSuccess(c, gin.H{"items": views})
}

// ---- 列表 ----

func ListSubAccounts(c *gin.Context) {
	actor, err := loadSubMgmtActor(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	page, pageSize := parseAgentPagination(c)
	emailKeyword := c.Query("email")
	users, total, err := model.ListSubAccounts(actorMainAccountId(actor), emailKeyword, 0, 0, (page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]subAccountView, 0, len(users))
	for _, u := range users {
		v := newSubAccountView(u)
		// 初始密码=登录凭证。管理员子号不得管理另一管理员子号(loadManagedTarget 硬约束)，
		// 列表也不能把兄弟管理员的初始密码泄露给它——否则可直接拿凭证登录绕过该边界。
		// 自己那行保留；普通子号行保留(管理员本就可重置普通子号密码，不新增能力)。
		if !isOwner(actor) && u.Id != actor.Id && u.IsSubAdmin() {
			v.InitialPassword = ""
		}
		items = append(items, v)
	}
	// 概览汇总：名下子号总消耗 + 主号(付款池)余额 + 主号邮箱，供前端三卡展示。
	mainId := actorMainAccountId(actor)
	subUsed, _ := model.SubAccountsTotalUsed(mainId)
	mainBalance, _ := model.GetUserQuota(mainId, false)
	mainEmail := actor.Email
	if !isOwner(actor) {
		if owner, e := model.GetUserById(mainId, false); e == nil {
			mainEmail = owner.Email
		}
	}
	summary := gin.H{
		"sub_used_usd": quotaToUSD(int(subUsed)),
		"balance_usd":  quotaToUSD(mainBalance),
		"main_email":   mainEmail,
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize, "summary": summary})
}

// ---- 更新 ----

type updateSubAccountRequest struct {
	DisplayName *string          `json:"display_name"`
	Note        *string          `json:"note"`
	RolePreset  *string          `json:"role_preset"`
	Permissions *map[string]bool `json:"permissions"`
	NewPassword *string          `json:"new_password"`
	TotalLimit  *subLimitInput   `json:"total_limit"`
	MonthLimit  *subLimitInput   `json:"month_limit"`
	DayLimit    *subLimitInput   `json:"day_limit"`
}

func UpdateSubAccount(c *gin.Context) {
	actor, err := loadSubMgmtActor(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	target, err := loadManagedTarget(c, actor)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	var req updateSubAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	params := model.UpdateSubAccountParams{
		DisplayName: req.DisplayName,
		Note:        req.Note,
		NewPassword: req.NewPassword,
	}
	// 任命/改预设、改权限的 wallet/team_management 授予是 Owner 专属
	preset := ""
	if req.RolePreset != nil {
		preset = *req.RolePreset
		if preset == dto.SubRolePresetAdmin && !isOwner(actor) {
			common.ApiErrorMsg(c, "only the owner can promote to admin")
			return
		}
		params.RolePreset = req.RolePreset
	} else if sa := target.GetSetting().SubAccount; sa != nil && sa.RolePreset == dto.SubRolePresetAdmin {
		preset = dto.SubRolePresetAdmin
	} else {
		preset = dto.SubRolePresetUser
	}
	if req.Permissions != nil {
		sanitized := sanitizeSubPermissions(preset, *req.Permissions)
		// 非 Owner 不得授予 wallet/team_management
		if !isOwner(actor) {
			delete(sanitized, dto.SubPermWallet)
			delete(sanitized, dto.SubPermTeamManagement)
		}
		params.Permissions = &sanitized
	}
	for _, l := range []*subLimitInput{req.TotalLimit, req.MonthLimit, req.DayLimit} {
		if l != nil && !l.valid() {
			common.ApiErrorMsg(c, "quota limit must not be negative")
			return
		}
	}
	if req.TotalLimit != nil {
		q := req.TotalLimit.toQuota()
		params.TotalLimit = &q
	}
	if req.MonthLimit != nil {
		q := req.MonthLimit.toQuota()
		params.MonthLimit = &q
	}
	if req.DayLimit != nil {
		q := req.DayLimit.toQuota()
		params.DayLimit = &q
	}
	if err := model.UpdateSubAccount(target, params); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// ---- 停用 / 删除 ----

func DisableSubAccount(c *gin.Context) {
	actor, err := loadSubMgmtActor(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	target, err := loadManagedTarget(c, actor)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := model.DisableSubAccount(target.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func DeleteSubAccount(c *gin.Context) {
	actor, err := loadSubMgmtActor(c)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	target, err := loadManagedTarget(c, actor)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := model.DeleteSubAccount(target.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// loadManagedTarget 按路径 :id 加载被管理子号，并校验归属 + 管理员子号不得操作另一管理员子号。
func loadManagedTarget(c *gin.Context, actor *model.User) (*model.User, error) {
	targetId := common.String2Int(c.Param("id"))
	if targetId <= 0 {
		return nil, errors.New("invalid sub-account id")
	}
	target, err := model.GetSubAccountForParent(targetId, actorMainAccountId(actor))
	if err != nil {
		return nil, err
	}
	// 管理员子号只能操作普通子号；任命/管理管理员子号是 Owner 专属。
	if !isOwner(actor) && target.IsSubAdmin() {
		return nil, errors.New("only the owner can manage admin sub-accounts")
	}
	return target, nil
}
