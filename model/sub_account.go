package model

// jzlh-sub 子账号模型层：主号直建从属可登录子号，共享主号钱包（共享池），三档周期额度。
// 计费内核与订阅/钱包同源（service 层复用 WalletFunding，付款人换主号）；本文件只管
// 子号的 CRUD、邮箱/初始密码生成、功能权限白名单，以及三档额度的惰性周期重置与增减。
// 计费接线（付款人解析、四结算出口、分润排除）在 service/M2；控制器/权限中间件在 M3。

import (
	cryptorand "crypto/rand"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"gorm.io/gorm"
)

// 子账号站点配置（可由 OptionMap 覆盖；默认值见下）。
var (
	SubAccountEmailDomain         = "sub.local" // 生成邮箱的域（纯登录标识，不真实收件）
	SubAccountEnabled             = false       // 站点总开关：关则拦新建子号（存量子号照常计费）。已接 OptionMap，管理员在设置页开启
	MaxSubAccounts                = 0           // 每主号最大子号数，0=不限
	SubAccountShowInitialPassword = true        // 是否允许主号查看初始密码明文
)

var (
	// 三档额度触顶哨兵错误（M2 计费预扣与控制器据此给独立中文文案）。
	ErrSubTotalQuotaExceeded = errors.New("sub account total quota exceeded")
	ErrSubMonthQuotaExceeded = errors.New("sub account monthly quota exceeded")
	ErrSubDayQuotaExceeded   = errors.New("sub account daily quota exceeded")
	ErrNotSubAccountOwner    = errors.New("target is not your sub-account")
	ErrSubAccountNested      = errors.New("sub-account cannot own sub-accounts")
)

// ---- 身份/权限 helper ----

// IsSubAccount 报告该用户是否为子账号。
func (user *User) IsSubAccount() bool { return user.ParentId != 0 }

// subAccountSetting 取子号权限元信息（nil=非子号或未配置）。
func (user *User) subAccountSetting() *dto.SubAccountSetting {
	return user.GetSetting().SubAccount
}

// HasSubPermission 报告子号是否被授予某功能权限。非子号恒 true（主号不受白名单限制）。
func (user *User) HasSubPermission(perm string) bool {
	if user.ParentId == 0 {
		return true
	}
	sa := user.subAccountSetting()
	if sa == nil {
		return false
	}
	return sa.Permissions[perm]
}

// IsSubAdmin 报告子号是否为「管理员」预设（可被授予 wallet/team_management）。
func (user *User) IsSubAdmin() bool {
	sa := user.subAccountSetting()
	return sa != nil && sa.RolePreset == dto.SubRolePresetAdmin
}

// ---- 邮箱 / 用户名 / 初始密码生成 ----

// randomDigits 返回 n 位随机数字串（crypto/rand）。
func randomDigits(n int) string {
	const digits = "0123456789"
	b := make([]byte, n)
	_, _ = cryptorand.Read(b)
	for i := range b {
		b[i] = digits[int(b[i])%10]
	}
	return string(b)
}

// generateSubEmail 生成唯一子号邮箱 <prefix><10位随机数字>@<域>；冲突重试。
func generateSubEmail(prefix string) (string, error) {
	for i := 0; i < 8; i++ {
		email := NormalizeEmail(prefix + randomDigits(10) + "@" + SubAccountEmailDomain)
		var count int64
		if err := DB.Model(&User{}).Where("email = ?", email).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return email, nil
		}
	}
	return "", errors.New("failed to generate unique sub-account email")
}

// generateSubUsername 生成唯一内部用户名（登录用邮箱，username 仅满足唯一/非空约束）。
func generateSubUsername() (string, error) {
	for i := 0; i < 8; i++ {
		username := "sub_" + randomDigits(12)
		var count int64
		if err := DB.Model(&User{}).Where("username = ?", username).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return username, nil
		}
	}
	return "", errors.New("failed to generate unique sub-account username")
}

// ---- 建号（批量）----

