package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/shopspring/decimal"

	"github.com/gin-gonic/gin"
)

// https://github.com/songquanpeng/one-api/issues/79

type OpenAISubscriptionResponse struct {
	Object             string  `json:"object"`
	HasPaymentMethod   bool    `json:"has_payment_method"`
	SoftLimitUSD       float64 `json:"soft_limit_usd"`
	HardLimitUSD       float64 `json:"hard_limit_usd"`
	SystemHardLimitUSD float64 `json:"system_hard_limit_usd"`
	AccessUntil        int64   `json:"access_until"`
}

type OpenAIUsageDailyCost struct {
	Timestamp float64 `json:"timestamp"`
	LineItems []struct {
		Name string  `json:"name"`
		Cost float64 `json:"cost"`
	}
}

type OpenAICreditGrants struct {
	Object         string  `json:"object"`
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
	TotalAvailable float64 `json:"total_available"`
}

type OpenAIUsageResponse struct {
	Object string `json:"object"`
	//DailyCosts []OpenAIUsageDailyCost `json:"daily_costs"`
	TotalUsage float64 `json:"total_usage"` // unit: 0.01 dollar
}

type OpenAISBUsageResponse struct {
	Msg  string `json:"msg"`
	Data *struct {
		Credit string `json:"credit"`
	} `json:"data"`
}

type AIProxyUserOverviewResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	ErrorCode int    `json:"error_code"`
	Data      struct {
		TotalPoints float64 `json:"totalPoints"`
	} `json:"data"`
}

type API2GPTUsageResponse struct {
	Object         string  `json:"object"`
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
	TotalRemaining float64 `json:"total_remaining"`
}

type APGC2DGPTUsageResponse struct {
	//Grants         interface{} `json:"grants"`
	Object         string  `json:"object"`
	TotalAvailable float64 `json:"total_available"`
	TotalGranted   float64 `json:"total_granted"`
	TotalUsed      float64 `json:"total_used"`
}

type SiliconFlowUsageResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  bool   `json:"status"`
	Data    struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Image         string `json:"image"`
		Email         string `json:"email"`
		IsAdmin       bool   `json:"isAdmin"`
		Balance       string `json:"balance"`
		Status        string `json:"status"`
		Introduction  string `json:"introduction"`
		Role          string `json:"role"`
		ChargeBalance string `json:"chargeBalance"`
		TotalBalance  string `json:"totalBalance"`
		Category      string `json:"category"`
	} `json:"data"`
}

type DeepSeekUsageResponse struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}

type OpenRouterCreditResponse struct {
	Data struct {
		TotalCredits float64 `json:"total_credits"`
		TotalUsage   float64 `json:"total_usage"`
	} `json:"data"`
}

// Sub2APIUsageCostBucket sub2api /v1/usage 里 usage.today / usage.total 的成本摘要。
// cost 是表价、actual_cost 是按 Key 所在分组倍率实扣（源码契约：
// ActualCost = TotalCost × RateMultiplier），因此 actual_cost/cost 即有效倍率。
type Sub2APIUsageCostBucket struct {
	Cost       float64 `json:"cost"`
	ActualCost float64 `json:"actual_cost"`
}

// Sub2APIUsageResponse 对应 sub2api 上游 GET /v1/usage 的响应。
// 该端点用渠道 Key（Bearer）鉴权，即便 Key 过期/额度耗尽也允许查询自身用量。
// remaining（USD）在 quota_limited / 订阅 / 钱包三种模式下均存在，是渠道余额。
type Sub2APIUsageResponse struct {
	Mode      string   `json:"mode"`
	IsValid   bool     `json:"isValid"`
	Remaining *float64 `json:"remaining"`
	Balance   *float64 `json:"balance"`
	Unit      string   `json:"unit"`
	Quota     *struct {
		Limit     float64 `json:"limit"`
		Used      float64 `json:"used"`
		Remaining float64 `json:"remaining"`
		Unit      string  `json:"unit"`
	} `json:"quota"`
	Usage *struct {
		Today *Sub2APIUsageCostBucket `json:"today"`
		Total *Sub2APIUsageCostBucket `json:"total"`
	} `json:"usage"`
}

