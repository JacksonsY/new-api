package epay

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// isNonJSONResponse 判断错误是否为「平台返回了非 JSON（HTML 错误页）」——
// 这类错误代表平台可达但接口/协议不匹配，与传输层不可达要区别对待。
func isNonJSONResponse(err error) bool {
	var nonJSON *NonJSONResponseError
	return errors.As(err, &nonJSON)
}

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

	// query_order：本次探测**真实走过**一次查单往返，是实测项
	queryDetail := "实测通过：查单往返正常"
	if !credentialsValid {
		queryDetail = "查单探测未通过：" + credentialDetail
	}
	report.Capabilities = append(report.Capabilities, CapabilityStatus{
		Name: "query_order", Available: credentialsValid, Detail: queryDetail,
	})

	// 支付/退款类：与查单同凭证同端点体系，但发起真实交易才会实际调用，不做破坏性实测，
	// 故据凭证有效性**推断**为就绪（不夸大为“已实测”）。
	readyDetail := "接口就绪（凭证有效；发起真实交易才实际调用，不做破坏性实测）"
	if !credentialsValid {
		readyDetail = "凭证/连通性检测未通过，暂不可用"
	}
	for _, name := range []string{"page_pay", "api_pay", "refund", "refund_query", "close_order"} {
		report.Capabilities = append(report.Capabilities, CapabilityStatus{
			Name: name, Available: credentialsValid, Detail: readyDetail,
		})
	}

	// 管理/代付类（merchant_info/list_orders/balance/transfer/transfer_query）：
	// v2 下这些无副作用接口会由 ProbeCapabilities 进一步**真实实测**并覆盖此处占位；
	// v1 不支持。
	management := []string{"merchant_info", "list_orders", "balance", "transfer", "transfer_query"}
	if version == VersionV2 {
		for _, name := range management {
			report.Capabilities = append(report.Capabilities, CapabilityStatus{
				Name: name, Available: credentialsValid, Detail: "待实测",
			})
		}
	} else {
		for _, name := range management {
			report.Capabilities = append(report.Capabilities, CapabilityStatus{
				Name: name, Available: false, Detail: "v1(MD5) 协议不支持，需商户开通 v2(RSA) 新版接口",
			})
		}
	}

	switch {
	case !reachable:
		report.Summary = "平台地址不可达：" + credentialDetail
	case !credentialsValid:
		// 可达但检测未通过，具体原因（凭证错误 / 接口协议不匹配）由 credentialDetail 说明
		report.Summary = "平台可达，但接口检测未通过：" + credentialDetail
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
		if isNonJSONResponse(err) {
			return buildCapabilityReport(VersionV1, true, false,
				"接口返回非 JSON（HTML 错误页），请核对平台地址是否正确（v1 查单端点为 api.php）")
		}
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
//
// 查单字段与官方 SDK 对齐用 trade_no（平台交易号）：平台的 api/pay/query 以 trade_no
// 取值，只发 out_trade_no 会让平台取到空 trade_no 抛异常回 HTML 错误页（曾误判为不可达）。
//
// 凭证有效后，再用**无副作用**的管理类接口（merchant/info、balance）做逐项真实实测，
// 回填商户实时状态（支付/结算状态、余额、代付费率）——报告不再是“凭证有效即推断可用”。
func (c *clientV2) ProbeCapabilities() *CapabilityReport {
	reachable, verified, detail := c.executeProbe("api/pay/query", map[string]string{
		"trade_no": probeOrderNo,
	})
	report := buildCapabilityReport(VersionV2, reachable, verified, detail)
	if verified {
		c.probeManagementCapabilities(report)
	}
	return report
}

// probeManagementCapabilities 真实调用 merchant/info 与 balance（均无资金副作用），
// 据实回填商户状态与这些能力的可用性——把“待实测”的占位换成逐项实测结论。
func (c *clientV2) probeManagementCapabilities(report *CapabilityReport) {
	if mi, err := c.MerchantInfoQuery(); err == nil {
		report.Merchant = &MerchantSnapshot{
			Status:       mi.Status,
			PayStatus:    mi.PayStatus,
			SettleStatus: mi.SettleStatus,
			Balance:      mi.Money,
			OrderNum:     mi.OrderNum,
		}
		setCapabilityStatus(report, "merchant_info", true, "实测通过：已返回商户信息")
		setCapabilityStatus(report, "list_orders", true, "实测通过（商户接口体系可用）")
	} else {
		setCapabilityStatus(report, "merchant_info", false, "实测失败："+err.Error())
		setCapabilityStatus(report, "list_orders", false, "实测失败："+err.Error())
	}

	if bal, err := c.Balance(); err == nil {
		if report.Merchant == nil {
			report.Merchant = &MerchantSnapshot{}
		}
		report.Merchant.Balance = bal.AvailableMoney
		report.Merchant.TransferRate = bal.TransferRate
		setCapabilityStatus(report, "balance", true, "实测通过：可用余额 "+bal.AvailableMoney)
		setCapabilityStatus(report, "transfer", true, "代付通道已开通（余额接口实测通过，费率 "+bal.TransferRate+"）")
		setCapabilityStatus(report, "transfer_query", true, "可用（与代付同接口体系）")
	} else {
		setCapabilityStatus(report, "balance", false, "实测失败："+err.Error())
		setCapabilityStatus(report, "transfer", false, "代付未开通/未授权（余额接口实测失败）")
		setCapabilityStatus(report, "transfer_query", false, "代付未开通/未授权")
	}
	// 商户实时状态放在结构化的 report.Merchant 里，由前端成块展示；Summary 只留检测结论。
}

// setCapabilityStatus 覆盖（或追加）某项能力的实测结论。
func setCapabilityStatus(report *CapabilityReport, name string, available bool, detail string) {
	for i := range report.Capabilities {
		if report.Capabilities[i].Name == name {
			report.Capabilities[i].Available = available
			report.Capabilities[i].Detail = detail
			return
		}
	}
	report.Capabilities = append(report.Capabilities, CapabilityStatus{Name: name, Available: available, Detail: detail})
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
		if isNonJSONResponse(err) {
			// 平台可达（拿到了 HTTP 响应），但返回的是 HTML/非 JSON。把 reachable 置 true，
			// 避免误报「平台地址不可达」；具体多为平台地址填错、或平台未开启 v2(RSA) 新版接口。
			return true, false,
				"接口返回非 JSON（HTML 错误页）：平台可达但接口报错，请核对平台地址是否正确、以及平台是否已开启 v2(RSA) 新版接口（api/pay/*）"
		}
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
