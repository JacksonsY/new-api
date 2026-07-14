package model

import (
	"github.com/QuantumNous/new-api/common"
)

// SupplierLedger 供应商打款/没收/调整台账（jzlh-supplier）。
//
// 采用人工打款模型：供应商收益不走自助提现，而是管理员出结算报表、线下转账后
// 回系统记一条 payout 台账；风控没收记一条 confiscation。可打款额实时聚合：
//
//	可打款 = 成熟毛收入(Σ min(channel_quota,quota) over 成熟窗口)
//	        − Σpayout − Σconfiscation
//
// 见 supplier.go 的 GetSupplierSettlement。
type SupplierLedger struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	SupplierId int    `json:"supplier_id" gorm:"index"`
	Type       int    `json:"type" gorm:"index"` // 见 SupplierLedgerType* 常量
	Quota      int64  `json:"quota"`             // 正数金额（按 Type 解释为打款/没收/调整）
	PeriodFrom int64  `json:"period_from" gorm:"bigint;default:0"`
	PeriodTo   int64  `json:"period_to" gorm:"bigint;default:0"`
	Voucher    string `json:"voucher" gorm:"type:varchar(255);default:''"` // 凭证号/截图 URL
	Remark     string `json:"remark" gorm:"type:varchar(255);default:''"`
	OperatorId int    `json:"operator_id" gorm:"index;default:0"` // 经办管理员
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index"`
	// 展示回填，不落库
	OperatorName string `json:"operator_name,omitempty" gorm:"-"`
	SupplierName string `json:"supplier_name,omitempty" gorm:"-"`
}

const (
	SupplierLedgerTypePayout       = 1 // 人工打款
	SupplierLedgerTypeConfiscation = 2 // 风控没收
	SupplierLedgerTypeAdjustment   = 3 // 手工调整（正负均可，Quota 仍存正值，语义由 Remark 说明）
)

func (l *SupplierLedger) Insert() error {
	if l.CreatedAt == 0 {
		l.CreatedAt = common.GetTimestamp()
	}
	return DB.Create(l).Error
}

// sumSupplierLedger 汇总某供应商某类型的台账金额。
func sumSupplierLedger(supplierId int, ledgerType int) (int64, error) {
	var total int64
	err := DB.Model(&SupplierLedger{}).
		Where("supplier_id = ? AND type = ?", supplierId, ledgerType).
		Select("COALESCE(SUM(quota),0)").Scan(&total).Error
	return total, err
}

// GetSupplierPaidQuota 返回供应商累计已打款额度。
func GetSupplierPaidQuota(supplierId int) (int64, error) {
	return sumSupplierLedger(supplierId, SupplierLedgerTypePayout)
}

// GetSupplierConfiscatedQuota 返回供应商累计被没收额度。
func GetSupplierConfiscatedQuota(supplierId int) (int64, error) {
	return sumSupplierLedger(supplierId, SupplierLedgerTypeConfiscation)
}

// ListSupplierLedger 分页列出某供应商的台账（supplierId<=0 表示不过滤，管理端全量）。
func ListSupplierLedger(supplierId int, offset int, limit int) ([]*SupplierLedger, int64, error) {
	var records []*SupplierLedger
	var total int64
	query := DB.Model(&SupplierLedger{})
	if supplierId > 0 {
		query = query.Where("supplier_id = ?", supplierId)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&records).Error
	return records, total, err
}