// Sub2APIAdminGroup 上游分组倍率的展示行（前端「查看上游分组倍率」表格的行契约）。
// new-api 上游来自公开 pricing 的分组表；sub2api 上游是客户视角，没有分组列表
// 端点，返回一行"本渠道 Key 的有效倍率"（用量推导）。
type Sub2APIAdminGroup struct {
	ID                  int64   `json:"id"`
	Name                string  `json:"name"`
	Platform            string  `json:"platform"`
	Status              string  `json:"status"`
	RateMultiplier      float64 `json:"rate_multiplier"`
	ImageRateMultiplier float64 `json:"image_rate_multiplier"`
	PeakRateEnabled     bool    `json:"peak_rate_enabled"`
	PeakRateMultiplier  float64 `json:"peak_rate_multiplier"`
	IsExclusive         bool    `json:"is_exclusive"`
	SubscriptionType    string  `json:"subscription_type"`
}

// GetAuthHeader get auth header
func GetAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	return h
}

// GetClaudeAuthHeader get claude auth header
func GetClaudeAuthHeader(token string) http.Header {
	h := http.Header{}
	h.Add("x-api-key", token)
	h.Add("anthropic-version", "2023-06-01")
	return h
}

func GetResponseBody(method, url string, channel *model.Channel, headers http.Header) ([]byte, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	for k := range headers {
		req.Header.Add(k, headers.Get(k))
	}
	client, err := service.NewProxyHttpClient(channel.GetSetting().Proxy)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code: %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	err = res.Body.Close()
	if err != nil {
		return nil, err
	}
	return body, nil
}

func updateChannelCloseAIBalance(channel *model.Channel) (float64, error) {
	url := fmt.Sprintf("%s/dashboard/billing/credit_grants", channel.GetBaseURL())
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))

	if err != nil {
		return 0, err
	}
	response := OpenAICreditGrants{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(response.TotalAvailable)
	return response.TotalAvailable, nil
}

func updateChannelOpenAISBBalance(channel *model.Channel) (float64, error) {
	url := fmt.Sprintf("https://api.openai-sb.com/sb-api/user/status?api_key=%s", channel.Key)
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := OpenAISBUsageResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	if response.Data == nil {
		return 0, errors.New(response.Msg)
	}
	balance, err := strconv.ParseFloat(response.Data.Credit, 64)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

func updateChannelAIProxyBalance(channel *model.Channel) (float64, error) {
	url := "https://aiproxy.io/api/report/getUserOverview"
	headers := http.Header{}
	headers.Add("Api-Key", channel.Key)
	body, err := GetResponseBody("GET", url, channel, headers)
	if err != nil {
		return 0, err
	}
	response := AIProxyUserOverviewResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	if !response.Success {
		return 0, fmt.Errorf("code: %d, message: %s", response.ErrorCode, response.Message)
	}
	channel.UpdateBalance(response.Data.TotalPoints)
	return response.Data.TotalPoints, nil
}

func updateChannelAPI2GPTBalance(channel *model.Channel) (float64, error) {
	url := "https://api.api2gpt.com/dashboard/billing/credit_grants"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))

	if err != nil {
		return 0, err
	}
	response := API2GPTUsageResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(response.TotalRemaining)
	return response.TotalRemaining, nil
}

