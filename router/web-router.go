package router

import (
	"embed"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// ThemeAssets holds the embedded frontend assets for both themes.
type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
}

// nonSPAPathPrefixes 是绝不能回退到 SPA HTML 的后端/运维路径前缀。
// 未注册时必须返回 API 风格 404，避免 /metrics 等被 index.html 伪装成 200。
//
// 注意：前端控制台路由是 /dashboard 与 /dashboard/$section（如 /dashboard/overview）。
// 这里只能拦截 OpenAI 兼容账单 API 前缀 /dashboard/billing，不能把整个 /dashboard
// 标成 non-SPA，否则登录后跳转会拿到 JSON 404 而不是 SPA HTML。
// /seedance 是本 fork 的 Seedance(火山 ARK)视频入口，一并纳入。
var nonSPAPathPrefixes = []string{
	"/api",
	"/v1",
	"/v1beta",
	"/assets",
	"/metrics",
	"/pg",
	"/mj",
	"/suno",
	"/kling",
	"/jimeng",
	"/seedance",
	"/dashboard/billing",
	"/healthz",
	"/livez",
	"/readyz",
	"/frontend-healthz",
}

// isNonSPARequestPath 判断路径是否属于 API/Relay/运维端点，禁止 SPA NoRoute 接管。
func isNonSPARequestPath(requestURI string) bool {
	path := requestURI
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		path = "/"
	}
	for _, prefix := range nonSPAPathPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	// Midjourney 模式前缀：/:mode/mj 或 /:mode/mj/...
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) >= 2 && parts[1] == "mj" {
		return true
	}
	return false
}

func SetWebRouter(router *gin.Engine, assets ThemeAssets) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/default/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	themeFS := common.NewThemeAwareFS(defaultFS, classicFS)

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(static.Serve("/", themeFS))
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if isNonSPARequestPath(c.Request.RequestURI) {
			// 未注册的后端/运维路径返回 JSON 404，禁止回退 HTML。
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		if common.GetTheme() == "classic" {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.ClassicIndexPage)
		} else {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.DefaultIndexPage)
		}
	})
}
