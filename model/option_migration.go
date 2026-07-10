package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// reliabilityDefaultsMigrationKey 标记一次性迁移已执行。迁移完成后管理员再
// 显式把这些开关改回旧值（例如故意关闭重试）必须生效，因此绝不重复清理。
const reliabilityDefaultsMigrationKey = "MigrationReliabilityDefaultsV1"

// legacyReliabilityDefaultRows 列出可靠性开关在上游的旧默认值。
//
// fork 已把这些开关改为代码层默认开启，但启动加载顺序是「代码默认值 ←
// options 表逐行覆盖」且默认值从不回写数据库：存量部署只要早年在设置页点过
// 保存，表里就留有等于旧默认值的行（RetryTimes=0、false、scheduled_all 等），
// 每次启动都会把新默认值覆盖回关闭状态，防护静默失效。
//
// 迁移只删除「值仍等于上游旧默认」的行——管理员显式定制过的值不等于旧默认，
// 一律不动；被删的 key 回落到代码默认值，并继续跟随未来的默认值调整。
var legacyReliabilityDefaultRows = map[string]string{
	"RetryTimes":                                "0",
	"AutomaticDisableChannelEnabled":            "false",
	"AutomaticEnableChannelEnabled":             "false",
	"monitor_setting.auto_test_channel_enabled": "false",
	"monitor_setting.auto_test_channel_minutes": "10",
	"monitor_setting.channel_test_mode":         "scheduled_all",
	"adaptive_routing_setting.enabled":          "false",
	"adaptive_routing_setting.open_threshold":   "5",
}

// migrateLegacyReliabilityDefaults 在 InitOptionMap 填入代码默认值之后、
// loadOptionsFromDatabase 之前执行。幂等：多实例并发启动时重复删除与
// 重复写标记都安全。
func migrateLegacyReliabilityDefaults() {
	var marker Option
	if err := DB.Where(&Option{Key: reliabilityDefaultsMigrationKey}).First(&marker).Error; err == nil {
		return
	}

	removed := make([]string, 0, len(legacyReliabilityDefaultRows))
	for key, legacyValue := range legacyReliabilityDefaultRows {
		var row Option
		if err := DB.Where(&Option{Key: key}).First(&row).Error; err != nil {
			continue // 无该行：代码默认值本来就生效
		}
		if strings.TrimSpace(row.Value) != legacyValue {
			continue // 管理员显式定制过，保留
		}
		if err := DB.Delete(&Option{Key: key}).Error; err != nil {
			common.SysError("reliability defaults migration: failed to remove legacy row " + key + ": " + err.Error())
			continue
		}
		removed = append(removed, key)
	}

	// 直接落库写标记（不经 UpdateOption/OptionMap）：迁移运行在 OptionMap
	// 填充与 DB 加载之间，标记行随后会被 loadOptionsFromDatabase 正常载入。
	marker = Option{Key: reliabilityDefaultsMigrationKey}
	DB.FirstOrCreate(&marker, Option{Key: reliabilityDefaultsMigrationKey})
	marker.Value = "done"
	if err := DB.Save(&marker).Error; err != nil {
		common.SysError("reliability defaults migration: failed to persist marker: " + err.Error())
		return
	}
	if len(removed) > 0 {
		common.SysLog("reliability defaults migration: removed legacy default rows (fork defaults now apply): " + strings.Join(removed, ", "))
	} else {
		common.SysLog("reliability defaults migration: no legacy default rows found")
	}
}