func updateChannelSiliconFlowBalance(channel *model.Channel) (float64, error) {
	url := "https://api.siliconflow.cn/v1/user/info"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := SiliconFlowUsageResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	if response.Code != 20000 {
		return 0, fmt.Errorf("code: %d, message: %s", response.Code, response.Message)
	}
	balance, err := strconv.ParseFloat(response.Data.TotalBalance, 64)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

func updateChannelDeepSeekBalance(channel *model.Channel) (float64, error) {
	url := "https://api.deepseek.com/user/balance"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := DeepSeekUsageResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	index := -1
	for i, balanceInfo := range response.BalanceInfos {
		if balanceInfo.Currency == "CNY" {
			index = i
			break
		}
	}
	if index == -1 {
		return 0, errors.New("currency CNY not found")
	}
	balance, err := strconv.ParseFloat(response.BalanceInfos[index].TotalBalance, 64)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(balance)
	return balance, nil
}

func updateChannelAIGC2DBalance(channel *model.Channel) (float64, error) {
	url := "https://api.aigc2d.com/dashboard/billing/credit_grants"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := APGC2DGPTUsageResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	channel.UpdateBalance(response.TotalAvailable)
	return response.TotalAvailable, nil
}

func updateChannelOpenRouterBalance(channel *model.Channel) (float64, error) {
	url := "https://openrouter.ai/api/v1/credits"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := OpenRouterCreditResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	balance := response.Data.TotalCredits - response.Data.TotalUsage
	channel.UpdateBalance(balance)
	return balance, nil
}

func updateChannelMoonshotBalance(channel *model.Channel) (float64, error) {
	url := "https://api.moonshot.cn/v1/users/me/balance"
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}

	type MoonshotBalanceData struct {
		AvailableBalance float64 `json:"available_balance"`
		VoucherBalance   float64 `json:"voucher_balance"`
		CashBalance      float64 `json:"cash_balance"`
	}

	type MoonshotBalanceResponse struct {
		Code   int                 `json:"code"`
		Data   MoonshotBalanceData `json:"data"`
		Scode  string              `json:"scode"`
		Status bool                `json:"status"`
	}

	response := MoonshotBalanceResponse{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return 0, err
	}
	if !response.Status || response.Code != 0 {
		return 0, fmt.Errorf("failed to update moonshot balance, status: %v, code: %d, scode: %s", response.Status, response.Code, response.Scode)
	}
	availableBalanceCny := response.Data.AvailableBalance
	// moonshot 报真人民币。按原口径除以充值售价 Price（≈本站单位的人民币面值，
	// 1:1 站 Price=1 → 原样入库），仅补除零兜底——Price 可为 0，历史上会得 +Inf。
	divisor := operation_setting.Price
	if divisor <= 0 {
		divisor = usdExchangeRateOrDefault()
	}
	availableBalanceUsd := decimal.NewFromFloat(availableBalanceCny).Div(decimal.NewFromFloat(divisor)).InexactFloat64()
	channel.UpdateBalance(availableBalanceUsd)
	return availableBalanceUsd, nil
}

// usdExchangeRateOrDefault 返回 CNY/USD 汇率，未配置(<=0)时退回默认 7.3。
// 仅供真人民币供应商（moonshot）换算时的除零兜底链使用。
func usdExchangeRateOrDefault() float64 {
	rate := operation_setting.USDExchangeRate
	if rate <= 0 {
		return 7.3
	}
	return rate
}

// updateChannelSub2APIBalance 通过 sub2api 上游的 /v1/usage 端点查询余额。
// 用渠道 Key 以 Bearer 鉴权，读取 remaining 作为渠道余额；
// remaining 缺失时回退到 balance / quota.remaining。
// 上游报的数字与本站单位同构（同为名义美元额度单位），原样入库不做币种换算
// ——蓝图B 的探测式 CNY 归一评估后不适用本生态，已回退（见图鉴 §十一）。
func updateChannelSub2APIBalance(channel *model.Channel) (float64, error) {
	url := fmt.Sprintf("%s/v1/usage", channel.GetBaseURL())
	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	response := Sub2APIUsageResponse{}
	if err = common.Unmarshal(body, &response); err != nil {
		return 0, err
	}
	// 倍率自动同步搭同一响应推导（actual_cost/cost），零额外请求
	maybeSyncSub2apiDerivedRatio(channel, &response)
	switch {
	case response.Remaining != nil:
		channel.UpdateBalance(*response.Remaining)
		return *response.Remaining, nil
	case response.Balance != nil:
		channel.UpdateBalance(*response.Balance)
		return *response.Balance, nil
	case response.Quota != nil:
		channel.UpdateBalance(response.Quota.Remaining)
		return response.Quota.Remaining, nil
	default:
		return 0, errors.New("sub2api /v1/usage 未返回余额字段（remaining/balance/quota）")
	}
}

