package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSubUser 建一个子账号行（parent_id=1），三档上限与已用可指定；返回其 id。
func makeSubUser(t *testing.T, total, month, day, used, monthUsed, dayUsed int) *User {
	t.Helper()
	suffix := common.GetRandomString(8)
	u := &User{
		Username:        "subtest_" + suffix,
		Email:           "subtest_" + suffix + "@sub.local",
		AffCode:         "aff_" + suffix,
		ParentId:        1,
		Status:          common.UserStatusEnabled,
		Role:            common.RoleCommonUser,
		TotalQuotaLimit: total,
		MonthQuotaLimit: month,
		DayQuotaLimit:   day,
		UsedQuota:       used,
		MonthUsedQuota:  monthUsed,
		DayUsedQuota:    dayUsed,
		// 锚点设为当前周期起点，避免 CheckSubAccountQuota 的惰性重置把已用清零
		MonthAnchor: monthStartUnix(time.Now()),
		DayAnchor:   dayStartUnix(time.Now()),
	}
	require.NoError(t, DB.Create(u).Error)
	return u
}

// TestSubAccountTierEnforcement 验证三档额度（总/月/日）各自独立拦截，-1 档不拦。
func TestSubAccountTierEnforcement(t *testing.T) {
	cases := []struct {
		name              string
		total, month, day int
		used, mUsed, dUsed int
		amount            int
		wantErr           error
	}{
		{"total_exceeded", 100, -1, -1, 90, 0, 0, 11, ErrSubTotalQuotaExceeded},
		{"total_boundary_ok", 100, -1, -1, 90, 0, 0, 10, nil},
		{"month_exceeded", -1, 100, -1, 0, 90, 0, 11, ErrSubMonthQuotaExceeded},
		{"day_exceeded", -1, -1, 100, 0, 0, 90, 11, ErrSubDayQuotaExceeded},
		{"all_unlimited", -1, -1, -1, 1_000_000, 1_000_000, 1_000_000, 999_999, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := makeSubUser(t, tc.total, tc.month, tc.day, tc.used, tc.mUsed, tc.dUsed)
			err := CheckSubAccountQuota(u.Id, tc.amount)
			if tc.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

// TestSubAccountPeriodLazyReset 跨自然日/月时，CheckSubAccountQuota 应把对应周期已用清零后再判。
func TestSubAccountPeriodLazyReset(t *testing.T) {
	u := makeSubUser(t, -1, -1, 100, 0, 0, 100) // 日档已用满 100
	// 把日锚点改到昨天，制造"跨日"
	yesterday := dayStartUnix(time.Now().Add(-24 * time.Hour))
	require.NoError(t, DB.Model(&User{}).Where("id = ?", u.Id).Update("day_anchor", yesterday).Error)

	// 跨日后日额度应被重置为 0，故 amount=100 应放行（而非因旧的 100 已用被拦）
	require.NoError(t, CheckSubAccountQuota(u.Id, 100))

	var fresh User
	require.NoError(t, DB.First(&fresh, u.Id).Error)
	assert.Equal(t, 0, fresh.DayUsedQuota, "跨日后日已用应清零")
	assert.Equal(t, dayStartUnix(time.Now()), fresh.DayAnchor, "日锚点应推进到今日起点")
}

// TestAddSubAccountPeriodUsage 月/日两档按有符号 delta 增减，且 floor 0，总档(used_quota)不受影响。
func TestAddSubAccountPeriodUsage(t *testing.T) {
	u := makeSubUser(t, -1, -1, -1, 500, 100, 100)

	require.NoError(t, AddSubAccountPeriodUsage(u.Id, 50))
	var afterAdd User
	require.NoError(t, DB.First(&afterAdd, u.Id).Error)
	assert.Equal(t, 150, afterAdd.MonthUsedQuota)
	assert.Equal(t, 150, afterAdd.DayUsedQuota)
	assert.Equal(t, 500, afterAdd.UsedQuota, "总消耗(used_quota)不由周期函数维护")

	// 退款回退：超额回退应 floor 到 0，不为负
	require.NoError(t, AddSubAccountPeriodUsage(u.Id, -1000))
	var afterRefund User
	require.NoError(t, DB.First(&afterRefund, u.Id).Error)
	assert.Equal(t, 0, afterRefund.MonthUsedQuota, "回退应 floor 0")
	assert.Equal(t, 0, afterRefund.DayUsedQuota)
}

// TestGetUserCachePopulatesParentId 回归：GetUserCache 的 DB 兜底路径必须带出 ParentId。
// 曾漏此字段(手工构造 UserBase)导致子账号权限门 fail-open + relay 计费扣错钱包。
func TestGetUserCachePopulatesParentId(t *testing.T) {
	suffix := common.GetRandomString(8)
	sub := &User{
		Username: "cache_" + suffix,
		Email:    "cache_" + suffix + "@sub.local",
		AffCode:  "cache_" + suffix,
		ParentId: 4242,
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
	}
	require.NoError(t, DB.Create(sub).Error)

	base, err := GetUserCache(sub.Id) // RedisEnabled=false → 走 DB 兜底
	require.NoError(t, err)
	require.NotNil(t, base)
	assert.Equal(t, 4242, base.ParentId, "GetUserCache 必须带出 ParentId(付款人解析/权限门依赖)")
}

// TestCreateSubAccountsSingleLayer 子号不能再建子号（单层从属）。
func TestCreateSubAccountsSingleLayer(t *testing.T) {
	// 先建一个主号
	os1 := common.GetRandomString(6)
	owner := &User{Username: "owner_" + os1, Email: "owner_" + os1 + "@x.local", AffCode: "aff_" + os1, Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
	require.NoError(t, DB.Create(owner).Error)
	// 建一个子号
	ss1 := common.GetRandomString(6)
	sub := &User{Username: "sub_" + ss1, Email: "sub_" + ss1 + "@sub.local", AffCode: "aff_" + ss1, ParentId: owner.Id, Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
	require.NoError(t, DB.Create(sub).Error)

	_, err := CreateSubAccounts(CreateSubAccountsParams{
		ParentId: sub.Id, Prefix: "x", Count: 1, RolePreset: "user",
		TotalLimit: -1, MonthLimit: -1, DayLimit: -1,
	})
	assert.ErrorIs(t, err, ErrSubAccountNested, fmt.Sprintf("子号(id=%d)不应能建子号", sub.Id))
}
