package model

import (
	"fmt"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedBatchUser(t *testing.T, name string, role int, quota int, group string) *User {
	t.Helper()
	accessToken := common.GetUUID()
	user := &User{
		Username:    name,
		Password:    "test-password-hash",
		Role:        role,
		Quota:       quota,
		Group:       group,
		Status:      common.UserStatusEnabled,
		AccessToken: &accessToken,
		AffCode:     common.GetUUID()[:8],
	}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() { DB.Unscoped().Delete(&User{}, user.Id) })
	return user
}

func loadUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, id).Error)
	return user.Quota
}

func injectBeforeNextUserUpdate(t *testing.T, mutate func(*gorm.DB) error) func() bool {
	t.Helper()
	callbackName := "test:batch-user-cas-" + common.GetUUID()
	fired := false
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if fired || tx.Statement.Table != "users" {
			return
		}
		fired = true
		tx.AddError(mutate(tx))
	}))
	t.Cleanup(func() { DB.Callback().Update().Remove(callbackName) })
	return func() bool { return fired }
}

// 批量调额是资金操作：加/减/覆盖/倍乘各自的账目语义必须精确，减扣不得透支。
func TestBatchAdjustUserQuotaModes(t *testing.T) {
	rich := seedBatchUser(t, fmt.Sprintf("batch-rich-%s", common.GetUUID()[:8]), common.RoleCommonUser, 1000, "default")
	poor := seedBatchUser(t, fmt.Sprintf("batch-poor-%s", common.GetUUID()[:8]), common.RoleCommonUser, 100, "default")

	// add
	result, err := BatchAdjustUserQuota(common.RoleRootUser, []int{rich.Id, poor.Id}, BatchQuotaModeAdd, 500, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, result.UpdatedCount)
	assert.Equal(t, 1500, loadUserQuota(t, rich.Id))
	assert.Equal(t, 600, loadUserQuota(t, poor.Id))

	// subtract：余额不足的跳过且分文不动
	result, err = BatchAdjustUserQuota(common.RoleRootUser, []int{rich.Id, poor.Id}, BatchQuotaModeSubtract, 1000, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.UpdatedCount)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, poor.Id, result.Skipped[0].UserId)
	assert.Equal(t, BatchSkipReasonInsufficientQuota, result.Skipped[0].Reason)
	assert.Equal(t, 500, loadUserQuota(t, rich.Id))
	assert.Equal(t, 600, loadUserQuota(t, poor.Id), "余额不足的用户必须分文不动")

	// override
	result, err = BatchAdjustUserQuota(common.RoleRootUser, []int{rich.Id}, BatchQuotaModeOverride, 200, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.UpdatedCount)
	assert.Equal(t, 200, loadUserQuota(t, rich.Id))

	// multiply
	result, err = BatchAdjustUserQuota(common.RoleRootUser, []int{rich.Id}, BatchQuotaModeMultiply, 0, 1.5, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.UpdatedCount)
	assert.Equal(t, 300, loadUserQuota(t, rich.Id))

	// 不存在的用户报 not_found
	result, err = BatchAdjustUserQuota(common.RoleRootUser, []int{99999999}, BatchQuotaModeAdd, 100, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, result.UpdatedCount)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, BatchSkipReasonNotFound, result.Skipped[0].Reason)

	// 非法模式直接报错
	_, err = BatchAdjustUserQuota(common.RoleRootUser, []int{rich.Id}, "steal", 100, 0, nil)
	assert.Error(t, err)
}

// 批量调额必须遵守与计费相同的 int32 quota 边界：计算溢出时饱和，
// 直接输入越界或非有限倍数时拒绝，不能把余额写成负数或触发数据库方言差异。
func TestBatchAdjustUserQuotaBounds(t *testing.T) {
	user := seedBatchUser(t, fmt.Sprintf("batch-bound-%s", common.GetUUID()[:8]), common.RoleCommonUser, common.MaxQuota-10, "default")

	result, err := BatchAdjustUserQuota(common.RoleRootUser, []int{user.Id}, BatchQuotaModeAdd, 100, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.UpdatedCount)
	assert.Equal(t, common.MaxQuota, loadUserQuota(t, user.Id), "add overflow must saturate")

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", common.MaxQuota-10).Error)
	result, err = BatchAdjustUserQuota(common.RoleRootUser, []int{user.Id}, BatchQuotaModeMultiply, 0, 2, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.UpdatedCount)
	assert.Equal(t, common.MaxQuota, loadUserQuota(t, user.Id), "multiply overflow must saturate")

	invalid := []struct {
		name   string
		mode   string
		amount int
		factor float64
	}{
		{name: "add amount above max", mode: BatchQuotaModeAdd, amount: common.MaxQuota + 1},
		{name: "override amount above max", mode: BatchQuotaModeOverride, amount: common.MaxQuota + 1},
		{name: "multiply NaN", mode: BatchQuotaModeMultiply, factor: math.NaN()},
		{name: "multiply infinity", mode: BatchQuotaModeMultiply, factor: math.Inf(1)},
		{name: "multiply above policy max", mode: BatchQuotaModeMultiply, factor: 101},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			before := loadUserQuota(t, user.Id)
			_, err := BatchAdjustUserQuota(common.RoleRootUser, []int{user.Id}, tc.mode, tc.amount, tc.factor, nil)
			assert.Error(t, err)
			assert.Equal(t, before, loadUserQuota(t, user.Id), "invalid input must not mutate quota")
		})
	}
}