// updateChannelBalance 更新渠道余额；成功后搭车自动同步成本倍率（开关
// upstream_ratio_sync）。sub2api 渠道在余额响应里就地推导（见
// maybeSyncSub2apiDerivedRatio），这里只处理 new-api 上游的 pricing 路径。
// 手动单渠道与全量定时两条调用路径都经此收口。
func updateChannelBalance(channel *model.Channel) (float64, error) {
	balance, err := updateChannelBalanceInner(channel)
	if err == nil && !channel.GetSetting().Sub2ApiBalanceQuery {
		maybeSyncUpstreamGroupRatio(channel)
	}
	return balance, err
}

func updateChannelBalanceInner(channel *model.Channel) (float64, error) {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() == "" {
		channel.BaseURL = &baseURL
	}
	if channel.GetSetting().Sub2ApiBalanceQuery {
		return updateChannelSub2APIBalance(channel)
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI:
		if channel.GetBaseURL() != "" {
			baseURL = channel.GetBaseURL()
		}
	case constant.ChannelTypeAzure:
		return 0, errors.New("尚未实现")
	case constant.ChannelTypeCustom:
		baseURL = channel.GetBaseURL()
	//case common.ChannelTypeOpenAISB:
	//	return updateChannelOpenAISBBalance(channel)
	case constant.ChannelTypeAIProxy:
		return updateChannelAIProxyBalance(channel)
	case constant.ChannelTypeAPI2GPT:
		return updateChannelAPI2GPTBalance(channel)
	case constant.ChannelTypeAIGC2D:
		return updateChannelAIGC2DBalance(channel)
	case constant.ChannelTypeSiliconFlow:
		return updateChannelSiliconFlowBalance(channel)
	case constant.ChannelTypeDeepSeek:
		return updateChannelDeepSeekBalance(channel)
	case constant.ChannelTypeOpenRouter:
		return updateChannelOpenRouterBalance(channel)
	case constant.ChannelTypeMoonshot:
		return updateChannelMoonshotBalance(channel)
	default:
		return 0, errors.New("尚未实现")
	}
	url := fmt.Sprintf("%s/v1/dashboard/billing/subscription", baseURL)

	body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	subscription := OpenAISubscriptionResponse{}
	err = json.Unmarshal(body, &subscription)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	startDate := fmt.Sprintf("%s-01", now.Format("2006-01"))
	endDate := now.Format("2006-01-02")
	if !subscription.HasPaymentMethod {
		startDate = now.AddDate(0, 0, -100).Format("2006-01-02")
	}
	url = fmt.Sprintf("%s/v1/dashboard/billing/usage?start_date=%s&end_date=%s", baseURL, startDate, endDate)
	body, err = GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
	if err != nil {
		return 0, err
	}
	usage := OpenAIUsageResponse{}
	err = json.Unmarshal(body, &usage)
	if err != nil {
		return 0, err
	}
	balance := subscription.HardLimitUSD - usage.TotalUsage/100
	channel.UpdateBalance(balance)
	return balance, nil
}

func UpdateChannelBalance(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.CacheGetChannel(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel.ChannelInfo.IsMultiKey {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "多密钥渠道不支持余额查询",
		})
		return
	}
	balance, err := updateChannelBalance(channel)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"balance": balance,
	})
}

// upstreamPricingProbe new-api 上游公开 /api/pricing 响应里的分组倍率表。
type upstreamPricingProbe struct {
	Success    bool               `json:"success"`
	GroupRatio map[string]float64 `json:"group_ratio"`
}

