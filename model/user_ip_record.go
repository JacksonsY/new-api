package model

// jzlh-agent 用户 IP 快表：登录/注册/API 调用三处无条件埋点，为分销反欺诈的
// IP 重合检测提供不受"用户 IP 日志开关(RecordIpLog)"影响的数据源。
// 设计参考 moeacgx/new-api 的 user_ip_records，两点适配：
//   - 补上了原实现缺失的过期清理调用（见 main.go 的每日清理任务）；
//   - 检测只认 IPv4 非 loopback（IPv6 轮换地址会造成大量噪音）。

import (
	"net"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

type UserIPRecord struct {
	Id     int    `json:"id" gorm:"primaryKey"`
	UserId int    `json:"user_id" gorm:"index:idx_user_ip,priority:1"`
	Ip     string `json:"ip" gorm:"type:varchar(45);index:idx_user_ip,priority:2"`
	Action string `json:"action" gorm:"type:varchar(32)"` // login / register / api_call
	// 秒级时间戳。清理任务与检测窗口都按它过滤。
	CreatedAt int64 `json:"created_at" gorm:"autoCreateTime;index"`
}

func (UserIPRecord) TableName() string {
	return "user_ip_records"
}

// RecordUserIP 异步落一条用户 IP 记录。1 小时内同 (user, ip, action) 去重，
// 控制高频 api_call 场景的写入量；并发下偶发的重复行无害（检测按 DISTINCT ip）。
func RecordUserIP(userId int, ip string, action string) {
	if userId <= 0 || ip == "" || ip == "127.0.0.1" || ip == "::1" {
		return
	}
	gopool.Go(func() {
		recordUserIPSync(userId, ip, action)
	})
}

func recordUserIPSync(userId int, ip string, action string) {
	oneHourAgo := common.GetTimestamp() - 3600
	var count int64
	if err := DB.Model(&UserIPRecord{}).
		Where("user_id = ? AND ip = ? AND action = ? AND created_at > ?", userId, ip, action, oneHourAgo).
		Count(&count).Error; err != nil || count > 0 {
		return
	}
	if err := DB.Create(&UserIPRecord{UserId: userId, Ip: ip, Action: action}).Error; err != nil {
		common.SysLog("failed to record user ip: " + err.Error())
	}
}

// normalizeFraudIP 反欺诈口径的 IP 归一：仅认 IPv4 非 loopback，其余丢弃。
func normalizeFraudIP(ip string) (string, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsLoopback() || parsed.To4() == nil {
		return "", false
	}
	return parsed.String(), true
}

// filterFraudIPs 归一 + 去重，保持输入顺序。
func filterFraudIPs(ips []string) []string {
	seen := make(map[string]bool, len(ips))
	filtered := make([]string, 0, len(ips))
	for _, rawIP := range ips {
		ip, ok := normalizeFraudIP(rawIP)
		if !ok || seen[ip] {
			continue
		}
		seen[ip] = true
		filtered = append(filtered, ip)
	}
	return filtered
}

// getUserIPRecordIPs 取用户在窗口内的去重 IP 列表（快表来源）。
// 上限护栏防止超长 IN 列表拖垮后续交集查询；截断时留日志，不静默。
func getUserIPRecordIPs(userId int, sinceTimestamp int64, limit int) ([]string, error) {
	var ips []string
	query := DB.Model(&UserIPRecord{}).
		Where("user_id = ? AND ip <> ''", userId).
		Distinct("ip")
	if sinceTimestamp > 0 {
		query = query.Where("created_at >= ?", sinceTimestamp)
	}
	if limit > 0 {
		query = query.Limit(limit + 1)
	}
	if err := query.Pluck("ip", &ips).Error; err != nil {
		return nil, err
	}
	if limit > 0 && len(ips) > limit {
		ips = ips[:limit]
		common.SysLog("fraud detection: user ip set truncated, userId=" + strconv.Itoa(userId))
	}
	return filterFraudIPs(ips), nil
}

// getIPOverlapBatch 一次查询取全部下级与给定 IP 集的重合（快表来源）。
func getIPOverlapBatch(inviteeIds []int, inviterIPs []string, sinceTimestamp int64) (map[int][]string, error) {
	result := make(map[int][]string)
	if len(inviteeIds) == 0 || len(inviterIPs) == 0 {
		return result, nil
	}
	type ipUserRow struct {
		UserId int
		Ip     string
	}
	var rows []ipUserRow
	query := DB.Model(&UserIPRecord{}).
		Select("DISTINCT user_id, ip").
		Where("user_id IN ? AND ip IN ?", inviteeIds, inviterIPs)
	if sinceTimestamp > 0 {
		query = query.Where("created_at >= ?", sinceTimestamp)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if ip, ok := normalizeFraudIP(r.Ip); ok {
			result[r.UserId] = append(result[r.UserId], ip)
		}
	}
	return result, nil
}

// CleanOldIPRecords 删除超过保留期的 IP 记录，返回删除行数。由每日清理任务调用。
func CleanOldIPRecords(beforeTimestamp int64) (int64, error) {
	result := DB.Where("created_at < ?", beforeTimestamp).Delete(&UserIPRecord{})
	return result.RowsAffected, result.Error
}