func TestBatchAdjustUserQuotaDoesNotOverwriteConcurrentBalanceChange(t *testing.T) {
	user := seedBatchUser(t, fmt.Sprintf("batch-cas-%s", common.GetUUID()[:8]), common.RoleCommonUser, 100, "default")
	const concurrentQuota = 777

	wasInjected := injectBeforeNextUserUpdate(t, func(tx *gorm.DB) error {
		_, err := tx.Statement.ConnPool.ExecContext(
			tx.Statement.Context,
			"UPDATE users SET quota = ? WHERE id = ?",
			concurrentQuota,
			user.Id,
		)
		return err
	})

	result, err := BatchAdjustUserQuota(
		common.RoleRootUser,
		[]int{user.Id},
		BatchQuotaModeOverride,
		200,
		0,
		nil,
	)
	require.NoError(t, err)
	assert.True(t, wasInjected())
	assert.Zero(t, result.UpdatedCount)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, BatchSkipReasonConflict, result.Skipped[0].Reason)
	assert.Equal(t, concurrentQuota, loadUserQuota(t, user.Id))
}

func TestBatchAdjustUserQuotaDoesNotMutateConcurrentlyPromotedTarget(t *testing.T) {
	user := seedBatchUser(t, fmt.Sprintf("batch-role-cas-%s", common.GetUUID()[:8]), common.RoleCommonUser, 100, "default")
	wasInjected := injectBeforeNextUserUpdate(t, func(tx *gorm.DB) error {
		_, err := tx.Statement.ConnPool.ExecContext(
			tx.Statement.Context,
			"UPDATE users SET role = ? WHERE id = ?",
			common.RoleAdminUser,
			user.Id,
		)
		return err
	})

	result, err := BatchAdjustUserQuota(
		common.RoleAdminUser,
		[]int{user.Id},
		BatchQuotaModeAdd,
		100,
		0,
		nil,
	)
	require.NoError(t, err)
	assert.True(t, wasInjected())
	assert.Zero(t, result.UpdatedCount)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, BatchSkipReasonConflict, result.Skipped[0].Reason)
	assert.Equal(t, 100, loadUserQuota(t, user.Id))
}

func TestBatchUpdateUserGroupDoesNotMutateConcurrentlyPromotedTarget(t *testing.T) {
	user := seedBatchUser(t, fmt.Sprintf("batch-group-role-cas-%s", common.GetUUID()[:8]), common.RoleCommonUser, 100, "default")
	wasInjected := injectBeforeNextUserUpdate(t, func(tx *gorm.DB) error {
		_, err := tx.Statement.ConnPool.ExecContext(
			tx.Statement.Context,
			"UPDATE users SET role = ? WHERE id = ?",
			common.RoleAdminUser,
			user.Id,
		)
		return err
	})

	result, err := BatchUpdateUserGroup(
		common.RoleAdminUser,
		[]int{user.Id},
		"vip-batch-test",
		nil,
	)
	require.NoError(t, err)
	assert.True(t, wasInjected())
	assert.Zero(t, result.UpdatedCount)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, BatchSkipReasonConflict, result.Skipped[0].Reason)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, common.RoleAdminUser, reloaded.Role)
	assert.Equal(t, "default", reloaded.Group)
}

// 逐用户角色守卫：管理员不能批量操作同级/更高角色，root 全可。
func TestBatchOpsRespectRoleHierarchy(t *testing.T) {
	commonUser := seedBatchUser(t, fmt.Sprintf("batch-c-%s", common.GetUUID()[:8]), common.RoleCommonUser, 100, "default")
	adminUser := seedBatchUser(t, fmt.Sprintf("batch-a-%s", common.GetUUID()[:8]), common.RoleAdminUser, 100, "default")

	result, err := BatchAdjustUserQuota(common.RoleAdminUser, []int{commonUser.Id, adminUser.Id}, BatchQuotaModeAdd, 100, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.UpdatedCount)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, adminUser.Id, result.Skipped[0].UserId)
	assert.Equal(t, BatchSkipReasonNoPermission, result.Skipped[0].Reason)
	assert.Equal(t, 100, loadUserQuota(t, adminUser.Id), "同级角色必须分文不动")

	groupResult, err := BatchUpdateUserGroup(common.RoleAdminUser, []int{commonUser.Id, adminUser.Id}, "vip-batch-test", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, groupResult.UpdatedCount)

	var reloadedCommon User
	require.NoError(t, DB.First(&reloadedCommon, commonUser.Id).Error)
	assert.Equal(t, "vip-batch-test", reloadedCommon.Group)
	var reloadedAdmin User
	require.NoError(t, DB.First(&reloadedAdmin, adminUser.Id).Error)
	assert.Equal(t, "default", reloadedAdmin.Group, "无权限目标的分组不得变化")
}

// 分组不变的用户跳过 no_change，不产生多余日志。
func TestBatchUpdateUserGroupSkipsNoChange(t *testing.T) {
	user := seedBatchUser(t, fmt.Sprintf("batch-g-%s", common.GetUUID()[:8]), common.RoleCommonUser, 0, "vip")

	result, err := BatchUpdateUserGroup(common.RoleRootUser, []int{user.Id}, "vip", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, result.UpdatedCount)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, BatchSkipReasonNoChange, result.Skipped[0].Reason)
}
