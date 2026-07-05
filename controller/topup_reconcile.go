package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type epayReconcileRequest struct {
	// MinAgeSeconds/MaxAgeSeconds 对账窗口（按订单创建时间距今的秒数），默认 [60s, 7天]
	MinAgeSeconds int64 `json:"min_age_seconds"`
	MaxAgeSeconds int64 `json:"max_age_seconds"`
	Limit         int   `json:"limit"`
	// DryRun 缺省 true：先出报告，确认后再带 false 真补账
	DryRun *bool `json:"dry_run"`
}

// EpayReconcile 手动触发易支付对账（自动任务只兜近 10 分钟，更早的漏单走这里）。
func EpayReconcile(c *gin.Context) {
	var req epayReconcileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.MinAgeSeconds <= 0 {
		req.MinAgeSeconds = 60
	}
	if req.MaxAgeSeconds <= 0 {
		req.MaxAgeSeconds = 7 * 24 * 3600
	}
	if req.Limit <= 0 {
		req.Limit = 200
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}

	summary, err := service.ReconcileEpayOrders(req.MinAgeSeconds, req.MaxAgeSeconds, req.Limit, dryRun)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "topup.epay_reconcile", map[string]interface{}{
		"dry_run":         dryRun,
		"min_age_seconds": req.MinAgeSeconds,
		"max_age_seconds": req.MaxAgeSeconds,
		"scanned":         summary.Scanned,
		"completed":       summary.Completed,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    summary,
	})
}

// EpayDetectCapabilities 检测当前 Epay 配置的商户是否具备各接口能力
// （无副作用探测：查一个不存在的订单号，据往返结果判断可达性/凭证/端点）。
func EpayDetectCapabilities(c *gin.Context) {
	client := service.GetEpayClient()
	if client == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Epay 未配置或配置不完整（请先填写平台地址、商户 ID 及对应签名方式的密钥）",
		})
		return
	}
	report := client.ProbeCapabilities()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    report,
	})
}
