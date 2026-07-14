package model

import (
	"github.com/QuantumNous/new-api/common"
)

// DetectionRecord Veridrop 真伪检测历史（jzlh-veridrop），兼作渠道红黑榜数据源。
//
// ChannelId>0 为平台渠道验真；ChannelId=0 为公开检测页的临时检测（按 Domain 聚合上榜）。
// Results 存完整 DetectionReport JSON（走 common.Marshal），证据面板按需展开。
type DetectionRecord struct {
	Id            int     `json:"id" gorm:"primaryKey"`
	ChannelId     int     `json:"channel_id" gorm:"index;default:0"`
	Domain        string  `json:"domain" gorm:"type:varchar(255);index;default:''"` // base_url host，红黑榜聚合键
	Protocol      string  `json:"protocol" gorm:"type:varchar(16);default:''"`      // anthropic/openai/gemini
	Model         string  `json:"model" gorm:"type:varchar(128);default:''"`
	Verdict       string  `json:"verdict" gorm:"type:varchar(16);default:''"` // passed/marginal/failed
	Score         float64 `json:"score" gorm:"default:0"`
	CriticalCount int     `json:"critical_count" gorm:"default:0"`
	Results       string  `json:"results" gorm:"type:text"`                        // 完整 DetectionReport JSON
	Source        string  `json:"source" gorm:"type:varchar(16);index;default:''"` // admin/supplier/cron/public
	ApiKeyMasked  string  `json:"api_key_masked" gorm:"type:varchar(64);default:''"`
	CreatedAt     int64   `json:"created_at" gorm:"bigint;index"`
}

const (
	DetectionSourceAdmin    = "admin"
	DetectionSourceSupplier = "supplier"
	DetectionSourceCron     = "cron"
	DetectionSourcePublic   = "public"
)

func (r *DetectionRecord) Insert() error {
	if r.CreatedAt == 0 {
		r.CreatedAt = common.GetTimestamp()
	}
	return DB.Create(r).Error
}

// ApplyChannelDetectionSnapshot 把一次检测结果写回渠道快照（供列表徽章/排序用）。
// channelId<=0（公开临时检测）时不落渠道，直接返回。
func ApplyChannelDetectionSnapshot(channelId int, verdict string, score float64, criticalCount int, checkedAt int64) error {
	if channelId <= 0 {
		return nil
	}
	return DB.Model(&Channel{}).Where("id = ?", channelId).Updates(map[string]interface{}{
		"detect_verdict":        verdict,
		"detect_score":          score,
		"detect_critical_count": criticalCount,
		"detect_checked_at":     checkedAt,
	}).Error
}

// GetLatestDetectionByChannel 返回某渠道最近一次检测记录（无则 nil）。
func GetLatestDetectionByChannel(channelId int) (*DetectionRecord, error) {
	var record DetectionRecord
	err := DB.Where("channel_id = ?", channelId).Order("id desc").First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// ListDetectionRecords 分页列出检测记录（channelId>0 过滤某渠道，=0 不过滤）。
func ListDetectionRecords(channelId int, offset int, limit int) ([]*DetectionRecord, int64, error) {
	var records []*DetectionRecord
	var total int64
	query := DB.Model(&DetectionRecord{})
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Offset(offset).Limit(limit).Find(&records).Error
	return records, total, err
}

// DetectionLeaderboardEntry 红黑榜按域名聚合的一行（GROUP BY domain，纯聚合，三库安全）。
type DetectionLeaderboardEntry struct {
	Domain        string  `json:"domain"`
	Samples       int64   `json:"samples"`
	AvgScore      float64 `json:"avg_score"`
	MinScore      float64 `json:"min_score"`
	CriticalCount int64   `json:"critical_count"`
	LastCheckedAt int64   `json:"last_checked_at"`
}

// GetDetectionLeaderboard 按域名聚合最近检测结果，出红黑榜。
// 只聚合公开与管理端来源、有域名的记录。
func GetDetectionLeaderboard(offset int, limit int) ([]*DetectionLeaderboardEntry, error) {
	var entries []*DetectionLeaderboardEntry
	err := DB.Model(&DetectionRecord{}).
		Select("domain, COUNT(*) as samples, AVG(score) as avg_score, MIN(score) as min_score, " +
			"SUM(CASE WHEN critical_count > 0 THEN 1 ELSE 0 END) as critical_count, " +
			"MAX(created_at) as last_checked_at").
		Where("domain <> ''").
		Group("domain").
		Order("avg_score asc"). // 分低的排前面（黑榜优先）
		Offset(offset).Limit(limit).
		Scan(&entries).Error
	return entries, err
}
