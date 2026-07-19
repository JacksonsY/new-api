package middleware

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// SecurityHeaders 为经 HTTPS 到达的请求附加安全响应头。
// 当反向代理/Tunnel 终止 TLS 时，通过 X-Forwarded-Proto 识别 HTTPS。
// HSTS 仅在判定为 HTTPS 时发送，避免本机 http://127.0.0.1 被浏览器错误 HSTS。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		// X-Frame-Options 可经 X_FRAME_OPTIONS 配置；分销/团队场景需把面板嵌进
		// 下游门户(跨子域即跨源)时，设为空或 "off" 关闭该头，避免被浏览器硬拦。
		if xfo := strings.TrimSpace(common.XFrameOptions); xfo != "" && !strings.EqualFold(xfo, "off") {
			c.Header("X-Frame-Options", xfo)
		}

		if isHTTPSRequest(c.Request) {
			// 6 months; no includeSubDomains/preload — single-host production default.
			c.Header("Strict-Transport-Security", "max-age=15768000")
		}
		c.Next()
	}
}

// isHTTPSRequest 判断客户端侧是否为 HTTPS（含反代转发头）。
func isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		return false
	}
	// 取最左（最靠近客户端）的协议标记。
	if i := strings.IndexByte(proto, ','); i >= 0 {
		proto = proto[:i]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}
