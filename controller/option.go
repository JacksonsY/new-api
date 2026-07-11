package controller

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

var completionRatioMetaOptionKeys = []string{
	"ModelPrice",
	"ModelRatio",
	"CompletionRatio",
	"CacheRatio",
	"CreateCacheRatio",
	"ImageRatio",
	"AudioRatio",
	"AudioCompletionRatio",
}

func isPaymentComplianceOptionKey(key string) bool {
	return strings.HasPrefix(key, "payment_setting.compliance_")
}

func isPositiveOptionValue(value string) bool {
	intValue, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil {
		return intValue > 0
	}
	floatValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && floatValue > 0
}

func collectModelNamesFromOptionValue(raw string, modelNames map[string]struct{}) {
	if strings.TrimSpace(raw) == "" {
		return
	}

	var parsed map[string]any
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return
	}

	for modelName := range parsed {
		modelNames[modelName] = struct{}{}
	}
}

func buildCompletionRatioMetaValue(optionValues map[string]string) string {
	modelNames := make(map[string]struct{})
	for _, key := range completionRatioMetaOptionKeys {
		collectModelNamesFromOptionValue(optionValues[key], modelNames)
	}

	meta := make(map[string]ratio_setting.CompletionRatioInfo, len(modelNames))
	for modelName := range modelNames {
		meta[modelName] = ratio_setting.GetCompletionRatioInfo(modelName)
	}

	jsonBytes, err := common.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func GetOptions(c *gin.Context) {
	var options []*model.Option
	optionValues := make(map[string]string)
	common.OptionMapRWMutex.Lock()
	for k, v := range common.OptionMap {
		value := common.Interface2String(v)
		isSensitiveKey := strings.HasSuffix(k, "Token") ||
			strings.HasSuffix(k, "Secret") ||
			strings.HasSuffix(k, "Key") ||
			strings.HasSuffix(k, "secret") ||
			strings.HasSuffix(k, "api_key")
		if isSensitiveKey {
			continue
		}
		options = append(options, &model.Option{
			Key:   k,
			Value: value,
		})
		for _, optionKey := range completionRatioMetaOptionKeys {
			if optionKey == k {
				optionValues[k] = value
				break
			}
		}
	}
	common.OptionMapRWMutex.Unlock()
	options = append(options, &model.Option{
		Key:   "CompletionRatioMeta",
		Value: buildCompletionRatioMetaValue(optionValues),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    options,
	})
}

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type OptionBulkUpdateRequest struct {
	Options []OptionUpdateRequest `json:"options"`
}

var paymentOptionKeys = map[string]struct{}{
	"PayAddress": {}, "CustomCallbackAddress": {}, "EpayId": {}, "EpayKey": {},
	"EpayApiVersion": {}, "EpayPlatformPublicKey": {}, "EpayMerchantPrivateKey": {},
	"Price": {}, "MinTopUp": {}, "PayMethods": {},
	"payment_setting.amount_options": {}, "payment_setting.amount_discount": {},
	"StripeApiSecret": {}, "StripeWebhookSecret": {}, "StripePriceId": {},
	"StripeUnitPrice": {}, "StripeMinTopUp": {}, "StripePromotionCodesEnabled": {},
	"CreemApiKey": {}, "CreemWebhookSecret": {}, "CreemTestMode": {}, "CreemProducts": {},
	"WaffoEnabled": {}, "WaffoApiKey": {}, "WaffoPrivateKey": {}, "WaffoPublicCert": {},
	"WaffoSandboxPublicCert": {}, "WaffoSandboxApiKey": {}, "WaffoSandboxPrivateKey": {},
	"WaffoSandbox": {}, "WaffoMerchantId": {}, "WaffoNotifyUrl": {}, "WaffoReturnUrl": {},
	"WaffoCurrency": {}, "WaffoUnitPrice": {}, "WaffoMinTopUp": {}, "WaffoPayMethods": {},
}

func UpdatePaymentOptions(c *gin.Context) {
	var request OptionBulkUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Options) == 0 || len(request.Options) > 64 {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	values := make(map[string]string, len(request.Options))
	for _, option := range request.Options {
		if _, allowed := paymentOptionKeys[option.Key]; !allowed {
			common.ApiErrorMsg(c, "包含不允许批量修改的配置项")
			return
		}
		if _, duplicate := values[option.Key]; duplicate {
			common.ApiErrorMsg(c, "包含重复的配置项")
			return
		}
		values[option.Key] = common.Interface2String(option.Value)
	}
	if err := validatePaymentOptionValues(values); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}
	for key := range values {
		recordManageAudit(c, "option.update", map[string]interface{}{"key": key})
	}
	common.ApiSuccess(c, nil)
}