// fetchNewAPIGroupRates 从 new-api 上游的公开 /api/pricing 拉分组倍率
// （pricing 模块公开时匿名可访问），映射成与 sub2api 分组同构的行供前端同一张表展示。
func fetchNewAPIGroupRates(channel *model.Channel) ([]Sub2APIAdminGroup, error) {
	url := fmt.Sprintf("%s/api/pricing", channel.GetBaseURL())
	body, err := GetResponseBody("GET", url, channel, http.Header{})
	if err != nil {
		return nil, err
	}
	probe := upstreamPricingProbe{}
	if err := common.Unmarshal(body, &probe); err != nil {
		return nil, err
	}
	if len(probe.GroupRatio) == 0 {
		return nil, errors.New("上游 /api/pricing 未返回分组倍率（可能未公开 pricing 模块）")
	}
	names := make([]string, 0, len(probe.GroupRatio))
	for name := range probe.GroupRatio {
		names = append(names, name)
	}
	sort.Strings(names)
	groups := make([]Sub2APIAdminGroup, 0, len(names))
	for i, name := range names {
		groups = append(groups, Sub2APIAdminGroup{
			ID:             int64(i + 1),
			Name:           name,
			Platform:       "new-api",
			Status:         "active",
			RateMultiplier: probe.GroupRatio[name],
		})
	}
	return groups, nil
}

// fetchUpstreamGroupRates 拉取该渠道上游的倍率信息，全程只用渠道 Key
// （我们只是上游中转站的客户，没有管理面权限）：
//   - sub2api 上游（开了 sub2api 余额查询）：/v1/usage 推导本 Key 的有效倍率，单行返回；
//   - new-api 上游：公开 /api/pricing 的分组倍率全表（匿名可访问）。
func fetchUpstreamGroupRates(channel *model.Channel) ([]Sub2APIAdminGroup, error) {
	if channel.GetSetting().Sub2ApiBalanceQuery {
		url := fmt.Sprintf("%s/v1/usage", channel.GetBaseURL())
		body, err := GetResponseBody("GET", url, channel, GetAuthHeader(channel.Key))
		if err != nil {
			return nil, err
		}
		response := Sub2APIUsageResponse{}
		if err = common.Unmarshal(body, &response); err != nil {
			return nil, err
		}
		derived := deriveSub2apiEffectiveRatio(&response)
		if derived == nil {
			return nil, errors.New("该 Key 尚无用量数据，无法推导有效倍率（跑一笔请求后再试）")
		}
		return []Sub2APIAdminGroup{{
			ID:             1,
			Name:           "current-key",
			Platform:       "sub2api",
			Status:         "active",
			RateMultiplier: *derived,
		}}, nil
	}
	return fetchNewAPIGroupRates(channel)
}

// findUpstreamGroupRate 按组名在上游分组表里找倍率：先精确匹配，再不区分大小写。
func findUpstreamGroupRate(groups []Sub2APIAdminGroup, name string) (float64, bool) {
	for _, g := range groups {
		if g.Name == name {
			return g.RateMultiplier, true
		}
	}
	for _, g := range groups {
		if strings.EqualFold(g.Name, name) {
			return g.RateMultiplier, true
		}
	}
	return 0, false
}

// applyChannelRatioSync 成本倍率回写的公共出口：变化超过 0.0001 才写库
// （防浮点噪声空更新），失败只记日志不反噬余额主流程。
func applyChannelRatioSync(channel *model.Channel, ratio float64, source string) {
	current := channel.GetChannelRatio()
	if math.Abs(current-ratio) < 1e-4 {
		return
	}
	if err := model.UpdateChannelRatio(channel.Id, ratio); err != nil {
		common.SysLog(fmt.Sprintf("channel %d channel_ratio sync write failed: %s", channel.Id, err.Error()))
		return
	}
	common.SysLog(fmt.Sprintf("channel %d channel_ratio synced from %s: %.4f -> %.4f",
		channel.Id, source, current, ratio))
}

