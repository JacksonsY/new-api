package middleware

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

// 已通过 siteverify 的 token 缓存一次，用于兼容"发验证码 + 注册"这类前端复用
// 同一 token 的两步流程（Cloudflare siteverify 对同一 token 二次校验会失败）。
// 命中即删除：同一 token 最多放行两次（一次真实校验 + 一次缓存消费），
// 过期与硬上限防止恶意请求撑爆内存。
var (
	turnstileVerifiedMutex  sync.Mutex
	turnstileVerifiedTokens = map[string]time.Time{}
)

const (
	turnstileVerifiedTTL       = 5 * time.Minute
	turnstileVerifiedHardLimit = 100000
)

func consumeVerifiedTurnstileToken(token string) bool {
	turnstileVerifiedMutex.Lock()
	defer turnstileVerifiedMutex.Unlock()
	expireAt, ok := turnstileVerifiedTokens[token]
	if !ok {
		return false
	}
	delete(turnstileVerifiedTokens, token)
	return time.Now().Before(expireAt)
}

func rememberVerifiedTurnstileToken(token string) {
	turnstileVerifiedMutex.Lock()
	defer turnstileVerifiedMutex.Unlock()
	now := time.Now()
	for t, expireAt := range turnstileVerifiedTokens {
		if now.After(expireAt) {
			delete(turnstileVerifiedTokens, t)
		}
	}
	if len(turnstileVerifiedTokens) >= turnstileVerifiedHardLimit {
		return
	}
	turnstileVerifiedTokens[token] = now.Add(turnstileVerifiedTTL)
}

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			response := c.Query("turnstile")
			if response == "" {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile token 为空",
				})
				c.Abort()
				return
			}
			if consumeVerifiedTurnstileToken(response) {
				c.Next()
				return
			}
			rawRes, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", url.Values{
				"secret":   {common.TurnstileSecretKey},
				"response": {response},
				"remoteip": {c.ClientIP()},
			})
			if err != nil {
				common.SysLog(err.Error())
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			defer rawRes.Body.Close()
			var res turnstileCheckResponse
			err = common.DecodeJson(rawRes.Body, &res)
			if err != nil {
				common.SysLog(err.Error())
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				c.Abort()
				return
			}
			if !res.Success {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile 校验失败，请刷新重试！",
				})
				c.Abort()
				return
			}
			rememberVerifiedTurnstileToken(response)
		}
		c.Next()
	}
}