func validatePaymentOptionValues(values map[string]string) error {
	if version, ok := values["EpayApiVersion"]; ok && version != "v1" && version != "v2" {
		return fmt.Errorf("易支付协议版本无效")
	}
	for _, numericKey := range []string{"Price", "StripeUnitPrice", "WaffoUnitPrice"} {
		if value, ok := values[numericKey]; ok {
			number, parseErr := strconv.ParseFloat(value, 64)
			if parseErr != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
				return fmt.Errorf("支付配置数值无效")
			}
		}
	}
	for _, integerKey := range []string{"MinTopUp", "StripeMinTopUp", "WaffoMinTopUp"} {
		if value, ok := values[integerKey]; ok {
			number, parseErr := strconv.Atoi(value)
			if parseErr != nil || number < 0 {
				return fmt.Errorf("支付配置数值无效")
			}
		}
	}
	for _, booleanKey := range []string{"StripePromotionCodesEnabled", "CreemTestMode", "WaffoEnabled", "WaffoSandbox"} {
		if value, ok := values[booleanKey]; ok && value != "true" && value != "false" {
			return fmt.Errorf("支付配置布尔值无效")
		}
	}
	if value, ok := values["PayMethods"]; ok {
		var methods []map[string]string
		if err := common.UnmarshalJsonStr(value, &methods); err != nil {
			return fmt.Errorf("支付方式配置无效")
		}
		for _, method := range methods {
			if strings.TrimSpace(method["name"]) == "" || strings.TrimSpace(method["type"]) == "" {
				return fmt.Errorf("支付方式配置无效")
			}
			if minTopUp := strings.TrimSpace(method["min_topup"]); minTopUp != "" {
				amount, err := strconv.Atoi(minTopUp)
				if err != nil || amount < 0 {
					return fmt.Errorf("支付方式配置无效")
				}
			}
		}
	}
	if value, ok := values["payment_setting.amount_options"]; ok {
		var amounts []int
		if err := common.UnmarshalJsonStr(value, &amounts); err != nil {
			return fmt.Errorf("充值金额选项无效")
		}
		for _, amount := range amounts {
			if amount <= 0 {
				return fmt.Errorf("充值金额选项无效")
			}
		}
	}
	if value, ok := values["payment_setting.amount_discount"]; ok {
		var discounts map[int]float64
		if err := common.UnmarshalJsonStr(value, &discounts); err != nil {
			return fmt.Errorf("充值折扣配置无效")
		}
		for amount, discount := range discounts {
			if amount <= 0 || math.IsNaN(discount) || math.IsInf(discount, 0) || discount <= 0 || discount > 1 {
				return fmt.Errorf("充值折扣配置无效")
			}
		}
	}
	if value, ok := values["CreemProducts"]; ok {
		var products []CreemProduct
		if err := common.UnmarshalJsonStr(value, &products); err != nil {
			return fmt.Errorf("Creem 产品配置无效")
		}
		seenProductIDs := make(map[string]struct{}, len(products))
		for _, product := range products {
			productID := strings.TrimSpace(product.ProductId)
			if productID == "" || strings.TrimSpace(product.Name) == "" || strings.TrimSpace(product.Currency) == "" ||
				math.IsNaN(product.Price) || math.IsInf(product.Price, 0) || product.Price <= 0 || product.Quota <= 0 {
				return fmt.Errorf("Creem 产品配置无效")
			}
			if _, duplicate := seenProductIDs[productID]; duplicate {
				return fmt.Errorf("Creem 产品配置无效")
			}
			seenProductIDs[productID] = struct{}{}
		}
	}
	if value, ok := values["WaffoPayMethods"]; ok {
		var methods []struct {
			Name          string `json:"name"`
			Icon          string `json:"icon"`
			PayMethodType string `json:"payMethodType"`
			PayMethodName string `json:"payMethodName"`
		}
		if err := common.UnmarshalJsonStr(value, &methods); err != nil {
			return fmt.Errorf("Waffo 支付方式配置无效")
		}
		for _, method := range methods {
			if strings.TrimSpace(method.Name) == "" {
				return fmt.Errorf("Waffo 支付方式配置无效")
			}
		}
	}
	return nil
}