type CreateSubAccountsParams struct {
	ParentId    int
	Prefix      string          // 邮箱前缀 + 显示名（对齐 302「用户名」）
	Count       int             // 批量数量
	RolePreset  string          // dto.SubRolePreset*
	Permissions map[string]bool // 功能白名单
	Note        string          // 备注
	TotalLimit  int             // 内部 quota，-1=无限
	MonthLimit  int
	DayLimit    int
}

// SubAccountCreated 建号结果：含明文初始密码（一次性返回给主号展示，密文已落库）。
type SubAccountCreated struct {
	User            *User
	InitialPassword string
}

// CreateSubAccounts 主号批量创建子号；返回含明文初始密码的结果，供主号复制。
// 校验单层从属（parent 必须非子号）；子号 Quota=0（无独立钱包，走主号池）。
func CreateSubAccounts(p CreateSubAccountsParams) ([]SubAccountCreated, error) {
	if p.Count <= 0 {
		return nil, errors.New("count must be positive")
	}
	parent, err := GetUserById(p.ParentId, false)
	if err != nil {
		return nil, err
	}
	if parent.ParentId != 0 {
		return nil, ErrSubAccountNested // 单层：子号不能建子号
	}
	created := make([]SubAccountCreated, 0, p.Count)
	for i := 0; i < p.Count; i++ {
		email, err := generateSubEmail(p.Prefix)
		if err != nil {
			return created, err
		}
		username, err := generateSubUsername()
		if err != nil {
			return created, err
		}
		plain := common.GetRandomString(10)
		enc, err := common.AesEncrypt(plain)
		if err != nil {
			return created, err
		}
		u := &User{
			Username:        username,
			DisplayName:     p.Prefix,
			Email:           email,
			Password:        plain, // createSubAccountRow 内哈希
			Role:            common.RoleCommonUser,
			Status:          common.UserStatusEnabled,
			Group:           parent.Group, // K6 继承主号分组
			Quota:           0,            // 无独立钱包
			ParentId:        p.ParentId,
			TotalQuotaLimit: p.TotalLimit,
			MonthQuotaLimit: p.MonthLimit,
			DayQuotaLimit:   p.DayLimit,
		}
		setting := dto.UserSetting{SubAccount: &dto.SubAccountSetting{
			Note:               p.Note,
			RolePreset:         p.RolePreset,
			Permissions:        p.Permissions,
			InitialPasswordEnc: enc,
		}}
		u.SetSetting(setting)
		if err := createSubAccountRow(u); err != nil {
			return created, err
		}
		created = append(created, SubAccountCreated{User: u, InitialPassword: plain})
	}
	return created, nil
}

// createSubAccountRow 落一个子号行：哈希密码 + 邮箱唯一锁 + aff code；不赠新用户额度、不做邀请逻辑。
func createSubAccountRow(user *User) error {
	if err := user.PrepareAffCode(); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
			if err := user.prepareForInsert(tx); err != nil { // 哈希密码 + 邮箱可用性
				return err
			}
			return tx.Create(user).Error
		})
	})
}

// ---- 查询 ----

// ListSubAccounts 列出主号名下子号（emailKeyword/时间范围过滤，分页）。
func ListSubAccounts(parentId int, emailKeyword string, startAt, endAt int64, offset, limit int) ([]*User, int64, error) {
	query := DB.Model(&User{}).Where("parent_id = ?", parentId)
	if emailKeyword != "" {
		query = query.Where("email LIKE ?", "%"+emailKeyword+"%")
	}
	if startAt > 0 {
		query = query.Where("created_at >= ?", startAt)
	}
	if endAt > 0 {
		query = query.Where("created_at <= ?", endAt)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []*User
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&users).Error
	return users, total, err
}

// SubAccountsTotalUsed 汇总某主号名下所有子号的累计消耗（used_quota 求和，红黑榜/概览用）。
func SubAccountsTotalUsed(parentId int) (int64, error) {
	var sum int64
	err := DB.Model(&User{}).Where("parent_id = ?", parentId).
		Select("COALESCE(SUM(used_quota), 0)").Scan(&sum).Error
	return sum, err
}

