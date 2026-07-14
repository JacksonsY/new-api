package model

// jzlh-supplier 供应商市场后端逻辑：入驻身份、渠道归属/审核、成本价结算聚合、人工打款。
// 全部收敛在本文件 + supplier_ledger.go，尽量不改上游文件，便于合并 upstream。

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
)

var (
	ErrSupplierNotApproved    = errors.New("supplier not approved")
	ErrSupplierChannelDenied  = errors.New("channel not owned by this supplier")
	ErrSupplierRateTooHigh    = errors.New("channel ratio exceeds platform cap")
	ErrSupplierAlreadyApplied = errors.New("supplier application already pending or approved")
	ErrSupplierUserNotFound   = errors.New("user not found")
	ErrSupplierSuspended      = errors.New("supplier access is suspended")
	ErrSupplierAdminForbidden = errors.New("administrators cannot be suppliers")
)

const (
	// SupplierMatureDays 成熟期天数：收益要够老才可打款，躲开退款/风控窗口。
	// 人工打款下这是一个报表 WHERE 条件，不需要后台成熟作业。
	SupplierMatureDays = 3
	// SupplierMaxRate 供应商报价率硬上限：channel_ratio 是"官方价的百分之几供货"，
	// > 1 意味比官方价还贵、平台必亏，绝不允许。审批期封顶 + 结算期夹逼双保险。
	SupplierMaxRate = 1.0
)

// --- 供应商身份 ---

// ApplySupplier 用户申请成为供应商（None→Pending）。已在途或已通过则拒绝。
func ApplySupplier(userId int) error {
	user, err := GetUserById(userId, false)
	if err != nil || user == nil {
		return ErrSupplierUserNotFound
	}
	if user.SupplierStatus == SupplierStatusPending || user.SupplierStatus == SupplierStatusApproved {
		return ErrSupplierAlreadyApplied
	}
	return DB.Model(&User{}).Where("id = ?", userId).Update("supplier_status", SupplierStatusPending).Error
}

// EnsureSupplierApplied 使用户处于"至少已申请"状态，用于「申请即渠道要约」多次提交：
//   - None → 置 Pending（首次申请）
//   - Pending / Approved → 放行（允许再提交渠道要约，即多次申请）
//   - Suspended → 拒绝
func EnsureSupplierApplied(userId int) error {
	user, err := GetUserById(userId, false)
	if err != nil || user == nil {
		return ErrSupplierUserNotFound
	}
	// 管理员及以上不能作为供应商（裁判/运动员）；从入口拦死，绝不写入任何供应商状态，
	// 避免管理员账号落入 Pending/Suspended 后无法在 UI 恢复的死结（见 AdminReviewSupplier）。
	if user.Role >= common.RoleAdminUser {
		return ErrSupplierAdminForbidden
	}
	switch user.SupplierStatus {
	case SupplierStatusApproved, SupplierStatusPending:
		return nil
	case SupplierStatusSuspended:
		return ErrSupplierSuspended
	default:
		return DB.Model(&User{}).Where("id = ?", userId).Update("supplier_status", SupplierStatusPending).Error
	}
}

// SetSupplierStatus 管理员设置供应商状态（审批/停用）。
func SetSupplierStatus(userId int, status int) error {
	return DB.Model(&User{}).Where("id = ?", userId).Update("supplier_status", status).Error
}

// SupplierPayoutMethods 允许的收款方式（与前端 select 同步）；非法值归一为 other。
var SupplierPayoutMethods = map[string]bool{
	"alipay": true, "wechat": true, "bank": true, "usdt": true, "other": true,
}

// UpdateSupplierPayoutInfo 保存/更新供应商收款与联系方式（入驻时与审核后均可调用）。
// 字段长度已在 controller 层按列宽校验；此处仅 trim 并把非法 method 归一为 other。
func UpdateSupplierPayoutInfo(userId int, method, account, name, contact string) error {
	method = strings.TrimSpace(strings.ToLower(method))
	if !SupplierPayoutMethods[method] {
		method = "other"
	}
	return DB.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"supplier_payout_method":  method,
		"supplier_payout_account": strings.TrimSpace(account),
		"supplier_payout_name":    strings.TrimSpace(name),
		"supplier_contact":        strings.TrimSpace(contact),
	}).Error
}

