package epay

import (
	"net/url"
	"strconv"
	"strings"
)

// 能力检测的设计：绝不产生真实订单/资金副作用。用"查一个几乎不可能存在的随机商户单号"
// 做一次真实往返——平台会回"订单不存在"这类业务错误,但这恰好证明了：
//   - 平台地址可达（拿到 HTTP 响应）
//   - 商户凭证/签名被平台接受（v1 端点应答正常 / v2 响应验签通过）
//   - 查单端点存在
// 页面支付 / API 直付 / 退款走同一套端点与凭证,凭证有效即视为可用（这些接口无法在
// 不产生真实订单的前提下单独探测,故基于凭证有效性推断,并在 Detail 里说明）。
// 回调验签是纯本地能力（有密钥即可）,直接判定可用。

// probeOrderNo 探测用的商户单号：一个高概率不存在的固定单号，
// 查它只会得到"订单不存在"这类业务响应，绝无资金副作用。
const probeOrderNo = "PROBE-NOEXIST-CHECK"

func buildCapabilityReport(version string, reachable, credentialsValid bool, credentialDetail string) *CapabilityReport {
	report := &CapabilityReport{
		Version:          version,
		Reachable:        reachable,
		CredentialsValid: credentialsValid,
	}

	verifyDetail := "本地能力：已配置密钥即可验签回调"
	if version == VersionV2 {
		verifyDetail = "本地能力：已配置平台公钥即可验签回调"
	}
	report.Capabilities = append(report.Capabilities, CapabilityStatus{
		Name: "verify_notify", Available: credentialsValid, Detail: verifyDetail,
	})

	// 与查单同端点体系、同凭证的接口，凭证有效即视为可用
	sharedDetail := "凭证有效，与查单同端点体系，预期可用"
	if !credentialsValid {
		sharedDetail = "凭证/连通性检测未通过，暂不可用"
	}
	for _, name := range []string{"page_pay", "api_pay", "query_order", "refund"} {
		report.Capabilities = append(report.Capabilities, CapabilityStatus{
			Name: name, Available: credentialsValid, Detail: sharedDetail,
		})
	}

	switch {
	case !reachable:
		report.Summary = "平台地址不可达：" + credentialDetail
	case !credentialsValid:
		report.Summary = "平台可达，但凭证/签名校验未通过：" + credentialDetail
	default:
		report.Summary = "检测通过：平台可达、凭证有效，各接口预期可用"
	}
	return report
}

// ProbeCapabilities v1：查一个随机不存在订单号。能拿到结构化 JSON 响应（有 code 字段）
// 即证明平台可达 + pid/端点有效；v1 查单不校验响应签名，故凭证有效性以"端点正常应答"为准。
func (c *clientV1) ProbeCapabilities() *CapabilityReport {
	query := url.Values{}
	query.Set("act", "order")
	query.Set("pid", c.pid)
	query.Set("key", c.key)
	query.Set("out_trade_no", probeOrderNo)
	raw, err := httpGetJSON(joinURL(c.baseURL, "api.php") + "?" + query.Encode())
	if err != nil {
		return buildCapabilityReport(VersionV1, false, false, err.Error())
	}
	// 拿到 JSON 即可达。判断 key 是否被接受：平台对 key 错误通常回明确的签名/权限错误文案。
	msg := strings.ToLower(fieldString(raw, "msg"))
	if _, hasCode := raw["code"]; !hasCode {
		return buildCapabilityReport(VersionV1, true, false, "平台响应缺少 code 字段，端点可能不匹配")
	}
	if strings.Contains(msg, "sign") || strings.Contains(msg, "签名") ||
		strings.Contains(msg, "key") || strings.Contains(msg, "密钥") ||
		strings.Contains(msg, "pid") || strings.Contains(msg, "商户") {
		return buildCapabilityReport(VersionV1, true, false, "平台提示凭证错误："+fieldString(raw, "msg"))
	}
	return buildCapabilityReport(VersionV1, true, true, "查单端点正常应答")
}

// ProbeCapabilities v2：查一个随机不存在订单号。execute 会对成功响应验签；
// 订单不存在时平台回非零 code（execute 报错但那是业务错误,不代表凭证无效）。
// 因此用 executeProbe 拿到"验签是否通过"的确切信号来判断凭证有效性。
func (c *clientV2) ProbeCapabilities() *CapabilityReport {
	reachable, verified, detail := c.executeProbe("api/pay/query", map[string]string{
		"out_trade_no": probeOrderNo,
	})
	return buildCapabilityReport(VersionV2, reachable, verified, detail)
}

// executeProbe 是 execute 的探测版：不因业务错误码报错,只回 (可达, 验签通过, 说明)。
// 验签通过 => 平台公钥与商户签名匹配 => 凭证有效（哪怕订单不存在）。
func (c *clientV2) executeProbe(subPath string, params map[string]string) (reachable bool, verified bool, detail string) {
	signed, err := c.buildSignedParams(params)
	if err != nil {
		return false, false, "签名失败：" + err.Error()
	}
	form := url.Values{}
	for key, value := range signed {
		form.Set(key, value)
	}
	raw, err := httpPostFormJSON(joinURL(c.baseURL, subPath), form)
	if err != nil {
		return false, false, err.Error()
	}
	// 有响应即可达。若带 sign 字段则验签：通过=凭证有效；不通过=平台公钥或签名配置错。
	if _, hasSign := raw["sign"]; hasSign {
		if c.verifySignedParams(rawToSignParams(raw)) {
			return true, true, "响应验签通过（订单不存在属正常业务响应）"
		}
		return true, false, "响应验签失败：平台公钥或商户私钥配置错误"
	}
	// 平台对错误请求可能不带签名回错误码；据 code/msg 粗判凭证问题
	code := fieldInt(raw, "code", -1)
	msg := strings.ToLower(fieldString(raw, "msg"))
	if code != 0 && (strings.Contains(msg, "sign") || strings.Contains(msg, "签名") ||
		strings.Contains(msg, "pid") || strings.Contains(msg, "商户") || strings.Contains(msg, "key")) {
		return true, false, "平台提示凭证错误：" + fieldString(raw, "msg")
	}
	// 无签名、非凭证类错误（如"订单不存在"）：可达,凭证大概率有效但无法经验签确认
	return true, true, "平台可达（响应未带签名，无法二次验签，凭证初判有效，code=" + strconv.Itoa(code) + "）"
}
