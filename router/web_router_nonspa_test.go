package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsNonSPARequestPath(t *testing.T) {
	for _, path := range []string{
		"/metrics",
		"/metrics?foo=1",
		"/v1/models",
		"/v1beta/models",
		"/api/status",
		"/pg/chat/completions",
		"/mj/submit",
		"/suno/submit",
		"/kling/v1/videos/text2video",
		"/jimeng/",
		"/seedance/v1/video",
		"/dashboard/billing/usage",
		"/frontend-healthz",
		"/readyz",
		"/livez",
		"/healthz",
		"/fast/mj/task",
	} {
		require.Truef(t, isNonSPARequestPath(path), "expected non-SPA: %s", path)
	}
	for _, path := range []string{
		"/",
		"/console",
		"/pricing",
		"/about",
		"/sign-in",
		"/static/js/index.js",
		"/dashboard",
		"/dashboard/overview",
		"/dashboard/detail",
	} {
		require.Falsef(t, isNonSPARequestPath(path), "expected SPA-capable: %s", path)
	}
}

func TestSetWebRouterDoesNotServeSPAForMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := WebAssets{
		IndexPage: []byte("<html>index</html>"),
	}
	engine := gin.New()
	SetWebRouter(engine, assets)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.NotContains(t, recorder.Header().Get("Content-Type"), "text/html")
	require.NotContains(t, recorder.Body.String(), "<html>index</html>")

	// Console SPA routes still fall back to index.
	home := httptest.NewRecorder()
	engine.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/dashboard/overview", nil))
	require.Equal(t, http.StatusOK, home.Code)
	require.Equal(t, "<html>index</html>", home.Body.String())
}