// UpdateSupplierProfile 更新商户资料（名称/联系方式/简介）。入驻申请时写入，审核员据此沟通/展示。
// 与收款账户解耦：结构化收款信息走 UpdateSupplierPayoutInfo（审核通过后的收款设置页）。
func UpdateSupplierProfile(userId int, name, contact, intro string) error {
	return DB.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"supplier_name":    strings.TrimSpace(name),
		"supplier_contact": strings.TrimSpace(contact),
		"supplier_intro":   strings.TrimSpace(intro),
	}).Error
}

// IsApprovedSupplier 判断用户是否为已通过审核的供应商。
func IsApprovedSupplier(userId int) bool {
	var status int
	if err := DB.Model(&User{}).Where("id = ?", userId).Select("supplier_status").Scan(&status).Error; err != nil {
		return false
	}
	return status == SupplierStatusApproved
}

// ListSuppliers 管理端列出供应商（status<0 不过滤，仅列非 None）。
func ListSuppliers(status int, keyword string, offset int, limit int) ([]*User, int64, error) {
	var users []*User
	var total int64
	query := DB.Model(&User{})
	if status >= 0 {
		query = query.Where("supplier_status = ?", status)
	} else {
		query = query.Where("supplier_status <> ?", SupplierStatusNone)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("username LIKE ? OR display_name LIKE ? OR email LIKE ?", like, like, like)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&users).Error
	// 回填可打款额（低频管理端，逐个聚合可接受）
	for _, u := range users {
		if s, e := GetSupplierSettlement(u.Id); e == nil {
			u.SupplierPayableQuota = s.PayableQuota
		}
	}
	return users, total, err
}

// --- 供应商渠道（owner scope）---

// GetSupplierChannels 列出某供应商名下渠道（owner scope）。
func GetSupplierChannels(supplierId int, offset int, limit int) ([]*Channel, int64, error) {
	var channels []*Channel
	var total int64
	query := DB.Model(&Channel{}).Where("user_id = ?", supplierId)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&channels).Error
	return channels, total, err
}

// GetSupplierChannelById owner-scope 取单个渠道；非本人渠道返回 ErrSupplierChannelDenied。
func GetSupplierChannelById(channelId int, supplierId int) (*Channel, error) {
	var channel Channel
	err := DB.Where("id = ?", channelId).First(&channel).Error
	if err != nil {
		return nil, err
	}
	if channel.UserId != supplierId {
		return nil, ErrSupplierChannelDenied
	}
	return &channel, nil
}

// ListPendingSupplierChannels 管理端列出待审的供应商渠道。
func ListPendingSupplierChannels(offset int, limit int) ([]*Channel, int64, error) {
	var channels []*Channel
	var total int64
	query := DB.Model(&Channel{}).Where("audit_status = ? AND user_id > 0", ChannelAuditPending)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&channels).Error
	return channels, total, err
}

// MaterialChannelFieldsChanged 关键字段（Key/BaseURL/Models）是否变化——决定是否触发复审。
func MaterialChannelFieldsChanged(oldCh *Channel, newKey string, newBaseURL string, newModels string) bool {
	oldBase := ""
	if oldCh.BaseURL != nil {
		oldBase = *oldCh.BaseURL
	}
	// 空 Key 表示不改 Key（沿用旧值），不算变化
	if newKey != "" && newKey != oldCh.Key {
		return true
	}
	return newBaseURL != oldBase || newModels != oldCh.Models
}

// AdminApproveChannel 管理员审批通过供应商渠道：定 Group/Priority/Weight、封顶报价率，
// 置 approved 并同步 abilities 进池。
func AdminApproveChannel(channelId int, group string, priority int64, weight uint, channelRatio float64) error {
	if channelRatio > SupplierMaxRate {
		return ErrSupplierRateTooHigh
	}
	var channel Channel
	if err := DB.Where("id = ?", channelId).First(&channel).Error; err != nil {
		return err
	}
	channel.AuditStatus = ChannelAuditApproved
	if group != "" {
		channel.Group = group
	}
	channel.Priority = &priority
	w := weight
	channel.Weight = &w
	channel.ChannelRatio = &channelRatio
	// Update() 会调 UpdateAbilities；此时 audit=approved 故会真正建 abilities 进池。
	return channel.Update()
}