func UpdateOption(c *gin.Context) {
	var option OptionUpdateRequest
	err := common.DecodeJson(c.Request.Body, &option)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	switch option.Value.(type) {
	case bool:
		option.Value = common.Interface2String(option.Value.(bool))
	case float64:
		option.Value = common.Interface2String(option.Value.(float64))
	case int:
		option.Value = common.Interface2String(option.Value.(int))
	default:
		option.Value = fmt.Sprintf("%v", option.Value)
	}
	switch option.Key {
	case "QuotaForInviter", "QuotaForInvitee":
		if isPositiveOptionValue(option.Value.(string)) && !operation_setting.IsPaymentComplianceConfirmed() {
			common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
			return
		}
	default:
		if isPaymentComplianceOptionKey(option.Key) {
			common.ApiErrorMsg(c, "合规确认字段不允许通过通用设置接口修改")
			return
		}
	}
	switch option.Key {
	case "GitHubOAuthEnabled":
		if option.Value == "true" && common.GitHubClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 GitHub OAuth，请先填入 GitHub Client Id 以及 GitHub Client Secret！",
			})
			return
		}
	case "discord.enabled":
		if option.Value == "true" && system_setting.GetDiscordSettings().ClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 Discord OAuth，请先填入 Discord Client Id 以及 Discord Client Secret！",
			})
			return
		}
	case "oidc.enabled":
		if option.Value == "true" && system_setting.GetOIDCSettings().ClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 OIDC 登录，请先填入 OIDC Client Id 以及 OIDC Client Secret！",
			})
			return
		}
	case "LinuxDOOAuthEnabled":
		if option.Value == "true" && common.LinuxDOClientId == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 LinuxDO OAuth，请先填入 LinuxDO Client Id 以及 LinuxDO Client Secret！",
			})
			return
		}
	case "EmailDomainRestrictionEnabled":
		if option.Value == "true" && len(common.EmailDomainWhitelist) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用邮箱域名限制，请先填入限制的邮箱域名！",
			})
			return
		}
	case "WeChatAuthEnabled":
		if option.Value == "true" && common.WeChatServerAddress == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用微信登录，请先填入微信登录相关配置信息！",
			})
			return
		}
	case "GlobalProxyUrl":
		if trimmed := strings.TrimSpace(option.Value.(string)); trimmed != "" {
			parsedURL, err := url.Parse(trimmed)
			if err != nil || parsedURL.Host == "" ||
				(parsedURL.Scheme != "http" && parsedURL.Scheme != "https" &&
					parsedURL.Scheme != "socks5" && parsedURL.Scheme != "socks5h") {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "全局代理地址无效，仅支持 http://、https://、socks5:// 或 socks5h:// 协议",
				})
				return
			}
		}
	case "TurnstileCheckEnabled":
		if option.Value == "true" && common.TurnstileSiteKey == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 Turnstile 校验，请先填入 Turnstile 校验相关配置信息！",
			})

			return
		}
	case "TelegramOAuthEnabled":
		if option.Value == "true" && common.TelegramBotToken == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法启用 Telegram OAuth，请先填入 Telegram Bot Token！",
			})
			return
		}
	case "theme.frontend":
		if option.Value != "default" && option.Value != "classic" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无效的主题值，可选值：default（新版前端）、classic（经典前端）",
			})
			return
		}
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "图片倍率设置失败: " + err.Error(),
			})
			return
		}
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "音频倍率设置失败: " + err.Error(),
			})
			return
		}
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "音频补全倍率设置失败: " + err.Error(),
			})
			return
		}
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "缓存创建倍率设置失败: " + err.Error(),
			})
			return
		}
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticDisableStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.api_info":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "ApiInfo")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.announcements":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "Announcements")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.faq":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "FAQ")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "console_setting.uptime_kuma_groups":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "UptimeKumaGroups")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	err = model.UpdateOption(option.Key, option.Value.(string))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 出于安全考虑只记录被修改的配置项名称，不记录配置值（可能含密钥等敏感信息）。
	recordManageAudit(c, "option.update", map[string]interface{}{
		"key": option.Key,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
