package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// buildEpayClient 不再区分 RSA/MD5 模式：RSA(v2) 与 MD5(v1) 密钥都可同时配置，
// 运行时**优先 RSA、失败则降级 MD5**。选择依据是密钥是否齐全（而非版本开关）：
//   - RSA 两把钥匙（平台公钥 + 商户私钥）齐全 → 先建 v2(RSA) 客户端；
//   - RSA 缺失或密钥解析失败，且配了 MD5 商户密钥 → 降级建 v1(MD5) 客户端。
//
// 各失败分支返回**具体原因**，供上层直接展示，避免笼统的「未配置支付」。
func buildEpayClient() (epay.Client, error) {
	if operation_setting.PayAddress == "" || operation_setting.EpayId == "" {
		return nil, errors.New("易支付未配置：请先填写平台地址与商户 ID")
	}
	hasRSA := operation_setting.EpayPlatformPublicKey != "" && operation_setting.EpayMerchantPrivateKey != ""
	hasMD5 := operation_setting.EpayKey != ""
	if !hasRSA && !hasMD5 {
		return nil, errors.New("易支付未配置签名密钥：RSA（平台公钥 + 商户私钥）或 MD5（商户密钥）至少填一组")
	}

	// 优先 RSA(v2)
	if hasRSA {
		client, err := epay.NewClient(&epay.Config{
			Version:            epay.VersionV2,
			BaseURL:            operation_setting.PayAddress,
			PID:                operation_setting.EpayId,
			PlatformPublicKey:  operation_setting.EpayPlatformPublicKey,
			MerchantPrivateKey: operation_setting.EpayMerchantPrivateKey,
		})
		if err == nil {
			return client, nil
		}
		common.SysError("epay RSA(v2) 初始化失败，尝试降级 MD5：" + err.Error())
		if !hasMD5 {
			return nil, fmt.Errorf("易支付 RSA 密钥无效，且未配置 MD5 兜底：%w", err)
		}
	}

	// 降级 / 仅 MD5(v1)
	client, err := epay.NewClient(&epay.Config{
		Version: epay.VersionV1,
		BaseURL: operation_setting.PayAddress,
		PID:     operation_setting.EpayId,
		Key:     operation_setting.EpayKey,
	})
	if err != nil {
		common.SysError("failed to init epay MD5(v1) client: " + err.Error())
		return nil, fmt.Errorf("易支付 MD5 客户端初始化失败：%w", err)
	}
	return client, nil
}

// GetEpayClient 构建易支付客户端。缺配置 / 密钥无效等**具体原因只记入系统日志**
// （供管理员排查），对外一律返回 nil——调用方统一展示笼统「未配置支付」，
// 避免向终端用户（充值接口任何登录用户可达）泄露支付配置细节。
func GetEpayClient() epay.Client {
	client, err := buildEpayClient()
	if err != nil {
		common.SysError("易支付客户端不可用：" + err.Error())
	}
	return client
}

// ProbeEpayCapabilities 供「商户接口能力检测」使用。与支付运行时同样优先 RSA(v2)，
// 但当 v2 接口不可用（典型：平台只有经典版接口、api/pay/* 返回 HTML 错误页）且配置了
// MD5 时，**自动改用 v1(MD5) 再探一次**，并在结论里说明——让管理员一眼看清平台到底
// 支持哪种协议、该保留哪套密钥。这是一次真实往返（查一个不存在的订单号），无资金副作用。
func ProbeEpayCapabilities() *epay.CapabilityReport {
	if operation_setting.PayAddress == "" || operation_setting.EpayId == "" {
		return nil
	}
	hasRSA := operation_setting.EpayPlatformPublicKey != "" && operation_setting.EpayMerchantPrivateKey != ""
	hasMD5 := operation_setting.EpayKey != ""
	if !hasRSA && !hasMD5 {
		return nil
	}

	if hasRSA {
		v2, err := epay.NewClient(&epay.Config{
			Version:            epay.VersionV2,
			BaseURL:            operation_setting.PayAddress,
			PID:                operation_setting.EpayId,
			PlatformPublicKey:  operation_setting.EpayPlatformPublicKey,
			MerchantPrivateKey: operation_setting.EpayMerchantPrivateKey,
		})
		if err != nil {
			// RSA 密钥都解析不了：有 MD5 就直接测 MD5，否则回报 RSA 配置错误。
			if hasMD5 {
				return probeEpayV1WithNote("RSA 密钥无效（" + err.Error() + "），已改用 MD5(v1) 检测")
			}
			return &epay.CapabilityReport{Version: epay.VersionV2, Summary: "RSA 密钥无效：" + err.Error()}
		}
		report := v2.ProbeCapabilities()
		if report.Reachable && report.CredentialsValid {
			return report // v2 可用，直接采用
		}
		// v2 不可用，且有 MD5 兜底 → 再测 v1，用得上就改推 MD5。
		if hasMD5 {
			return probeEpayV1WithNote("v2(RSA) 接口不可用（" + report.Summary + "）；已自动改用 MD5(v1) 检测")
		}
		return report // 无 MD5 兜底：返回 v2 结论（其中已含「改用 MD5」的建议）
	}

	// 仅配置了 MD5。
	return probeEpayV1WithNote("")
}

// probeEpayV1WithNote 用 MD5(v1) 凭证探测能力；note 非空时作为前缀补进结论，
// 说明为何走到 v1（RSA 不可用而自动降级等）。
func probeEpayV1WithNote(note string) *epay.CapabilityReport {
	client, err := epay.NewClient(&epay.Config{
		Version: epay.VersionV1,
		BaseURL: operation_setting.PayAddress,
		PID:     operation_setting.EpayId,
		Key:     operation_setting.EpayKey,
	})
	if err != nil {
		return &epay.CapabilityReport{Version: epay.VersionV1, Summary: "MD5 客户端初始化失败：" + err.Error()}
	}
	report := client.ProbeCapabilities()
	if note != "" {
		report.Summary = note + "。" + report.Summary
	}
	return report
}
