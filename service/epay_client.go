package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// buildEpayClient uses the persisted protocol selected by capability detection
// and saved by the administrator. It must not silently prefer parseable RSA
// keys after detection established that only the v1 endpoint is usable.
func buildEpayClient() (epay.Client, error) {
	settings := operation_setting.GetEpaySettingSnapshot()
	return buildEpayClientFromSnapshot(settings)
}

func buildEpayClientFromSnapshot(settings operation_setting.EpaySettingSnapshot) (epay.Client, error) {
	if settings.PayAddress == "" || settings.MerchantID == "" {
		return nil, errors.New("易支付未配置：请先填写平台地址与商户 ID")
	}
	switch settings.APIVersion {
	case "", epay.VersionV1:
		if settings.MD5Key == "" {
			return nil, errors.New("易支付 v1 未配置 MD5 商户密钥")
		}
		client, err := epay.NewClient(&epay.Config{
			Version: epay.VersionV1,
			BaseURL: settings.PayAddress,
			PID:     settings.MerchantID,
			Key:     settings.MD5Key,
		})
		if err != nil {
			return nil, fmt.Errorf("易支付 MD5 客户端初始化失败：%w", err)
		}
		return client, nil
	case epay.VersionV2:
		if settings.PlatformPublicKey == "" || settings.MerchantPrivateKey == "" {
			return nil, errors.New("易支付 v2 未配置完整 RSA 密钥")
		}
		client, err := epay.NewClient(&epay.Config{
			Version:            epay.VersionV2,
			BaseURL:            settings.PayAddress,
			PID:                settings.MerchantID,
			PlatformPublicKey:  settings.PlatformPublicKey,
			MerchantPrivateKey: settings.MerchantPrivateKey,
		})
		if err != nil {
			return nil, fmt.Errorf("易支付 RSA 客户端初始化失败：%w", err)
		}
		return client, nil
	default:
		return nil, fmt.Errorf("易支付协议版本无效：%s", settings.APIVersion)
	}
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

// ProbeEpayCapabilities 供「商户接口能力检测」使用。检测会先尝试能力更完整的
// RSA(v2)，接口不可用且配置了 MD5 时再检测 v1；前端把报告中的 Version 写回表单，
// 管理员保存后 buildEpayClient 才会按该版本发起真实支付。
// 这是一次真实往返（查一个不存在的订单号），无资金副作用。
func ProbeEpayCapabilities() *epay.CapabilityReport {
	settings := operation_setting.GetEpaySettingSnapshot()
	if settings.PayAddress == "" || settings.MerchantID == "" {
		return nil
	}
	hasRSA := settings.PlatformPublicKey != "" && settings.MerchantPrivateKey != ""
	hasMD5 := settings.MD5Key != ""
	if !hasRSA && !hasMD5 {
		return nil
	}

	if hasRSA {
		v2, err := epay.NewClient(&epay.Config{
			Version:            epay.VersionV2,
			BaseURL:            settings.PayAddress,
			PID:                settings.MerchantID,
			PlatformPublicKey:  settings.PlatformPublicKey,
			MerchantPrivateKey: settings.MerchantPrivateKey,
		})
		if err != nil {
			// RSA 密钥都解析不了：有 MD5 就直接测 MD5，否则回报 RSA 配置错误。
			if hasMD5 {
				return probeEpayV1WithNote(settings, "RSA 密钥无效（"+err.Error()+"），已改用 MD5(v1) 检测")
			}
			return &epay.CapabilityReport{Version: epay.VersionV2, Summary: "RSA 密钥无效：" + err.Error()}
		}
		report := v2.ProbeCapabilities()
		if report.Reachable && report.CredentialsValid {
			return report // v2 可用，直接采用
		}
		// v2 不可用，且有 MD5 兜底 → 再测 v1，用得上就改推 MD5。
		if hasMD5 {
			return probeEpayV1WithNote(settings, "v2(RSA) 接口不可用（"+report.Summary+"）；已自动改用 MD5(v1) 检测")
		}
		return report // 无 MD5 兜底：返回 v2 结论（其中已含「改用 MD5」的建议）
	}

	// 仅配置了 MD5。
	return probeEpayV1WithNote(settings, "")
}

// probeEpayV1WithNote 用 MD5(v1) 凭证探测能力；note 非空时作为前缀补进结论，
// 说明为何走到 v1（RSA 不可用而自动降级等）。
func probeEpayV1WithNote(settings operation_setting.EpaySettingSnapshot, note string) *epay.CapabilityReport {
	client, err := epay.NewClient(&epay.Config{
		Version: epay.VersionV1,
		BaseURL: settings.PayAddress,
		PID:     settings.MerchantID,
		Key:     settings.MD5Key,
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