// GetSubAccountForParent 取子号并校验其 parent_id==parentId（越权防护）。
func GetSubAccountForParent(subUserId, parentId int) (*User, error) {
	user, err := GetUserById(subUserId, false)
	if err != nil {
		return nil, err
	}
	if user.ParentId != parentId {
		return nil, ErrNotSubAccountOwner
	}
	return user, nil
}

// ---- 更新 ----

type UpdateSubAccountParams struct {
	DisplayName *string
	Note        *string
	RolePreset  *string
	Permissions *map[string]bool
	TotalLimit  *int
	MonthLimit  *int
	DayLimit    *int
	NewPassword *string // 非空则改登录密码（不改初始密码密文副本）
}

// UpdateSubAccount 更新子号字段（调用方已做归属/权限校验）。
func UpdateSubAccount(user *User, p UpdateSubAccountParams) error {
	updates := map[string]interface{}{}
	if p.DisplayName != nil {
		updates["display_name"] = *p.DisplayName
	}
	if p.TotalLimit != nil {
		updates["total_quota_limit"] = *p.TotalLimit
	}
	if p.MonthLimit != nil {
		updates["month_quota_limit"] = *p.MonthLimit
	}
	if p.DayLimit != nil {
		updates["day_quota_limit"] = *p.DayLimit
	}
	if p.NewPassword != nil && *p.NewPassword != "" {
		hashed, err := common.Password2Hash(*p.NewPassword)
		if err != nil {
			return err
		}
		updates["password"] = hashed
	}
	// setting JSON 里的备注/预设/权限
	if p.Note != nil || p.RolePreset != nil || p.Permissions != nil {
		setting := user.GetSetting()
		if setting.SubAccount == nil {
			setting.SubAccount = &dto.SubAccountSetting{}
		}
		if p.Note != nil {
			setting.SubAccount.Note = *p.Note
		}
		if p.RolePreset != nil {
			setting.SubAccount.RolePreset = *p.RolePreset
		}
		if p.Permissions != nil {
			setting.SubAccount.Permissions = *p.Permissions
		}
		user.SetSetting(setting)
		updates["setting"] = user.Setting
	}
	if len(updates) == 0 {
		return nil
	}
	if err := DB.Model(&User{}).Where("id = ?", user.Id).Updates(updates).Error; err != nil {
		return err
	}
	return InvalidateUserCache(user.Id)
}

// GetSubAccountInitialPassword 解密子号初始密码明文（供主号查看/复制）。
func GetSubAccountInitialPassword(user *User) string {
	sa := user.subAccountSetting()
	if sa == nil || sa.InitialPasswordEnc == "" {
		return ""
	}
	plain, err := common.AesDecrypt(sa.InitialPasswordEnc)
	if err != nil {
		return ""
	}
	return plain
}

// ---- 停用 / 删除 ----

// DisableSubAccount 停用子号：禁登录 + 禁其全部令牌 + 失效缓存。
func DisableSubAccount(subUserId int) error {
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", subUserId).Update("status", common.UserStatusDisabled).Error; err != nil {
			return err
		}
		return disableSubAccountTokens(tx, subUserId)
	}); err != nil {
		return err
	}
	return InvalidateUserCache(subUserId)
}

// DeleteSubAccount 软删子号：GORM 软删 user + 禁其全部令牌 + 失效缓存。
// 软删保留行承接在途结算/退款落账（快照回主号钱包）。
func DeleteSubAccount(subUserId int) error {
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := disableSubAccountTokens(tx, subUserId); err != nil {
			return err
		}
		return tx.Delete(&User{}, subUserId).Error
	}); err != nil {
		return err
	}
	return InvalidateUserCache(subUserId)
}

