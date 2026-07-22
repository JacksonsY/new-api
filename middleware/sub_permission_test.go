package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// setupSubPermTestDB 以进程内 SQLite 隔离 model.DB，并关闭 Redis，
// 使 SubPermission/RejectSubAccount 的 GetUserCache 走 DB 兜底路径。
func setupSubPermTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db

	t.Cleanup(func() {
		model.DB = originalDB
		common.RedisEnabled = originalRedisEnabled
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

// seedSubPermUser 播种一个用户；parentId>0 且 perms!=nil 时作为带权限白名单的子号。
func seedSubPermUser(t *testing.T, parentId int, perms map[string]bool) *model.User {
	t.Helper()
	s := common.GetRandomString(8)
	u := &model.User{
		Username: "spu_" + s,
		Email:    "spu_" + s + "@sub.local",
		AffCode:  "spa_" + s,
		ParentId: parentId,
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
	}
	if parentId != 0 && perms != nil {
		u.SetSetting(dto.UserSetting{SubAccount: &dto.SubAccountSetting{Permissions: perms}})
	}
	require.NoError(t, model.DB.Create(u).Error)
	return u
}

// runWithGate 以 userId 作为当前用户(c.Set("id"))过给定门中间件，返回末端 handler 是否执行。
func runWithGate(userId int, gate gin.HandlerFunc) bool {
	reached := false
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("id", userId)
		c.Next()
	}, gate)
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	return reached
}

// TestSubPermission 验证功能权限硬校验门：主号放行；子号仅在被授予该权限时放行。
// 关键回归：中间件必须自行按 id 解析用户(不依赖 authHelper 不写的上下文键)，否则 fail-open。
func TestSubPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupSubPermTestDB(t)

	main := seedSubPermUser(t, 0, nil)
	subWith := seedSubPermUser(t, main.Id, map[string]bool{"api_keys": true})
	subWithout := seedSubPermUser(t, main.Id, map[string]bool{"api_keys": false, "playground": true})
	subNoSetting := seedSubPermUser(t, main.Id, nil)

	gate := SubPermission("api_keys")
	assert.True(t, runWithGate(main.Id, gate), "主号放行")
	assert.True(t, runWithGate(subWith.Id, gate), "子号被授予 api_keys 放行")
	assert.False(t, runWithGate(subWithout.Id, gate), "子号未授予 api_keys 拦截")
	assert.False(t, runWithGate(subNoSetting.Id, gate), "子号无权限设置拦截")
}

// TestRejectSubAccount 验证 RejectSubAccount 门：任何子号被拦，主号放行。
func TestRejectSubAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupSubPermTestDB(t)

	main := seedSubPermUser(t, 0, nil)
	sub := seedSubPermUser(t, main.Id, map[string]bool{"wallet": true})

	gate := RejectSubAccount()
	assert.True(t, runWithGate(main.Id, gate), "主号/普通用户放行")
	assert.False(t, runWithGate(sub.Id, gate), "子号被拦截")
}