// deriveSub2apiEffectiveRatio 从 /v1/usage 的成本摘要推本 Key 的有效倍率
// （actual_cost/cost，见 Sub2APIUsageCostBucket 注释）。优先当日口径——反映上游
// 最新费率；无当日消费退累计口径；表价低于 1 分钱视为无数据不推导。
// 四舍五入到 4 位小数，与 applyChannelRatioSync 的写入阈值配套防抖。
func deriveSub2apiEffectiveRatio(response *Sub2APIUsageResponse) *float64 {
	if response == nil || response.Usage == nil {
		return nil
	}
	for _, bucket := range []*Sub2APIUsageCostBucket{response.Usage.Today, response.Usage.Total} {
		if bucket == nil || bucket.Cost < 0.01 || bucket.ActualCost < 0 {
			continue
		}
		ratio := math.Round(bucket.ActualCost/bucket.Cost*10000) / 10000
		return &ratio
	}
	return nil
}

// maybeSyncSub2apiDerivedRatio sub2api 渠道的倍率自动同步：搭余额更新同一响应推导，
// 零额外请求、无需分组名、无需 admin key。
func maybeSyncSub2apiDerivedRatio(channel *model.Channel, response *Sub2APIUsageResponse) {
	if !channel.GetSetting().UpstreamRatioSync {
		return
	}
	derived := deriveSub2apiEffectiveRatio(response)
	if derived == nil {
		return
	}
	applyChannelRatioSync(channel, *derived, "sub2api usage (actual_cost/cost)")
}

// maybeSyncUpstreamGroupRatio new-api 上游的成本倍率自动同步：按配置的分组名
// 拉公开 /api/pricing 的分组倍率回写。拉取/匹配失败只记日志。
func maybeSyncUpstreamGroupRatio(channel *model.Channel) {
	setting := channel.GetSetting()
	if !setting.UpstreamRatioSync {
		return
	}
	groupName := strings.TrimSpace(setting.UpstreamGroupName)
	if groupName == "" {
		common.SysLog(fmt.Sprintf("channel %d ratio sync enabled but upstream_group_name is empty (required for new-api upstreams)", channel.Id))
		return
	}
	groups, err := fetchUpstreamGroupRates(channel)
	if err != nil {
		common.SysLog(fmt.Sprintf("channel %d upstream group ratio sync failed: %s", channel.Id, err.Error()))
		return
	}
	ratio, ok := findUpstreamGroupRate(groups, groupName)
	if !ok {
		common.SysLog(fmt.Sprintf("channel %d upstream group %q not found among %d groups", channel.Id, groupName, len(groups)))
		return
	}
	applyChannelRatioSync(channel, ratio, fmt.Sprintf("upstream group %q", groupName))
}

// GetChannelSub2APIGroupRates 只读拉取该渠道上游的分组成本倍率，供管理员比对/应用
// 为本渠道 channel_ratio（也可配置 upstream_group_name 走全自动同步）。
func GetChannelSub2APIGroupRates(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.CacheGetChannel(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	groups, err := fetchUpstreamGroupRates(channel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "拉取上游分组倍率失败：" + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groups,
	})
}

func updateAllChannelsBalance() error {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if channel.ChannelInfo.IsMultiKey {
			continue // skip multi-key channels
		}
		// TODO: support Azure
		//if channel.Type != common.ChannelTypeOpenAI && channel.Type != common.ChannelTypeCustom {
		//	continue
		//}
		balance, err := updateChannelBalance(channel)
		if err != nil {
			continue
		} else {
			// err is nil & balance <= 0 means quota is used up
			if balance <= 0 {
				service.DisableChannel(*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, "", channel.GetAutoBan()), "余额不足")
			}
		}
		time.Sleep(common.RequestInterval)
	}
	return nil
}

func UpdateAllChannelsBalance(c *gin.Context) {
	// TODO: make it async
	err := updateAllChannelsBalance()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func AutomaticallyUpdateChannels(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		common.SysLog("updating all channels")
		_ = updateAllChannelsBalance()
		common.SysLog("channels update done")
	}
}
