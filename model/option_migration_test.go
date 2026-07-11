package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 选项表没有默认值与管理员显式配置的来源信息。迁移不能仅凭值匹配旧默认值
// 覆盖管理员有意保留的配置。
func TestMigrateLegacyReliabilityDefaultsPreservesPersistedOptions(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}))
	clearOptions := func() {
		require.NoError(t, DB.Where("1 = 1").Delete(&Option{}).Error)
	}
	clearOptions()
	t.Cleanup(clearOptions)

	seed := []Option{
		{Key: "RetryTimes", Value: "0"},
		{Key: "AutomaticDisableChannelEnabled", Value: "false"},
		{Key: "AutomaticEnableChannelEnabled", Value: "true"},
		{Key: "monitor_setting.auto_test_channel_enabled", Value: "false"},
		{Key: "monitor_setting.auto_test_channel_minutes", Value: "30"},
		{Key: "adaptive_routing_setting.enabled", Value: "false"},
		{Key: "adaptive_routing_setting.open_threshold", Value: "7"},
		{Key: "SystemName", Value: "九紫离火"},
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

	assert.True(t, rowExists("RetryTimes"))
	assert.True(t, rowExists("AutomaticDisableChannelEnabled"))
	assert.True(t, rowExists("monitor_setting.auto_test_channel_enabled"))
	assert.True(t, rowExists("adaptive_routing_setting.enabled"))
	assert.Equal(t, "0", rowValue("RetryTimes"))
	assert.Equal(t, "false", rowValue("AutomaticDisableChannelEnabled"))
	assert.Equal(t, "false", rowValue("monitor_setting.auto_test_channel_enabled"))
	assert.Equal(t, "false", rowValue("adaptive_routing_setting.enabled"))
	assert.Equal(t, "true", rowValue("AutomaticEnableChannelEnabled"))
	assert.Equal(t, "30", rowValue("monitor_setting.auto_test_channel_minutes"))
	assert.Equal(t, "7", rowValue("adaptive_routing_setting.open_threshold"))
	assert.Equal(t, "九紫离火", rowValue("SystemName"))

	assert.False(t, rowExists(reliabilityDefaultsMigrationKey))

	migrateLegacyReliabilityDefaults()
	assert.Equal(t, "0", rowValue("RetryTimes"))
}
