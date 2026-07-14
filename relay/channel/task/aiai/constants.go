package aiai

import (
	"math"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// init 把各模型的价目折算成相对基准价的倍率，注册到 ratio_setting，供模型广场定价页展示
// 「分档定价」表（价 = ModelPrice × 倍率，管理员改 ModelPrice 时展示价等比跟随）。
// 分辨率维度按 480p/720p/1080p/4k 展示（dimResRef 另带「带参考视频」列）；quality×声音 维度
// （Kling）展示成 4 行、描述放分辨率列（如「720p 有声」）、价在「无参考」列。
func init() {
	for model, p := range videoPricingTable {
		if p.base <= 0 {
			continue
		}
		var tiers []ratio_setting.VideoResolutionRatioTier
		switch p.dimension {
		case dimResRef, dimResolution:
			for _, res := range []string{"480p", "720p", "1080p", "4k"} {
				noRef, ok := p.prices[res]
				if !ok {
					continue
				}
				tier := ratio_setting.VideoResolutionRatioTier{Resolution: res, NoRefRatio: noRef / p.base}
				if withRef, ok := p.prices[res+refSuffix]; ok {
					tier.WithRefRatio = withRef / p.base
					tier.HasWithRef = true
				}
				tiers = append(tiers, tier)
			}
		case dimQualityAudio:
			for _, o := range []struct{ label, key string }{
				{"720p 无声", "std|noaudio"}, {"720p 有声", "std|audio"},
				{"1080p 无声", "pro|noaudio"}, {"1080p 有声", "pro|audio"},
			} {
				if price, ok := p.prices[o.key]; ok {
					tiers = append(tiers, ratio_setting.VideoResolutionRatioTier{Resolution: o.label, NoRefRatio: price / p.base})
				}
			}
		}
		if len(tiers) > 0 {
			ratio_setting.RegisterVideoResolutionRatios(model, tiers)
		}
	}
}

// ChannelName 渠道显示名。
var ChannelName = "aiai"

// ModelList aiai.ac 的视频模型（上游模型名），走本异步任务适配器。
// 图片模型（gpt-image-2 / nano-banana*）走 OpenAI 兼容同步路径，不在此列。
// 实际支持哪些模型由渠道「模型」栏决定；此列表仅用于自动填充/测试。
var ModelList = []string{
	// 豆包 Seedance —— 分辨率 × 参考视频分档
	"doubao-seedance-2.0",
	"doubao-seedance-2.0-mini",
	"doubao-seedance-2.0-fast",
	// Google Veo —— 分辨率分档（720p 基准），均衡线路固定 8s
	"veo-3.1-generate-preview",
	"veo-3.1-fast-generate-preview",
	"veo-3.1-lite-generate-preview",
	// 可灵 Kling —— quality × 声音 分档
	"kling-video-v3",
	"kling-video-v3-omni",
	// Grok Imagine —— 分辨率分档（480p 基准）
	"grok-imagine-video",
	// HappyHorse —— 分辨率分档（720p 0.9 / 1080p 1.6）
	"happyhorse-1.0-t2v",
	"happyhorse-1.0-i2v",
	"happyhorse-1.0-r2v",
	"happyhorse-1.0-video-edit",
}

// 分档维度：不同模型按不同请求参数分档（数据来源：aiai.ac 各模型文档价目表）。
const (
	dimResRef       = "res_ref"       // 分辨率 × 是否带参考视频（seedance），基准 480p 无参考
	dimResolution   = "resolution"    // 分辨率（veo），基准 720p（无 480p）
	dimQualityAudio = "quality_audio" // quality(std/pro) × with_audio（kling），基准 std 无声
	refSuffix       = "|ref"          // dimResRef 档位键里「带参考视频」的后缀
)

// videoPricing 一个模型的按档定价（元/秒）。
//
//	base         最低档单价 = 管理员应在「模型固定价格 ModelPrice」为该模型配置的每秒基准价
//	prices       档位键 → 每秒价；档位键由 dimension 决定怎么从请求算（见 tierKey）
//	dimension    分档维度
//	fixedSeconds >0 时计费秒数固定（如 Veo 均衡线路固定 8 秒、忽略 duration 参数）
//
// 计费：ratio = prices[档位] / base（OtherRatio "tier"），再乘计费秒数（OtherRatio "seconds"）；
// 最终额度 = ModelPrice × ratio × 计费秒数。
type videoPricing struct {
	base         float64
	prices       map[string]float64
	dimension    string
	fixedSeconds int
	defaultRes   string // 分辨率维度模型的默认档（客户端不传 resolution 时用）；空则回退 normalizeResolution 的 720p
}

// klingPrices Kling std/pro × 有无声 的每秒价（v3 与 v3-omni 共用）。
var klingPrices = map[string]float64{
	"std|noaudio": 0.6, "std|audio": 0.9,
	"pro|noaudio": 0.8, "pro|audio": 1.2,
}

// happyhorsePrices HappyHorse 分辨率每秒价（t2v/i2v/r2v/video-edit 共用）。
// 来源：/models「查看明细」官方原价——720p 0.9、1080p 1.6。页面"¥1.600起"取的是 1080p(outputPrice)，
// 真实最低档是 720p 0.9，故 base 取 1.6(=页面官方起价，与你配 ModelPrice 一致)、720p 走 0.5625 倍档。
var happyhorsePrices = map[string]float64{"720p": 0.9, "1080p": 1.6}

var videoPricingTable = map[string]videoPricing{
	// 豆包 Seedance：分辨率 × 参考视频（视频编辑），基准 480p 无参考。
	"doubao-seedance-2.0": {base: 0.5, dimension: dimResRef, prices: map[string]float64{
		"480p": 0.5, "720p": 1.0, "1080p": 2.5, "4k": 5.0,
		"480p|ref": 0.35, "720p|ref": 0.7, "1080p|ref": 1.55, "4k|ref": 3.15,
	}},
	"doubao-seedance-2.0-mini": {base: 0.25, dimension: dimResRef, prices: map[string]float64{
		"480p": 0.25, "720p": 0.5, "480p|ref": 0.24, "720p|ref": 0.48,
	}},
	"doubao-seedance-2.0-fast": {base: 0.4, dimension: dimResRef, prices: map[string]float64{
		"480p": 0.4, "720p": 0.8, "480p|ref": 0.24, "720p|ref": 0.48,
	}},
	// Google Veo 3.1：分辨率，基准 720p（无 480p，Lite 无 4k）。均衡线路固定 8 秒、忽略 duration。
	"veo-3.1-generate-preview":      {base: 3.0, dimension: dimResolution, fixedSeconds: 8, prices: map[string]float64{"720p": 3.0, "1080p": 3.0, "4k": 4.5}},
	"veo-3.1-fast-generate-preview": {base: 0.75, dimension: dimResolution, fixedSeconds: 8, prices: map[string]float64{"720p": 0.75, "1080p": 0.90, "4k": 2.25}},
	"veo-3.1-lite-generate-preview": {base: 0.375, dimension: dimResolution, fixedSeconds: 8, prices: map[string]float64{"720p": 0.375, "1080p": 0.60}},
	// 可灵 Kling V3：quality(std=720p / pro=1080p) × with_audio，基准 std 无声。
	"kling-video-v3":      {base: 0.6, dimension: dimQualityAudio, prices: klingPrices},
	"kling-video-v3-omni": {base: 0.6, dimension: dimQualityAudio, prices: klingPrices},
	// xAI Grok Imagine：分辨率，基准 480p（默认档也是 480p）。数据：/models geek office 档位（480p:720p=1:1.4）。
	"grok-imagine-video": {base: 0.375, dimension: dimResolution, defaultRes: "480p", prices: map[string]float64{"480p": 0.375, "720p": 0.525}},
	// HappyHorse 1.0：分辨率（720p 0.9 / 1080p 1.6），base 取 1080p=1.6（=页面官方起价），默认档 1080p。
	"happyhorse-1.0-t2v":        {base: 1.6, dimension: dimResolution, defaultRes: "1080p", prices: happyhorsePrices},
	"happyhorse-1.0-i2v":        {base: 1.6, dimension: dimResolution, defaultRes: "1080p", prices: happyhorsePrices},
	"happyhorse-1.0-r2v":        {base: 1.6, dimension: dimResolution, defaultRes: "1080p", prices: happyhorsePrices},
	"happyhorse-1.0-video-edit": {base: 1.6, dimension: dimResolution, defaultRes: "1080p", prices: happyhorsePrices},
}

// tierKey 根据模型的分档维度，从请求参数算出档位键。
func (p videoPricing) tierKey(bp billingParams) string {
	switch p.dimension {
	case dimResRef:
		k := normalizeResolution(firstNonEmpty(bp.effectiveResolution(), p.defaultRes))
		if hasReferenceVideo(bp.Video) {
			k += refSuffix
		}
		return k
	case dimResolution:
		return normalizeResolution(firstNonEmpty(bp.effectiveResolution(), p.defaultRes))
	case dimQualityAudio:
		q := "std"
		if strings.EqualFold(strings.TrimSpace(bp.Quality), "pro") {
			q = "pro"
		}
		if bp.WithAudio {
			return q + "|audio"
		}
		return q + "|noaudio"
	}
	return ""
}

// tierRatio 返回请求相对基准价的倍率；未知档位落基准 1.0（对应上游会拒绝的非法组合，不影响计费）。
func (p videoPricing) tierRatio(bp billingParams) float64 {
	if p.base <= 0 {
		return 1.0
	}
	if price, ok := p.prices[p.tierKey(bp)]; ok {
		return price / p.base
	}
	return 1.0
}

// billedSeconds 计费秒数：fixedSeconds 优先（Veo 均衡线路固定 8s）；带参考视频的 seedance 走
// 「Max(参考+生成, 最低计费下限)」；其余按生成时长（缺省 5s）。
func (p videoPricing) billedSeconds(bp billingParams) int {
	if p.fixedSeconds > 0 {
		return p.fixedSeconds
	}
	d := bp.Duration
	if d <= 0 {
		d = 5
	}
	if p.dimension == dimResRef && hasReferenceVideo(bp.Video) {
		s := minBillingSeconds(d)
		if total := bp.refDuration() + d; total > s {
			s = total
		}
		// 计费秒数是用户可控乘数（duration/ref_duration），钳制上界防止溢出/异常多收。
		return min(s, relaycommon.MaxTaskDurationSeconds)
	}
	return min(d, relaycommon.MaxTaskDurationSeconds)
}

// minBillingSeconds 带参考视频时的最低计费时长（seedance 视频编辑档）。
// 官方对照表（生成时长 4..15 → 7,9,10,12,14,15,17,19,20,22,24,25）逐行等于 ceil(生成时长 × 5 / 3)。
func minBillingSeconds(duration int) int {
	if duration <= 0 {
		return 0
	}
	return int(math.Ceil(float64(duration) * 5.0 / 3.0))
}

func normalizeResolution(resolution string) string {
	r := strings.ToLower(strings.TrimSpace(resolution))
	if r == "" {
		return "720p" // aiai.ac 默认 720p
	}
	return r
}
