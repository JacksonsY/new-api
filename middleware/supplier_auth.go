package middleware

// jzlh-supplier 供应商自助接口鉴权中间件，单独成文件不改上游 auth.go。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
)

// SupplierAuth 要求当前用户是「已通过审核」的供应商，否则拒绝。
// 入驻申请接口（/supplier/apply）只挂 UserAuth，不挂本中间件（申请者尚非供应商）。
func SupplierAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		id := c.GetInt("id")
		user, err := model.GetUserById(id, false)
		if err != nil || user == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgAgentUserNotFound)})
			c.Abort()
			return
		}
		if user.Status != common.UserStatusEnabled {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgAuthUserBanned)})
			c.Abort()
			return
		}
		if user.SupplierStatus != model.SupplierStatusApproved {
			// i18n 键在 task#47 统一补齐，此处双语兜底文案。
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "需要供应商权限 / supplier access required"})
			c.Abort()
			return
		}
		c.Next()
	}
}
