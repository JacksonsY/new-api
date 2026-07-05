package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// GetEpayClient 按支付设置构建易支付客户端（v1/v2 由 EpayApiVersion 决定），未配置返回 nil。
// 放在 service 层供支付发起（controller）与主动对账（service）共用。
func GetEpayClient() epay.Client {
	if operation_setting.PayAddress == "" || operation_setting.EpayId == "" {
		return nil
	}
	if operation_setting.EpayApiVersion == epay.VersionV2 {
		if operation_setting.EpayPlatformPublicKey == "" || operation_setting.EpayMerchantPrivateKey == "" {
			return nil
		}
	} else if operation_setting.EpayKey == "" {
		return nil
	}
	client, err := epay.NewClient(&epay.Config{
		Version:            operation_setting.EpayApiVersion,
		BaseURL:            operation_setting.PayAddress,
		PID:                operation_setting.EpayId,
		Key:                operation_setting.EpayKey,
		PlatformPublicKey:  operation_setting.EpayPlatformPublicKey,
		MerchantPrivateKey: operation_setting.EpayMerchantPrivateKey,
	})
	if err != nil {
		common.SysError("failed to init epay client: " + err.Error())
		return nil
	}
	return client
}
