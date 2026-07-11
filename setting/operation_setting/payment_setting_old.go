/**
此文件为旧版支付设置文件，如需增加新的参数、变量等，请在 payment_setting.go 中添加
This file is the old version of the payment settings file. If you need to add new parameters, variables, etc., please add them in payment_setting.go
*/

package operation_setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var PayAddress = ""
var CustomCallbackAddress = ""
var EpayId = ""
var EpayKey = ""

// 易支付协议版本与 v2(RSA) 密钥。v1 用 EpayKey(MD5)；v2 用平台公钥验签 + 商户私钥签名。
var EpayApiVersion = "v1"
var EpayPlatformPublicKey = ""
var EpayMerchantPrivateKey = ""

type EpaySettingSnapshot struct {
	PayAddress         string
	MerchantID         string
	MD5Key             string
	APIVersion         string
	PlatformPublicKey  string
	MerchantPrivateKey string
	PayMethodCount     int
}

// GetEpaySettingSnapshot returns one coherent view of the legacy Epay options.
// Production writes are published while holding OptionMapRWMutex, including
// bulk updates, so readers cannot observe a protocol paired with half-updated
// credentials.
func GetEpaySettingSnapshot() EpaySettingSnapshot {
	// Keep lock ordering aligned with option publication: the option lock always
	// precedes the PayMethods lock.
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	payMethodsMu.RLock()
	defer payMethodsMu.RUnlock()
	return EpaySettingSnapshot{
		PayAddress:         PayAddress,
		MerchantID:         EpayId,
		MD5Key:             EpayKey,
		APIVersion:         EpayApiVersion,
		PlatformPublicKey:  EpayPlatformPublicKey,
		MerchantPrivateKey: EpayMerchantPrivateKey,
		PayMethodCount:     len(payMethods),
	}
}

var Price = 7.3
var MinTopUp = 1
var USDExchangeRate = 7.3

var payMethodsMu sync.RWMutex

var payMethods = []map[string]string{
	{
		"name": "支付宝",
		"icon": "SiAlipay",
		"type": "alipay",
	},
	{
		"name": "微信",
		"icon": "SiWechat",
		"type": "wxpay",
	},
	{
		"name":      "自定义1",
		"icon":      "LuCreditCard",
		"type":      "custom1",
		"min_topup": "50",
	},
}

func UpdatePayMethodsByJsonString(jsonString string) error {
	var parsed []map[string]string
	if err := common.Unmarshal([]byte(jsonString), &parsed); err != nil {
		return err
	}
	payMethodsMu.Lock()
	payMethods = parsed
	payMethodsMu.Unlock()
	return nil
}

func GetPayMethodsSnapshot() []map[string]string {
	payMethodsMu.RLock()
	defer payMethodsMu.RUnlock()

	snapshot := make([]map[string]string, len(payMethods))
	for i, method := range payMethods {
		methodCopy := make(map[string]string, len(method))
		for key, value := range method {
			methodCopy[key] = value
		}
		snapshot[i] = methodCopy
	}
	return snapshot
}

func PayMethods2JsonString() string {
	jsonBytes, err := common.Marshal(GetPayMethodsSnapshot())
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func ContainsPayMethod(method string) bool {
	payMethodsMu.RLock()
	defer payMethodsMu.RUnlock()
	for _, payMethod := range payMethods {
		if payMethod["type"] == method {
			return true
		}
	}
	return false
}