// AdminRejectChannel 管理员驳回供应商渠道（保留渠道记录，附驳回备注）。
func AdminRejectChannel(channelId int, remark string) error {
	var channel Channel
	if err := DB.Where("id = ?", channelId).First(&channel).Error; err != nil {
		return err
	}
	channel.AuditStatus = ChannelAuditRejected
	channel.Remark = &remark
	return channel.Update() // audit!=approved → UpdateAbilities 删除 abilities 出池
}

// GetApprovedSupplierChannels 列出所有已通过审核的供应商渠道（定期复检批量用）。
func GetApprovedSupplierChannels() ([]*Channel, error) {
	var channels []*Channel
	err := DB.Where("user_id > 0 AND audit_status = ?", ChannelAuditApproved).Find(&channels).Error
	return channels, err
}

// AutoSuspendChannelOnCritical 检测判定 critical 时把供应商渠道打回待审、摘出路由池。
// 只作用于供应商渠道（UserId>0）；平台自营渠道不动（管理员自行处理）。软处置：
// 打回 pending 而非永久拉黑，供应商可修复后重新提交（对齐"critical→自动停用+人工可申诉"）。
func AutoSuspendChannelOnCritical(channelId int) error {
	var channel Channel
	if err := DB.Where("id = ?", channelId).First(&channel).Error; err != nil {
		return err
	}
	if channel.UserId == 0 || channel.AuditStatus != ChannelAuditApproved {
		return nil
	}
	channel.AuditStatus = ChannelAuditPending
	return channel.Update() // 审核门 → UpdateAbilities 删 abilities 出池
}

// --- 成本价结算（billing-critical，守 R2/R4 不变量）---

// SupplierSettlement 某供应商的结算快照。
type SupplierSettlement struct {
	SupplierId       int    `json:"supplier_id"`
	SupplierName     string `json:"supplier_name,omitempty"`
	GrossQuota       int64  `json:"gross_quota"`       // 全部毛收入（含未成熟）
	MaturedQuota     int64  `json:"matured_quota"`     // 成熟窗口外的毛收入
	PaidQuota        int64  `json:"paid_quota"`        // 累计已打款
	ConfiscatedQuota int64  `json:"confiscated_quota"` // 累计已没收
	PayableQuota     int64  `json:"payable_quota"`     // 当前可打款 = matured - paid - confiscated (>=0)
}

// supplierGrossExpr 跨库安全的 Σmin(channel_quota, quota)。
// 用 CASE 避免 LEAST(MySQL/PG) / MIN 标量(SQLite) 的方言差异；min 夹逼守住
// "平台毛利永不为负"（R4：供应商报价率固定、大折扣组可能 channel_quota>quota）。
func supplierGrossExpr() string {
	return "COALESCE(SUM(CASE WHEN channel_quota < quota THEN channel_quota ELSE quota END),0)"
}

// sumSupplierGross 聚合某供应商的毛收入。
//
// 过滤 type=LogTypeConsume AND token_id>0：token_id>0 排除渠道测试（testUserID 无 token，
// TokenId=0，见 controller/channel-test.go），守住 R2 不把管理员测渠道算成供应商收益。
// 已知微小误差：违规罚费（service/violation_fee.go）带真实 TokenId 会被计入——罕见且金额
// 小，跨库过滤 other JSON marker 太脆弱，暂接受。
func sumSupplierGross(supplierId int, maturedBefore int64) (int64, error) {
	var total int64
	query := LOG_DB.Model(&Log{}).
		Where("supplier_id = ? AND type = ? AND token_id > 0", supplierId, LogTypeConsume).
		Select(supplierGrossExpr())
	if maturedBefore > 0 {
		query = query.Where("created_at < ?", maturedBefore)
	}
	err := query.Scan(&total).Error
	return total, err
}