// disableSubAccountTokens 禁用子号全部令牌并失效其令牌缓存（防 TTL 内继续烧池）。
func disableSubAccountTokens(tx *gorm.DB, subUserId int) error {
	var tokens []Token
	if err := tx.Where("user_id = ?", subUserId).Find(&tokens).Error; err != nil {
		return err
	}
	if len(tokens) == 0 {
		return nil
	}
	if err := tx.Model(&Token{}).Where("user_id = ?", subUserId).
		Update("status", common.TokenStatusDisabled).Error; err != nil {
		return err
	}
	for _, t := range tokens {
		_ = cacheDeleteToken(t.Key)
	}
	return nil
}

// ---- 三档周期额度：惰性重置 + 校验 + 增减 ----

// monthStartUnix / dayStartUnix 按站点(服务器)时区计算自然月/日起点 unix 秒。
func monthStartUnix(t time.Time) int64 {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, t.Location()).Unix()
}

func dayStartUnix(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).Unix()
}

// resetSubPeriodsInPlace 若跨自然月/日则把对应已用清零、锚点推进到本周期起点；返回是否变更。
func resetSubPeriodsInPlace(u *User, now time.Time) bool {
	changed := false
	if ms := monthStartUnix(now); u.MonthAnchor != ms {
		u.MonthUsedQuota = 0
		u.MonthAnchor = ms
		changed = true
	}
	if ds := dayStartUnix(now); u.DayAnchor != ds {
		u.DayUsedQuota = 0
		u.DayAnchor = ds
		changed = true
	}
	return changed
}

// CheckSubAccountQuota 预扣阶段：惰性重置后校验三档（总/月/日）是否够扣 amount。
// 任一档触顶返回对应哨兵错误。amount 为内部 quota。行锁避免与增减并发下的重置竞态。
func CheckSubAccountQuota(subUserId, amount int) error {
	if amount <= 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var u User
		if err := lockForUpdate(tx).First(&u, subUserId).Error; err != nil {
			return err
		}
		now := time.Now()
		if resetSubPeriodsInPlace(&u, now) {
			if err := tx.Model(&User{}).Where("id = ?", u.Id).Updates(map[string]interface{}{
				"month_used_quota": u.MonthUsedQuota, "month_anchor": u.MonthAnchor,
				"day_used_quota": u.DayUsedQuota, "day_anchor": u.DayAnchor,
			}).Error; err != nil {
				return err
			}
		}
		if u.TotalQuotaLimit >= 0 && u.UsedQuota+amount > u.TotalQuotaLimit {
			return ErrSubTotalQuotaExceeded
		}
		if u.MonthQuotaLimit >= 0 && u.MonthUsedQuota+amount > u.MonthQuotaLimit {
			return ErrSubMonthQuotaExceeded
		}
		if u.DayQuotaLimit >= 0 && u.DayUsedQuota+amount > u.DayQuotaLimit {
			return ErrSubDayQuotaExceeded
		}
		return nil
	})
}

// AddSubAccountPeriodUsage 结算/退款：惰性重置后把「月/日」两档已用各加有符号 delta（floor 0）。
// 「总」档不在此累加——子号 used_quota 已由现有 UpdateUserUsedQuotaAndRequestCount(子号)
// 天然维护（统计归子号），此处只补现有链路没有的周期计数。delta 负数=退款回退。
func AddSubAccountPeriodUsage(subUserId, delta int) error {
	if delta == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var u User
		if err := lockForUpdate(tx).First(&u, subUserId).Error; err != nil {
			return err
		}
		resetSubPeriodsInPlace(&u, time.Now())
		u.MonthUsedQuota += delta
		u.DayUsedQuota += delta
		if u.MonthUsedQuota < 0 {
			u.MonthUsedQuota = 0
		}
		if u.DayUsedQuota < 0 {
			u.DayUsedQuota = 0
		}
		return tx.Model(&User{}).Where("id = ?", u.Id).Updates(map[string]interface{}{
			"month_used_quota": u.MonthUsedQuota, "month_anchor": u.MonthAnchor,
			"day_used_quota": u.DayUsedQuota, "day_anchor": u.DayAnchor,
		}).Error
	})
}
