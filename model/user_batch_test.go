package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
