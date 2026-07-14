package aiai

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain 给各模型配 ModelPrice（aiai 全部按秒计费），EstimateBilling 才会返回
// 每秒/分辨率倍率（未配 ModelPrice 的模型返回 nil）。
func TestMain(m *testing.M) {
	// "A/doubao-seedance-2.0" 模拟带路由前缀的本地名配置 ModelPrice（见 TestEstimateBillingModelPrefix）；
	// veo/kling 基准价让它们进入秒计费模式，以测各自的分档倍率。
	_ = ratio_setting.UpdateModelPriceByJSONString(
		`{"doubao-seedance-2.0":0.5,"doubao-seedance-2.0-mini":0.25,"doubao-seedance-2.0-fast":0.4,"A/doubao-seedance-2.0":0.5,` +
			`"veo-3.1-generate-preview":3.0,"veo-3.1-fast-generate-preview":0.75,"veo-3.1-lite-generate-preview":0.375,"kling-video-v3":0.6,"grok-imagine-video":0.375,"happyhorse-1.0-t2v":1.6}`)
	os.Exit(m.Run())
}

// TestRegisteredResolutionRatios 校验 init 注册的分辨率倍率,乘以管理员基准价(ModelPrice)
// 后等于 aiai.ac 价目表的每秒价——覆盖 2.0 / Mini / Fast 三个模型,保证定价页展示与实际计费一致。
func TestRegisteredResolutionRatios(t *testing.T) {
	cases := []struct {
		model  string
		base   float64               // 管理员在 ModelPrice 配的基准每秒价
		hasRef bool                  // 是否有「带参考视频」档（seedance 有，veo 无）
		want   map[string][2]float64 // 分辨率 -> {无参考每秒价, 带参考每秒系数}
	}{
		{"doubao-seedance-2.0", 0.5, true, map[string][2]float64{
			"480p": {0.5, 0.35}, "720p": {1.0, 0.7}, "1080p": {2.5, 1.55}, "4k": {5.0, 3.15},
		}},
		{"doubao-seedance-2.0-mini", 0.25, true, map[string][2]float64{
			"480p": {0.25, 0.24}, "720p": {0.5, 0.48},
		}},
		{"doubao-seedance-2.0-fast", 0.4, true, map[string][2]float64{
			"480p": {0.4, 0.24}, "720p": {0.8, 0.48},
		}},
		// Veo 也注册分辨率倍率（720p 基准、无带参考档）
		{"veo-3.1-fast-generate-preview", 0.75, false, map[string][2]float64{
			"720p": {0.75, 0}, "1080p": {0.90, 0}, "4k": {2.25, 0},
		}},
	}
	for _, tc := range cases {
		tiers, ok := ratio_setting.GetVideoResolutionRatios(tc.model)
		require.Truef(t, ok, "init 未注册 %s 分辨率倍率", tc.model)
		require.Lenf(t, tiers, len(tc.want), "%s 分辨率档数", tc.model)
		for _, tr := range tiers {
			w, exists := tc.want[tr.Resolution]
			require.Truef(t, exists, "%s 未预期的分辨率档 %s", tc.model, tr.Resolution)
			assert.InDeltaf(t, w[0], tc.base*tr.NoRefRatio, 1e-9, "%s %s 无参考", tc.model, tr.Resolution)
			if tc.hasRef {
				require.Truef(t, tr.HasWithRef, "%s %s 缺带参考计价", tc.model, tr.Resolution)
				assert.InDeltaf(t, w[1], tc.base*tr.WithRefRatio, 1e-9, "%s %s 带参考", tc.model, tr.Resolution)
			} else {
				assert.Falsef(t, tr.HasWithRef, "%s %s 不应有带参考档", tc.model, tr.Resolution)
			}
		}
	}
}