// GetSupplierSettlement 实时聚合某供应商结算（logs 在 LOG_DB、台账在 DB，跨库两查）。
func GetSupplierSettlement(supplierId int) (*SupplierSettlement, error) {
	now := common.GetTimestamp()
	matureCutoff := now - int64(SupplierMatureDays)*86400
	gross, err := sumSupplierGross(supplierId, 0)
	if err != nil {
		return nil, err
	}
	matured, err := sumSupplierGross(supplierId, matureCutoff)
	if err != nil {
		return nil, err
	}
	paid, err := GetSupplierPaidQuota(supplierId)
	if err != nil {
		return nil, err
	}
	confiscated, err := GetSupplierConfiscatedQuota(supplierId)
	if err != nil {
		return nil, err
	}
	payable := matured - paid - confiscated
	if payable < 0 {
		payable = 0
	}
	return &SupplierSettlement{
		SupplierId:       supplierId,
		GrossQuota:       gross,
		MaturedQuota:     matured,
		PaidQuota:        paid,
		ConfiscatedQuota: confiscated,
		PayableQuota:     payable,
	}, nil
}

// settleSupplierLedgerLocked 在锁住供应商行的事务内重算结算并写一条台账（打款/罚没）。
// 锁供应商 User 行以串行化同一供应商的并发打款/罚没：解锁在提交后，故第二笔会读到
// 第一笔已提交的台账、看到收缩后的 payable，从而正确以当前 payable 为上界——修掉先前
// 「读结算→Insert」无事务无锁导致两笔并发各 ≤payable 却合计超付/超没的 TOCTOU 竞态。
func settleSupplierLedgerLocked(supplierId int, ledgerType int, amount int64, voucher, remark string, operatorId int) error {
	if amount <= 0 {
		return ErrInvalidWithdrawalAmount
	}
	// matured 来自 LOG_DB（慢变、且不受打款/罚没影响），在事务外算：单连接(SQLite
	// MaxOpenConns=1)下若在事务内再走全局 DB/LOG_DB 会等待事务占住的连接 → 死锁。
	matured, err := sumSupplierGross(supplierId, common.GetTimestamp()-int64(SupplierMatureDays)*86400)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		// 锁供应商行串行化并发打款/罚没（MySQL/PG 靠 FOR UPDATE，SQLite 靠单连接）。
		var u User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", supplierId).First(&u).Error; err != nil {
			return err
		}
		// 竞态变量 paid/confiscated 用 tx 读（同一连接，绝不走全局 DB，避免死锁）；
		// 第二笔在第一笔提交后才拿到锁/连接，故能读到收缩后的 payable。
		var paid, confiscated int64
		if err := tx.Model(&SupplierLedger{}).
			Where("supplier_id = ? AND type = ?", supplierId, SupplierLedgerTypePayout).
			Select("COALESCE(SUM(quota),0)").Scan(&paid).Error; err != nil {
			return err
		}
		if err := tx.Model(&SupplierLedger{}).
			Where("supplier_id = ? AND type = ?", supplierId, SupplierLedgerTypeConfiscation).
			Select("COALESCE(SUM(quota),0)").Scan(&confiscated).Error; err != nil {
			return err
		}
		payable := matured - paid - confiscated
		if payable < 0 {
			payable = 0
		}
		if amount > payable {
			return ErrInsufficientCommission
		}
		return tx.Create(&SupplierLedger{
			SupplierId: supplierId,
			Type:       ledgerType,
			Quota:      amount,
			Voucher:    voucher,
			Remark:     remark,
			OperatorId: operatorId,
			CreatedAt:  now,
		}).Error
	})
}

// RecordSupplierPayout 管理员人工打款后回记一条 payout 台账。amount<=0 或超过可打款额则拒绝。
func RecordSupplierPayout(supplierId int, amount int64, voucher string, remark string, operatorId int) error {
	return settleSupplierLedgerLocked(supplierId, SupplierLedgerTypePayout, amount, voucher, remark, operatorId)
}

// ConfiscateSupplier 风控没收供应商未打款收益（记 confiscation 台账）。以当前 payable 为上界，
// 不能没收超过应结未结的部分（与打款对称）。
func ConfiscateSupplier(supplierId int, amount int64, remark string, operatorId int) error {
	return settleSupplierLedgerLocked(supplierId, SupplierLedgerTypeConfiscation, amount, "", remark, operatorId)
}
