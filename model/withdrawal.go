package model

// jzlh-agent 代理分润出口：① 转成 API 额度(commission_quota → user.quota)；② 现金提现(申请→审批→打款)。
// 独立文件，便于合并 upstream。

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 提现状态。人工打款流程：待审核 → 认领(打款中) → 已打款/已拒绝。
// 认领是防"两个管理员各自线下转账造成重复打款"的闸门：标记已打款前必须先认领，
// 且只有认领人能标记已打款。
const (
	WithdrawalPending    = 1 // 待审核（已预扣分润余额）
	WithdrawalApproved   = 2 // 已通过并打款
	WithdrawalRejected   = 3 // 已拒绝（分润余额已退回）
	WithdrawalProcessing = 4 // 已被管理员认领，线下打款进行中
	WithdrawalCancelled  = 5 // 代理自行撤销（分润余额已退回）
)

// 提现 / 转换相关的校验错误。错误文案统一在 controller 层按用户语言转换后再返回给前端
// （见 controller/agent.go 的 agentErrorI18nKeys）；这里只保留稳定的英文标识，
// 用于日志与错误比对。
var (
	ErrInsufficientCommission     = errors.New("insufficient commission balance")
	ErrInvalidWithdrawalMethod    = errors.New("invalid withdrawal method")
	ErrInvalidConvertAmount       = errors.New("invalid convert amount")
	ErrInvalidWithdrawalAmount    = errors.New("invalid withdrawal amount")
	ErrWithdrawalBelowMinimum     = errors.New("withdrawal amount below minimum")
	ErrTooManyPendingWithdrawals  = errors.New("too many pending withdrawals")
	ErrPayeeInfoRequired          = errors.New("payee info required")
	ErrPayeeNameLength            = errors.New("invalid payee name length")
	ErrPayeeNameFormat            = errors.New("invalid payee name format")
	ErrPayeeAccountAlipayInvalid  = errors.New("invalid alipay payee account")
	ErrPayeeAccountWechatInvalid  = errors.New("invalid wechat payee account")
	ErrPayeeAccountBankInvalid    = errors.New("invalid bank payee account")
	ErrWithdrawalAlreadyProcessed = errors.New("withdrawal already processed")
	ErrWithdrawalNotClaimed       = errors.New("withdrawal not claimed")
	ErrWithdrawalClaimedByOther   = errors.New("withdrawal claimed by another admin")
	ErrPayoutReferenceRequired    = errors.New("payout reference required")
	ErrInvalidReviewAction        = errors.New("invalid review action")
	ErrCannotReviewOwnWithdrawal  = errors.New("cannot review own withdrawal")
	ErrQuotaOverflow              = errors.New("quota overflow")
)

// maxQuotaBalance 单账户 quota 余额上限护栏：历史上 quota 列曾是 32 位（type:int），
// 列放宽后保留为通用防溢出检查，任何转入使余额超过该值时拒绝。
const maxQuotaBalance = math.MaxInt32

