package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateOptionPersistsExplicitEmptyEpayKey(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Option{}))
	const key = "EpayPlatformPublicKey"

	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousMapValue, hadMapValue := common.OptionMap[key]
	common.OptionMapRWMutex.Unlock()
	var previous Option
	previousErr := DB.Where("key = ?", key).First(&previous).Error
	previousRuntime := operation_setting.EpayPlatformPublicKey
	t.Cleanup(func() {
		if previousErr == nil {
			_ = DB.Save(&previous).Error
		} else {
			_ = DB.Where("key = ?", key).Delete(&Option{}).Error
		}
		operation_setting.EpayPlatformPublicKey = previousRuntime
		common.OptionMapRWMutex.Lock()
		if hadMapValue {
			common.OptionMap[key] = previousMapValue
		} else {
			delete(common.OptionMap, key)
		}
		common.OptionMapRWMutex.Unlock()
	})

	require.NoError(t, UpdateOption(key, "rsa-public-key"))
	require.NoError(t, UpdateOption(key, ""))

	var stored Option
	require.NoError(t, DB.Where("key = ?", key).First(&stored).Error)
	assert.Empty(t, stored.Value)
	assert.Empty(t, operation_setting.EpayPlatformPublicKey)
}

func TestUpdateOptionReturnsPersistenceFailure(t *testing.T) {
	originalDB := DB
	brokenDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := brokenDB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	DB = brokenDB
	t.Cleanup(func() {
		DB = originalDB
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, "test.closed.database.option")
		common.OptionMapRWMutex.Unlock()
	})

	err = UpdateOption("test.closed.database.option", "")
	assert.Error(t, err)
}

func TestUpdateOptionsBulkRollsBackPersistenceWhenRuntimePublishFails(t *testing.T) {
	originalDB := DB
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Option{}))
	DB = testDB

	const key = "PayMethods"
	const originalValue = `[{"name":"Card","type":"card"}]`
	require.NoError(t, testDB.Create(&Option{Key: key, Value: originalValue}).Error)
	previousPayMethods := operation_setting.PayMethods2JsonString()

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{key: originalValue}
	require.NoError(t, operation_setting.UpdatePayMethodsByJsonString(originalValue))
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, operation_setting.UpdatePayMethodsByJsonString(previousPayMethods))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	err = UpdateOptionsBulk(map[string]string{key: "{"})
	assert.Error(t, err)

	var stored Option
	require.NoError(t, testDB.Where("key = ?", key).First(&stored).Error)
	assert.Equal(t, originalValue, stored.Value)
	assert.Equal(t, originalValue, common.OptionMap[key])
	assert.Equal(t, "card", operation_setting.GetPayMethodsSnapshot()[0]["type"])
}

func TestUpdateOptionRollsBackPersistenceWhenRuntimePublishFails(t *testing.T) {
	originalDB := DB
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Option{}))
	DB = testDB

	const key = "PayMethods"
	const originalValue = `[{"name":"Card","type":"card"}]`
	require.NoError(t, testDB.Create(&Option{Key: key, Value: originalValue}).Error)
	previousPayMethods := operation_setting.PayMethods2JsonString()

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{key: originalValue}
	require.NoError(t, operation_setting.UpdatePayMethodsByJsonString(originalValue))
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, operation_setting.UpdatePayMethodsByJsonString(previousPayMethods))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	err = UpdateOption(key, "{")
	assert.Error(t, err)

	var stored Option
	require.NoError(t, testDB.Where("key = ?", key).First(&stored).Error)
	assert.Equal(t, originalValue, stored.Value)
	assert.Equal(t, originalValue, common.OptionMap[key])
	assert.Equal(t, "card", operation_setting.GetPayMethodsSnapshot()[0]["type"])
}
