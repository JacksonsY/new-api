package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTokenOrUserAuthTestDB(t *testing.T) *gorm.DB {
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

func tokenOrUserAuthSessionCookies(t *testing.T, router *gin.Engine) []*http.Cookie {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNoContent, recorder.Code)
	return recorder.Result().Cookies()
}

func newTokenOrUserAuthTestRouter(t *testing.T, userID int, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("token-or-user-auth-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", userID)
		session.Set("username", "stale-user")
		session.Set("role", common.RoleCommonUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/protected", TokenOrUserAuth(), handler)
	return router
}

func TestTokenOrUserAuthRejectsDBDisabledSessionUser(t *testing.T) {
	db := setupTokenOrUserAuthTestDB(t)
	user := &model.User{
		Id:       101,
		Username: "disabled-user",
		Password: "not-used-in-test",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusDisabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(user).Error)

	handlerCalled := false
	router := newTokenOrUserAuthTestRouter(t, user.Id, func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})
	cookies := tokenOrUserAuthSessionCookies(t, router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	for _, sessionCookie := range cookies {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.False(t, handlerCalled)
}

func TestTokenOrUserAuthRefreshesSessionContextFromDB(t *testing.T) {
	db := setupTokenOrUserAuthTestDB(t)
	user := &model.User{
		Id:       102,
		Username: "fresh-user",
		Password: "not-used-in-test",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
		Group:    "premium",
	}
	require.NoError(t, db.Create(user).Error)

	router := newTokenOrUserAuthTestRouter(t, user.Id, func(c *gin.Context) {
		require.Equal(t, user.Username, c.GetString("username"))
		require.Equal(t, user.Role, c.GetInt("role"))
		require.Equal(t, user.Group, c.GetString("user_group"))
		c.Status(http.StatusNoContent)
	})
	cookies := tokenOrUserAuthSessionCookies(t, router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	for _, sessionCookie := range cookies {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestTokenOrUserAuthRejectsMissingSessionUser(t *testing.T) {
	setupTokenOrUserAuthTestDB(t)
	const missingUserID = 103

	handlerCalled := false
	router := newTokenOrUserAuthTestRouter(t, missingUserID, func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})
	cookies := tokenOrUserAuthSessionCookies(t, router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	for _, sessionCookie := range cookies {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.False(t, handlerCalled)
}

func TestTokenOrUserAuthFailsClosedOnDatabaseError(t *testing.T) {
	db := setupTokenOrUserAuthTestDB(t)
	user := &model.User{
		Id:       104,
		Username: "db-error-user",
		Password: "not-used-in-test",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(user).Error)

	handlerCalled := false
	router := newTokenOrUserAuthTestRouter(t, user.Id, func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})
	cookies := tokenOrUserAuthSessionCookies(t, router)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	for _, sessionCookie := range cookies {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.False(t, handlerCalled)
}

func TestAdminAuthRejectsDeletedSessionUser(t *testing.T) {
	setupTokenOrUserAuthTestDB(t)
	const missingUserID = 105

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("admin-auth-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", missingUserID)
		session.Set("username", "deleted-admin")
		session.Set("role", common.RoleAdminUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	handlerCalled := false
	router.GET("/admin", AdminAuth(), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})

	cookies := tokenOrUserAuthSessionCookies(t, router)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Header.Set("New-Api-User", fmt.Sprintf("%d", missingUserID))
	for _, sessionCookie := range cookies {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.False(t, handlerCalled)
}

func TestAdminAuthRejectsDemotedSessionRole(t *testing.T) {
	// Cookie 声称 admin，DB 已降为普通用户：鉴权必须以 DB 快照为准拒绝，
	// 绝不回读 cookie 里被抬高的角色。
	db := setupTokenOrUserAuthTestDB(t)
	user := &model.User{
		Id:       106,
		Username: "demoted-user",
		Password: "not-used-in-test",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(user).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("admin-auth-role-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", common.RoleAdminUser) // stale elevated cookie
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	handlerCalled := false
	router.GET("/admin", AdminAuth(), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})

	cookies := tokenOrUserAuthSessionCookies(t, router)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	for _, sessionCookie := range cookies {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	// authHelper 对权限不足返回 200 + success:false
	require.Equal(t, http.StatusOK, recorder.Code)
	require.False(t, handlerCalled)
	require.Contains(t, recorder.Body.String(), "false")
}

func TestUserAuthPropagatesStatusIntoContext(t *testing.T) {
	db := setupTokenOrUserAuthTestDB(t)
	user := &model.User{
		Id:       107,
		Username: "status-user",
		Password: "not-used-in-test",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "premium",
	}
	require.NoError(t, db.Create(user).Error)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("user-auth-status-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", "stale-name")
		session.Set("role", common.RoleCommonUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/me", UserAuth(), func(c *gin.Context) {
		require.Equal(t, user.Username, c.GetString("username"))
		require.Equal(t, user.Role, c.GetInt("role"))
		require.Equal(t, user.Status, c.GetInt("status"))
		require.Equal(t, user.Group, c.GetString("user_group"))
		c.Status(http.StatusNoContent)
	})

	cookies := tokenOrUserAuthSessionCookies(t, router)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	request.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	for _, sessionCookie := range cookies {
		request.AddCookie(sessionCookie)
	}
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}