// Withdrawal 代理提现申请单。
type Withdrawal struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	UserId       int    `json:"user_id" gorm:"index"`
	Amount       int    `json:"amount"` // 提现额度(quota，申请时预扣自 commission_quota)
	Fee          int    `json:"fee"`    // 手续费(quota)
	Method       string `json:"method"` // alipay / wxpay / bank
	PayeeName    string `json:"payee_name" gorm:"type:varchar(64)"`
	PayeeAccount string `json:"payee_account" gorm:"type:varchar(128)"`
	Remark       string `json:"remark" gorm:"type:varchar(255)"` // 申请人备注(选填)
	Status       int    `json:"status" gorm:"index;default:1"`   // 见上方常量
	AdminRemark  string `json:"admin_remark" gorm:"type:varchar(255)"`
	// ReviewerId 认领/处理该单的管理员 id（认领与终态操作时写入，防重复打款+留经办审计）。
	ReviewerId int `json:"reviewer_id" gorm:"index;default:0"`
	// ExchangeRate 申请时的「本地货币/美元」价格快照，取支付网关共享配置 Price
	// （系统设置 → 计费 → 支付 → 通用设置，与充值同一标准）。提现按美元计价、
	// 人工打款按本币结算时按此快照折算，避免"申请与打款之间调价"的争议。0=未配置。
	ExchangeRate float64 `json:"exchange_rate" gorm:"default:0"`
	CreatedAt    int64   `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt    int64   `json:"updated_at" gorm:"autoUpdateTime"`
	// Username / ReviewerName 申请人与经办管理员用户名，仅超管列表展示用（批量回填，不落库）。
	Username     string `json:"username,omitempty" gorm:"-"`
	ReviewerName string `json:"reviewer_name,omitempty" gorm:"-"`
}

// WithdrawalMethodValid 校验打款方式。
func WithdrawalMethodValid(m string) bool {
	return m == "alipay" || m == "wxpay" || m == "bank"
}

// 收款人字段格式校验(非真实身份核验,仅拦截明显填错/乱填的情况；最终仍需超管人工审核)。
var (
	payeeNameCharsetRe = regexp.MustCompile(`[\p{Han}A-Za-z]`)
	payeePhoneRe       = regexp.MustCompile(`^1[3-9]\d{9}$`)
	payeeEmailRe       = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	payeeWechatIdRe    = regexp.MustCompile(`^[a-zA-Z][-_a-zA-Z0-9]{5,19}$`)
	payeeBankCardRe    = regexp.MustCompile(`^\d{16,19}$`)
)

// validatePayeeName 收款人姓名格式校验：长度 2-30，且至少含一个中/英文字符(拦截纯数字/纯符号误填)。
func validatePayeeName(name string) error {
	n := len([]rune(name))
	if n < 2 || n > 30 {
		return ErrPayeeNameLength
	}
	if !payeeNameCharsetRe.MatchString(name) {
		return ErrPayeeNameFormat
	}
	return nil
}

// validatePayeeAccount 按打款方式校验收款账号格式(支付宝/微信需手机号或对应账号规则；银行卡需位数+Luhn 校验)。
func validatePayeeAccount(method string, account string) error {
	switch method {
	case "alipay":
		if payeePhoneRe.MatchString(account) || payeeEmailRe.MatchString(account) {
			return nil
		}
		return ErrPayeeAccountAlipayInvalid
	case "wxpay":
		if payeePhoneRe.MatchString(account) || payeeWechatIdRe.MatchString(account) {
			return nil
		}
		return ErrPayeeAccountWechatInvalid
	case "bank":
		if payeeBankCardRe.MatchString(account) && luhnValid(account) {
			return nil
		}
		return ErrPayeeAccountBankInvalid
	}
	return ErrInvalidWithdrawalMethod
}

// luhnValid Luhn 校验和算法，用于银行卡号基本合法性校验(不代表账户真实存在)。
func luhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// ConvertCommissionToQuota 把代理的分润余额转成自己可用的 API 额度（原子、防超取）。
func ConvertCommissionToQuota(userId int, amount int) error {
	if amount <= 0 {
		return ErrInvalidConvertAmount
	}
	// jzlh-agent 风控冻结：分润资产出口拦截
	if IsCommissionAssetsFrozen(userId) {
		return ErrCommissionAssetsFrozen
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 余额守卫 + 目标 quota 上限护栏（防转入后溢出）
		res := tx.Model(&User{}).
			Where("id = ? AND commission_quota >= ? AND quota <= ?", userId, amount, maxQuotaBalance-amount).
			Updates(map[string]interface{}{
				"commission_quota": gorm.Expr("commission_quota - ?", amount),
				"quota":            gorm.Expr("quota + ?", amount),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 区分失败原因：余额不足 vs 目标额度将溢出
			var u User
			if err := tx.Select("commission_quota", "quota").Where("id = ?", userId).First(&u).Error; err != nil {
				return err
			}
			if u.CommissionQuota < amount {
				return ErrInsufficientCommission
			}
			return ErrQuotaOverflow
		}
		return nil
	})
	if err != nil {
		return err
	}
	// 同步 Redis 用户额度缓存（与 IncreaseUserQuota 的处理一致）。
	gopool.Go(func() {
		if cerr := cacheIncrUserQuota(userId, int64(amount)); cerr != nil {
			common.SysLog("failed to sync user quota cache after commission convert: " + cerr.Error())
		}
	})
	return nil
}

// CreateWithdrawal 代理申请提现：原子预扣分润余额并创建待审核单。
func CreateWithdrawal(userId int, amount int, method string, payeeName string, payeeAccount string, remark string) (*Withdrawal, error) {
	if amount <= 0 {
		return nil, ErrInvalidWithdrawalAmount
	}
	// jzlh-agent 风控冻结：分润资产出口拦截
	if IsCommissionAssetsFrozen(userId) {
		return nil, ErrCommissionAssetsFrozen
	}
	if amount < common.AgentWithdrawMinQuota {
		return nil, ErrWithdrawalBelowMinimum
	}
	if !WithdrawalMethodValid(method) {
		return nil, ErrInvalidWithdrawalMethod
	}
	payeeName = strings.TrimSpace(payeeName)
	payeeAccount = strings.TrimSpace(payeeAccount)
	if payeeName == "" || payeeAccount == "" {
		return nil, ErrPayeeInfoRequired
	}
	if err := validatePayeeName(payeeName); err != nil {
		return nil, err
	}
	if err := validatePayeeAccount(method, payeeAccount); err != nil {
		return nil, err
	}
	// 未决单数量闸门(防刷单轰炸审核列表)，待审核与打款中都算未决。
	// 预检非原子，并发下可能略超上限，但每单仍原子预扣真金白银，无资损风险；
	// 上限为软性管理约束，可接受。
	if common.AgentWithdrawMaxPending > 0 {
		var pending int64
		if err := DB.Model(&Withdrawal{}).
			Where("user_id = ? AND status IN ?", userId,
				[]int{WithdrawalPending, WithdrawalProcessing}).
			Count(&pending).Error; err != nil {
			return nil, err
		}
		if pending >= int64(common.AgentWithdrawMaxPending) {
			return nil, ErrTooManyPendingWithdrawals
		}
	}
	var w *Withdrawal
	err := DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&User{}).
			Where("id = ? AND commission_quota >= ?", userId, amount).
			Update("commission_quota", gorm.Expr("commission_quota - ?", amount))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrInsufficientCommission
		}
		fee := int(float64(amount) * common.AgentWithdrawFeeRate)
		w = &Withdrawal{
			UserId:       userId,
			Amount:       amount,
			Fee:          fee,
			Method:       method,
			PayeeName:    payeeName,
			PayeeAccount: payeeAccount,
			Remark:       remark,
			Status:       WithdrawalPending,
			ExchangeRate: operation_setting.Price,
		}
		return tx.Create(w).Error
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// ReviewWithdrawal 超管处理提现单，action ∈ claim / release / approve / reject。
// 人工打款两阶段：claim 认领（pending→打款中，锁定经办人）→ 线下转账 →
// approve 标记已打款（仅认领人可操作，且必须填打款流水号/备注）。
// release 释放认领（打款中→pending，任何管理员可操作，兜住经办人离席）；
// reject 可从 pending 或打款中直接拒绝并退回余额。
// 所有状态迁移都用条件 UPDATE 做并发闸门，避免"先读后判再写"竞态。
func ReviewWithdrawal(id int, action string, adminId int, adminRemark string) error {
	// 审批人不得处理自己的提现单（管理员兼代理的历史数据/越权兜底）。
	{
		var w Withdrawal
		if err := DB.First(&w, id).Error; err != nil {
			return err
		}
		if w.UserId == adminId {
			return ErrCannotReviewOwnWithdrawal
		}
	}
	switch action {
	case "claim":
		res := DB.Model(&Withdrawal{}).
			Where("id = ? AND status = ?", id, WithdrawalPending).
			Updates(map[string]interface{}{
				"status":      WithdrawalProcessing,
				"reviewer_id": adminId,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrWithdrawalAlreadyProcessed
		}
		return nil
	case "release":
		res := DB.Model(&Withdrawal{}).
			Where("id = ? AND status = ?", id, WithdrawalProcessing).
			Updates(map[string]interface{}{
				"status":      WithdrawalPending,
				"reviewer_id": 0,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrWithdrawalAlreadyProcessed
		}
		return nil
	case "approve":
		// 打款流水号/凭证必填：人工转账的唯一对账依据，出争议时系统内要能自证。
		if strings.TrimSpace(adminRemark) == "" {
			return ErrPayoutReferenceRequired
		}
		res := DB.Model(&Withdrawal{}).
			Where("id = ? AND status = ? AND reviewer_id = ?", id, WithdrawalProcessing, adminId).
			Updates(map[string]interface{}{
				"status":       WithdrawalApproved,
				"admin_remark": adminRemark,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 区分失败原因，给操作者准确提示。
			var w Withdrawal
			if err := DB.First(&w, id).Error; err != nil {
				return err
			}
			switch {
			case w.Status == WithdrawalPending:
				return ErrWithdrawalNotClaimed
			case w.Status == WithdrawalProcessing && w.ReviewerId != adminId:
				return ErrWithdrawalClaimedByOther
			default:
				return ErrWithdrawalAlreadyProcessed
			}
		}
		return nil
	case "reject":
		return DB.Transaction(func(tx *gorm.DB) error {
			var w Withdrawal
			if err := tx.First(&w, id).Error; err != nil {
				return err
			}
			// 打款中的单只有认领人本人能拒绝(与 approve 一致):防止 A 已线下转账、
			// B 却拒绝退款,造成"既打款又退回余额"的双重支付——这正是认领机制要堵的窗口。
			// 他人要拒绝需先 release 释放认领再操作。pending 单(未认领)任何管理员可拒。
			res := tx.Model(&Withdrawal{}).
				Where("id = ? AND (status = ? OR (status = ? AND reviewer_id = ?))",
					id, WithdrawalPending, WithdrawalProcessing, adminId).
				Updates(map[string]interface{}{
					"status":       WithdrawalRejected,
					"admin_remark": adminRemark,
					"reviewer_id":  adminId,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				if w.Status == WithdrawalProcessing && w.ReviewerId != adminId {
					return ErrWithdrawalClaimedByOther
				}
				return ErrWithdrawalAlreadyProcessed
			}
			// 拒绝：退回预扣的分润余额（仅当上面赢得状态迁移时执行，天然只退一次）
			refund := tx.Model(&User{}).Where("id = ?", w.UserId).
				Update("commission_quota", gorm.Expr("commission_quota + ?", w.Amount))
			if refund.Error != nil {
				return refund.Error
			}
			if refund.RowsAffected == 0 {
				// 用户在 pending 期间被删除：无处可退，记录审计日志避免余额静默蒸发无迹可查。
				common.SysLog(fmt.Sprintf(
					"withdrawal %d rejected but refund skipped: user %d no longer exists (amount=%d)",
					w.Id, w.UserId, w.Amount))
			}
			return nil
		})
	}
	return ErrInvalidReviewAction
}

// CancelWithdrawal 代理撤销自己的待审核提现单并取回预扣余额。
// 仅 pending 可撤：已被管理员认领(打款中)说明线下转账可能已发生，不允许单方面撤回。
func CancelWithdrawal(userId int, id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var w Withdrawal
		if err := tx.First(&w, id).Error; err != nil {
			return err
		}
		res := tx.Model(&Withdrawal{}).
			Where("id = ? AND user_id = ? AND status = ?", id, userId, WithdrawalPending).
			Update("status", WithdrawalCancelled)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrWithdrawalAlreadyProcessed
		}
		return tx.Model(&User{}).Where("id = ?", userId).
			Update("commission_quota", gorm.Expr("commission_quota + ?", w.Amount)).Error
	})
}

// ListUserWithdrawals 代理查看自己的提现记录。
func ListUserWithdrawals(userId int, offset int, limit int) (items []*Withdrawal, total int64, err error) {
	tx := DB.Model(&Withdrawal{}).Where("user_id = ?", userId)
	if err = tx.Count(&total).Error; err != nil {
		return
	}
	err = tx.Order("id desc").Limit(limit).Offset(offset).Find(&items).Error
	return
}

// ListAllWithdrawals 超管查看全部提现（status<=0 表示不筛选），并批量回填申请人用户名。
func ListAllWithdrawals(status int, keyword string, offset int, limit int) (items []*Withdrawal, total int64, err error) {
	tx := DB.Model(&Withdrawal{})
	if status > 0 {
		tx = tx.Where("status = ?", status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where(
			"payee_name LIKE ? OR payee_account LIKE ? OR user_id IN (?)",
			like, like,
			DB.Model(&User{}).Select("id").Where("username LIKE ?", like),
		)
	}
	if err = tx.Count(&total).Error; err != nil {
		return
	}
	if err = tx.Order("id desc").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return
	}
	fillWithdrawalUsernames(items)
	return
}

// fillWithdrawalUsernames 批量查 users 表回填申请人与经办管理员用户名
// （一次 IN 查询，失败仅降级不报错）。
func fillWithdrawalUsernames(items []*Withdrawal) {
	if len(items) == 0 {
		return
	}
	idSet := make(map[int]struct{}, len(items)*2)
	ids := make([]int, 0, len(items)*2)
	collect := func(id int) {
		if id <= 0 {
			return
		}
		if _, ok := idSet[id]; !ok {
			idSet[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	for _, w := range items {
		collect(w.UserId)
		collect(w.ReviewerId)
	}
	var users []struct {
		Id       int
		Username string
	}
	if err := DB.Model(&User{}).Select("id", "username").Where("id IN ?", ids).Find(&users).Error; err != nil {
		common.SysLog("failed to fill withdrawal usernames: " + err.Error())
		return
	}
	nameById := make(map[int]string, len(users))
	for _, u := range users {
		nameById[u.Id] = u.Username
	}
	for _, w := range items {
		w.Username = nameById[w.UserId]
		w.ReviewerName = nameById[w.ReviewerId]
	}
}
