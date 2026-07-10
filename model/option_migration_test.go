package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 存量库自愈契约：等于上游旧默认值的行被删除（回落到 fork 代码默认值），
// 管理员定制过的值与无关选项保留；标记写入后迁移永不重跑。
func TestMigrateLegacyReliabilityDefaults(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}))
	clearOptions := func() {
		require.NoError(t, DB.Where("1 = 1").Delete(&Option{}).Error)
	}
	clearOptions()
	t.Cleanup(clearOptions)

	seed := []Option{
		{Key: "RetryTimes", Value: "0"},                                    // 旧默认 → 删
		{Key: "AutomaticDisableChannelEnabled", Value: "false"},            // 旧默认 → 删
		{Key: "AutomaticEnableChannelEnabled", Value: "true"},              // 定制 → 留
		{Key: "monitor_setting.auto_test_channel_enabled", Value: "false"}, // 旧默认 → 删
		{Key: "monitor_setting.auto_test_channel_minutes", Value: "30"},    // 定制 → 留
		{Key: "adaptive_routing_setting.enabled", Value: "false"},          // 旧默认 → 删
		{Key: "adaptive_routing_setting.open_threshold", Value: "7"},       // 定制 → 留
		{Key: "SystemName", Value: "九紫离火"},                                 // 无关 → 留
	}
	for _, o := range seed {
		require.NoError(t, DB.Create(&Option{Key: o.Key, Value: o.Value}).Error)
	}

	migrateLegacyReliabilityDefaults()

	rowExists := func(key string) bool {
		var row Option
		return DB.Where(&Option{Key: key}).First(&row).Error == nil
	}
	rowValue := func(key string) string {
		var row Option
		require.NoError(t, DB.Where(&Option{Key: key}).First(&row).Error)
		return row.Value
	}

	assert.False(t, rowExists("RetryTimes"))
	assert.False(t, rowExists("AutomaticDisableChannelEnabled"))
	assert.False(t, rowExists("monitor_setting.auto_test_channel_enabled"))
	assert.False(t, rowExists("adaptive_routing_setting.enabled"))

	assert.Equal(t, "true", rowValue("AutomaticEnableChannelEnabled"))
	assert.Equal(t, "30", rowValue("monitor_setting.auto_test_channel_minutes"))
	assert.Equal(t, "7", rowValue("adaptive_routing_setting.open_threshold"))
	assert.Equal(t, "九紫离火", rowValue("SystemName"))

	assert.Equal(t, "done", rowValue(reliabilityDefaultsMigrationKey))

	// 标记存在后重跑是 no-op：管理员事后显式改回旧值必须生效
	require.NoError(t, DB.Create(&Option{Key: "RetryTimes", Value: "0"}).Error)
	migrateLegacyReliabilityDefaults()
	assert.Equal(t, "0", rowValue("RetryTimes"))
}
