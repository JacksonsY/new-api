package controller

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/s3lite"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type UpdateUserStorageSettingRequest struct {
	Endpoint     string `json:"endpoint"`
	Bucket       string `json:"bucket"`
	Region       string `json:"region"`
	AccessKeyID  string `json:"access_key_id"`
	SecretKey    string `json:"secret_key"`
	PublicDomain string `json:"public_domain"`
}

// UpdateUserStorageSetting 保存用户个人存储桶配置。
// 保存前会向桶内试写一个测试文件验证凭证与权限,并自动探测寻址方式(virtual-hosted / path-style)。
// SecretKey 留空表示沿用已保存的密钥,便于只改其他字段。
func UpdateUserStorageSetting(c *gin.Context) {
	var req UpdateUserStorageSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.Bucket = strings.TrimSpace(req.Bucket)
	req.Region = strings.TrimSpace(req.Region)
	req.AccessKeyID = strings.TrimSpace(req.AccessKeyID)
	req.SecretKey = strings.TrimSpace(req.SecretKey)
	req.PublicDomain = strings.TrimSuffix(strings.TrimSpace(req.PublicDomain), "/")

	if req.Endpoint == "" || req.Bucket == "" || req.AccessKeyID == "" {
		common.ApiErrorMsg(c, "Endpoint、Bucket、AccessKey ID 不能为空")
		return
	}
	if len(req.Endpoint) > 512 || len(req.Bucket) > 128 || len(req.Region) > 64 ||
		len(req.AccessKeyID) > 512 || len(req.SecretKey) > 512 || len(req.PublicDomain) > 512 {
		common.ApiErrorMsg(c, "配置项长度超出限制")
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	setting := user.GetSetting()

	// SecretKey 留空时沿用已保存的密钥
	if req.SecretKey == "" {
		if setting.Storage == nil || setting.Storage.SecretKey == "" {
			common.ApiErrorMsg(c, "请填写 SecretKey")
			return
		}
		req.SecretKey = setting.Storage.SecretKey
	}

	cfg := &s3lite.Config{
		Endpoint:     req.Endpoint,
		Bucket:       req.Bucket,
		Region:       req.Region,
		AccessKeyID:  req.AccessKeyID,
		SecretKey:    req.SecretKey,
		PublicDomain: req.PublicDomain,
	}
	if err := cfg.Validate(); err != nil {
		common.ApiErrorMsg(c, "配置无效: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := service.VerifyUserStorage(ctx, cfg); err != nil {
		common.ApiErrorMsg(c, "存储验证失败,请检查 Endpoint、桶名与密钥权限: "+err.Error())
		return
	}

	setting.Storage = &dto.UserStorageSetting{
		Endpoint:     req.Endpoint,
		Bucket:       req.Bucket,
		Region:       req.Region,
		AccessKeyID:  req.AccessKeyID,
		SecretKey:    req.SecretKey,
		PublicDomain: req.PublicDomain,
		PathStyle:    cfg.PathStyle,
	}
	if err := model.UpdateUserSetting(userId, setting); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"path_style": cfg.PathStyle})
}

// DeleteUserStorageSetting 清除用户个人存储桶配置。
func DeleteUserStorageSetting(c *gin.Context) {
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	setting := user.GetSetting()
	if setting.Storage == nil {
		common.ApiSuccess(c, nil)
		return
	}
	setting.Storage = nil
	if err := model.UpdateUserSetting(userId, setting); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