// TestKlingDisplayTiers 校验 kling 的 quality×声音 维度注册成定价页展示档：4 行、描述放
// 分辨率列（如「720p 有声」）、价在「无参考」列、无「带参考」档。价 = base(0.6) × 倍率。
func TestKlingDisplayTiers(t *testing.T) {
	tiers, ok := ratio_setting.GetVideoResolutionRatios("kling-video-v3")
	require.True(t, ok, "kling 应注册展示档")
	const base = 0.6
	want := map[string]float64{"720p 无声": 0.6, "720p 有声": 0.9, "1080p 无声": 0.8, "1080p 有声": 1.2}
	require.Len(t, tiers, len(want))
	for _, tr := range tiers {
		w, exists := want[tr.Resolution]
		require.Truef(t, exists, "未预期档 %s", tr.Resolution)
		assert.InDeltaf(t, w, base*tr.NoRefRatio, 1e-9, "%s 每秒价", tr.Resolution)
		assert.Falsef(t, tr.HasWithRef, "%s 不应有带参考档", tr.Resolution)
	}
}

// TestMinBillingSeconds 校验最低计费时长闭式 ceil(d*5/3) 与官方对照表逐行一致。
func TestMinBillingSeconds(t *testing.T) {
	want := map[int]int{4: 7, 5: 9, 6: 10, 7: 12, 8: 14, 9: 15, 10: 17, 11: 19, 12: 20, 13: 22, 14: 24, 15: 25}
	for d := 4; d <= 15; d++ {
		assert.Equalf(t, want[d], minBillingSeconds(d), "minBillingSeconds(%d)", d)
	}
}

// TestSeedanceTierCharge 校验 seedance 分辨率×参考视频分档：基准价 × 倍率 × 计费秒数 与价目表一致
// （doubao-seedance-2.0 基准 = 480p 无参考 = 0.5 元/秒；编辑档计费秒数走最低下限 ceil(d*5/3)）。
func TestSeedanceTierCharge(t *testing.T) {
	const base = 0.5
	p := videoPricingTable["doubao-seedance-2.0"]

	cases := []struct {
		name       string
		resolution string
		hasVideo   bool
		duration   int
		wantYuan   float64
	}{
		{"480p no-ref 5s", "480p", false, 5, 2.5},    // 0.5×5
		{"720p no-ref 5s", "720p", false, 5, 5.0},    // 1.0×5
		{"1080p no-ref 8s", "1080p", false, 8, 20.0}, // 2.5×8
		{"4k no-ref 6s", "4k", false, 6, 30.0},       // 5.0×6
		{"default(720p) no-ref 5s", "", false, 5, 5.0},
		{"edit 720p gen5s floor9", "720p", true, 5, 6.3},       // 0.7×9  (doc 示例)
		{"edit 1080p gen12s floor20", "1080p", true, 12, 31.0}, // 1.55×20
	}
	for _, tc := range cases {
		bp := billingParams{Resolution: tc.resolution, Duration: tc.duration}
		if tc.hasVideo {
			bp.Video = json.RawMessage(`"https://e/v.mp4"`)
		}
		gotYuan := base * p.tierRatio(bp) * float64(p.billedSeconds(bp))
		assert.InDeltaf(t, tc.wantYuan, gotYuan, 1e-9, "%s: charge", tc.name)
	}
}

// TestUnknownModelHasNoTable 未配价目表的模型不在 videoPricingTable（EstimateBilling 会走纯秒平价兜底）。
// 注：当前 13 个视频模型已全部有档位表，此兜底仅对未来/未知模型生效。
func TestUnknownModelHasNoTable(t *testing.T) {
	_, ok := videoPricingTable["some-unknown-model"]
	assert.False(t, ok)
}

// TestResolutionFromSize 校验 size("宽x高") → 分辨率档：按短边、就近向上取档，
// 兼容 x/*/× 分隔与竖屏；无法解析返回 ""（退化到默认档）。
func TestResolutionFromSize(t *testing.T) {
	cases := map[string]string{
		"854x480":   "480p",
		"1280x720":  "720p",
		"720x1280":  "720p", // 竖屏，短边 720
		"720x720":   "720p",
		"1920x1080": "1080p",
		"1080x1920": "1080p", // 竖屏，短边 1080
		"3840x2160": "4k",
		"1920×1080": "1080p", // 全角 ×
		"1280*720":  "720p",  // 星号
		"640x360":   "480p",  // 360 短边就近向上到 480p 档（宁可多收）
		"":          "",
		"abc":       "",
		"1280":      "", // 无分隔符
		"0x0":       "",
	}
	for in, want := range cases {
		assert.Equalf(t, want, resolutionFromSize(in), "resolutionFromSize(%q)", in)
	}
}
