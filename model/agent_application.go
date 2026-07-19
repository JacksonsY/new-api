// Copyright (C) 2023-2026 QuantumNous
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// For commercial licensing, please contact support@quantumnous.com

package model

// jzlh-agent 代理自助入驻申请:补齐与供应商入驻对称的"申请→审核"漏斗。
// 一人一行(uniqueIndex user_id):被拒后重新提交把同一行翻回 pending,
// 保留最近一次拒绝原因可追溯到审核完成前。

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	AgentApplicationPending  = 1
	AgentApplicationApproved = 2
	AgentApplicationRejected = 3
)

type AgentApplication struct {
	Id     int `json:"id"`
	UserId int `json:"user_id" gorm:"uniqueIndex"`
	// Contact 联系方式(审核沟通用);Note 推广渠道/计划说明。
	Contact      string `json:"contact" gorm:"type:varchar(191)"`
	Note         string `json:"note" gorm:"type:text"`
	Status       int    `json:"status" gorm:"default:1"`
	Reason       string `json:"reason" gorm:"type:varchar(255)"`
	ReviewerId   int    `json:"reviewer_id"`
	CreatedTime  int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime  int64  `json:"updated_time" gorm:"bigint"`
	ReviewedTime int64  `json:"reviewed_time" gorm:"bigint"`
}

// SubmitAgentApplication 提交/重新提交申请。pending 期间允许更新材料;
// 已通过(用户已是代理)拒绝重复提交——那是脏状态,让调用方先查身份。
func SubmitAgentApplication(userId int, contact string, note string) error {
	contact = strings.TrimSpace(contact)
	note = strings.TrimSpace(note)
	if contact == "" || len(contact) > 191 {
		return errors.New("请填写有效的联系方式")
	}
	if note == "" || len(note) > 2000 {
		return errors.New("请填写推广渠道说明(2000 字以内)")
	}
	now := common.GetTimestamp()
	var existing AgentApplication
	err := DB.Where("user_id = ?", userId).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DB.Create(&AgentApplication{
			UserId: userId, Contact: contact, Note: note,
			Status: AgentApplicationPending, CreatedTime: now, UpdatedTime: now,
		}).Error
	}
	if err != nil {
		return err
	}
	if existing.Status == AgentApplicationApproved {
		return errors.New("申请已通过,无需重复提交")
	}
	return DB.Model(&AgentApplication{}).Where("id = ?", existing.Id).
		Updates(map[string]interface{}{
			"contact":      contact,
			"note":         note,
			"status":       AgentApplicationPending,
			"updated_time": now,
		}).Error
}

// GetAgentApplicationByUserId 查询自己的申请(无申请返回 nil, nil)。
func GetAgentApplicationByUserId(userId int) (*AgentApplication, error) {
	var app AgentApplication
	err := DB.Where("user_id = ?", userId).First(&app).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &app, nil
}

// ListAgentApplications 审核队列(root):status<=0 列全部,否则按状态筛。
func ListAgentApplications(status int, offset int, limit int) (apps []*AgentApplication, total int64, err error) {
	tx := DB.Model(&AgentApplication{})
	if status > 0 {
		tx = tx.Where("status = ?", status)
	}
	if err = tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = tx.Order("status asc, id desc").Limit(limit).Offset(offset).Find(&apps).Error
	return apps, total, err
}

// ReviewAgentApplication 审核(root):approve 在同一事务内落两笔——申请置
// approved + 用户设为代理(与手动 SetUserAgent 同一字段口径);任一步失败整体
// 回滚,不允许"申请通过了人却不是代理"的脏账。reject 记录原因。
// 仅 pending 可审(CAS 防并发双审)。
func ReviewAgentApplication(id int, approve bool, usageProfitRate float64, reason string, reviewerId int) error {
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		var app AgentApplication
		if err := lockForUpdate(tx).First(&app, "id = ?", id).Error; err != nil {
			return errors.New("申请不存在")
		}
		if app.Status != AgentApplicationPending {
			return errors.New("申请已审核")
		}
		newStatus := AgentApplicationRejected
		if approve {
			newStatus = AgentApplicationApproved
		}
		if err := tx.Model(&AgentApplication{}).Where("id = ? AND status = ?", id, AgentApplicationPending).
			Updates(map[string]interface{}{
				"status":        newStatus,
				"reason":        strings.TrimSpace(reason),
				"reviewer_id":   reviewerId,
				"reviewed_time": now,
				"updated_time":  now,
			}).Error; err != nil {
			return err
		}
		if approve {
			// 复检角色:提交申请时是普通用户,审核前可能已被提为管理员。管理员兼任
			// 代理会打破裁判/运动员隔离(既能发额度又能按下游消费抽成),与
			// AdminReviewSupplier 的处理保持一致——审核时刻再判一次。
			var applicant User
			if err := tx.Select("id", "role").First(&applicant, "id = ?", app.UserId).Error; err != nil {
				return errors.New("申请人不存在")
			}
			if applicant.Role >= common.RoleAdminUser {
				return errors.New("管理员及以上不能成为代理")
			}
			result := tx.Model(&User{}).Where("id = ?", app.UserId).
				Updates(map[string]interface{}{
					"agent_type":          "normal",
					"usage_profit_rate":   usageProfitRate,
					"agent_approved_time": now, // v2 §3.4 生效时刻落档(审计+未来边界地基)
				})
			if result.Error != nil {
				return result.Error
			}
			// 事务只保证两条语句同生共死,不保证第二条命中了行:申请人被删后
			// 更新会匹配 0 行且不报错,留下"申请已通过但无人成为代理"的脏账。
			if result.RowsAffected == 0 {
				return errors.New("申请人不存在")
			}
		}
		return nil
	})
}
