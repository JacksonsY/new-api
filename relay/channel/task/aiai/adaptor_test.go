package aiai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRequestBody 校验转发给 aiai.ac 的请求体：model 覆写为上游名、async 强制 true、
// 计费用的 ref_duration(顶层 + extra_body)被剥掉，其余文档参数原样保留。
func TestBuildRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"seedance-2.0-720p","prompt":"x","image":"https://e/i.jpg","video":"https://e/v.mp4",` +
		`"duration":10,"resolution":"1080p","aspect_ratio":"9:16","fps":24,"watermark":true,"with_audio":false,` +
		`"ref_duration":7,"extra_body":{"real_person_mode":true,"ref_duration":7},"async":false}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	a := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "doubao-seedance-2.0"},
	}
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	out, err := io.ReadAll(reader)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))

	assert.Equal(t, "doubao-seedance-2.0", m["model"], "model 应覆写为上游名")
	assert.Equal(t, true, m["async"], "async 应强制 true")

	_, hasRef := m["ref_duration"]
	assert.False(t, hasRef, "顶层 ref_duration 应被剥掉")
	eb, _ := m["extra_body"].(map[string]any)
	require.NotNil(t, eb, "extra_body 应保留")
	_, hasEbRef := eb["ref_duration"]
	assert.False(t, hasEbRef, "extra_body.ref_duration 应被剥掉")

	// 文档参数原样保留
	assert.Equal(t, "x", m["prompt"])
	assert.Equal(t, "https://e/i.jpg", m["image"])
	assert.Equal(t, "https://e/v.mp4", m["video"])
	assert.Equal(t, "1080p", m["resolution"])
	assert.Equal(t, "9:16", m["aspect_ratio"])
	assert.Equal(t, true, m["watermark"])
	assert.Equal(t, true, eb["real_person_mode"])
}

// TestEstimateBillingScenarios 用 aiai.ac 文档给的 6 个官方示例请求体,端到端跑 EstimateBilling,
// 校验哪些触发「编辑档」、以及最终计费(ModelPrice 0.5 基准)。只有传 video 的才进编辑档。
func TestEstimateBillingScenarios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const base = 0.5 // doubao-seedance-2.0 的 ModelPrice(480p 基准每秒价)

	cases := []struct {
		name     string
		body     string
		wantYuan float64
	}{
		{"文生视频 720p/5s", `{"model":"doubao-seedance-2.0","prompt":"x","duration":5,"resolution":"720p","aspect_ratio":"16:9","async":true}`, 5.0},
		{"图生视频 720p/5s", `{"model":"doubao-seedance-2.0","prompt":"x","image":"https://e/p.jpg","duration":5,"resolution":"720p","async":true}`, 5.0},
		{"真人模式 720p/5s", `{"model":"doubao-seedance-2.0","prompt":"x","image":"https://e/p.jpg","duration":5,"resolution":"720p","aspect_ratio":"9:16","extra_body":{"real_person_mode":true},"async":true}`, 5.0},
		{"首尾帧 720p/5s", `{"model":"doubao-seedance-2.0","prompt":"x","image":"https://e/b.jpg","image_tail":"https://e/l.jpg","duration":5,"resolution":"720p","async":true}`, 5.0},
		{"视频编辑 video/720p/10s", `{"model":"doubao-seedance-2.0","prompt":"x","video":"https://e/in.mp4","duration":10,"resolution":"720p","async":true}`, 11.9},
		{"音频视频 audio/720p/10s", `{"model":"doubao-seedance-2.0","prompt":"x","audio":"https://e/m.mp3","duration":10,"resolution":"720p","aspect_ratio":"16:9","async":true}`, 10.0},
	}

	for _, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(tc.body)))
		c.Request.Header.Set("Content-Type", "application/json")

		a := &TaskAdaptor{}
		info := &relaycommon.RelayInfo{OriginModelName: "doubao-seedance-2.0"}
		ratios := a.EstimateBilling(c, info)

		resRatio, ok := ratios["tier"]
		require.Truef(t, ok, "%s: 缺 resolution 倍率", tc.name)
		gotYuan := base * resRatio * ratios["seconds"]
		assert.InDeltaf(t, tc.wantYuan, gotYuan, 1e-9, "%s: 计费", tc.name)
	}
}

// TestEstimateBillingModelPrefix 校验带路由前缀的本地名（如 "A/doubao-seedance-2.0"）经模型重定向后，
// 分辨率倍率仍按干净的上游名（ChannelMeta.UpstreamModelName）命中价目表——否则前缀会让
// 档位查表查不到、720p/1080p/4k 掉回 480p 基准价而少收费。
func TestEstimateBillingModelPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"A/doubao-seedance-2.0","prompt":"x","duration":5,"resolution":"720p","async":true}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	a := &TaskAdaptor{}
	// 本地名带前缀（用户在本站配 ModelPrice 用它）；模型重定向后 UpstreamModelName 为干净上游名。
	info := &relaycommon.RelayInfo{
		OriginModelName: "A/doubao-seedance-2.0",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "doubao-seedance-2.0"},
	}
	ratios := a.EstimateBilling(c, info)

	resRatio, ok := ratios["tier"]
	require.True(t, ok, "带前缀模型应仍命中分辨率倍率，而非退化为纯秒计费")
	assert.InDelta(t, 2.0, resRatio, 1e-9, "720p 相对 480p 基准倍率应为 1.0/0.5=2.0")
	assert.InDelta(t, 5.0, ratios["seconds"], 1e-9)
	assert.InDelta(t, 5.0, 0.5*resRatio*ratios["seconds"], 1e-9, "最终 = ModelPrice0.5 × 2.0 × 5s = 5 元")
}

// TestEstimateBillingSizeFallback 校验客户端不传 resolution、改用 size 传分辨率时，
// 计费仍能识别分辨率档、加成正确倍率——否则会掉回默认 720p 而少收（这里 1080p 少收一半以上）。
func TestEstimateBillingSizeFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 只给 size=1920x1080、不给 resolution：应识别为 1080p（相对 480p 基准倍率 2.5/0.5=5.0）
	body := `{"model":"doubao-seedance-2.0","prompt":"x","duration":5,"size":"1920x1080","async":true}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	a := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: "doubao-seedance-2.0"}
	ratios := a.EstimateBilling(c, info)

	assert.InDelta(t, 5.0, ratios["tier"], 1e-9, "size=1920x1080 应兜底识别为 1080p(倍率5.0)，而非默认720p(2.0)")
	assert.InDelta(t, 5.0, ratios["seconds"], 1e-9)
	assert.InDelta(t, 12.5, 0.5*ratios["tier"]*ratios["seconds"], 1e-9, "最终 = 0.5 × 5.0 × 5 = 12.5 元")
}

// TestEstimateBillingVeoTiers 校验 Veo 分辨率分档（720p 基准）+ 均衡线路固定 8 秒（忽略 duration）。
// 之前把 Veo 当平价 → 1080p/4k 少收；此测试锁死高档倍率与固定 8s。
func TestEstimateBillingVeoTiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name     string
		model    string
		body     string
		wantTier float64
	}{
		// fast：4k=2.25 / 720p 基准 0.75 = 3.0 倍；传 duration:4 应被忽略（固定 8s）
		{"fast 4k dur4→8s", "veo-3.1-fast-generate-preview",
			`{"model":"veo-3.1-fast-generate-preview","prompt":"x","resolution":"4k","duration":4,"async":true}`, 3.0},
		// full：1080p=3.0 / 基准 3.0 = 1.0
		{"full 1080p", "veo-3.1-generate-preview",
			`{"model":"veo-3.1-generate-preview","prompt":"x","resolution":"1080p","async":true}`, 1.0},
		// lite：1080p=0.60 / 基准 0.375 = 1.6
		{"lite 1080p", "veo-3.1-lite-generate-preview",
			`{"model":"veo-3.1-lite-generate-preview","prompt":"x","resolution":"1080p","async":true}`, 1.6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(tc.body)))
			c.Request.Header.Set("Content-Type", "application/json")
			a := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{OriginModelName: tc.model}
			ratios := a.EstimateBilling(c, info)
			assert.InDelta(t, tc.wantTier, ratios["tier"], 1e-9, "分档倍率")
			assert.InDelta(t, 8.0, ratios["seconds"], 1e-9, "Veo 均衡线路固定 8s")
		})
	}
}

// TestEstimateBillingKlingTiers 校验 Kling 按 quality(std/pro)×with_audio 分档（基准 std 无声 0.6）。
// 之前 billingParams 没读 quality/with_audio → pro/有声全按 std 无声少收；此测试锁死四档。
func TestEstimateBillingKlingTiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name     string
		body     string
		wantTier float64
	}{
		{"std noaudio(默认)", `{"model":"kling-video-v3","prompt":"x","duration":5,"async":true}`, 1.0}, // 0.6/0.6
		{"std audio", `{"model":"kling-video-v3","prompt":"x","duration":5,"with_audio":true,"async":true}`, 0.9 / 0.6},
		{"pro noaudio", `{"model":"kling-video-v3","prompt":"x","duration":5,"quality":"pro","async":true}`, 0.8 / 0.6},
		{"pro audio", `{"model":"kling-video-v3","prompt":"x","duration":5,"quality":"pro","with_audio":true,"async":true}`, 2.0}, // 1.2/0.6
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(tc.body)))
			c.Request.Header.Set("Content-Type", "application/json")
			a := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{OriginModelName: "kling-video-v3"}
			ratios := a.EstimateBilling(c, info)
			assert.InDelta(t, tc.wantTier, ratios["tier"], 1e-9, "分档倍率")
			assert.InDelta(t, 5.0, ratios["seconds"], 1e-9, "kling 按 duration 计费")
		})
	}
}

// TestEstimateBillingGrokTiers 校验 Grok 分辨率分档（480p 基准 0.375、720p 0.525），
// 且默认档=480p（不传 resolution 时按基准 0.375，不误按 720p 多收）。
func TestEstimateBillingGrokTiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name     string
		body     string
		wantTier float64
	}{
		{"默认(不传)→480p", `{"model":"grok-imagine-video","prompt":"x","duration":5,"async":true}`, 1.0}, // 0.375/0.375
		{"显式 480p", `{"model":"grok-imagine-video","prompt":"x","duration":5,"resolution":"480p","async":true}`, 1.0},
		{"显式 720p", `{"model":"grok-imagine-video","prompt":"x","duration":5,"resolution":"720p","async":true}`, 0.525 / 0.375}, // 1.4
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(tc.body)))
			c.Request.Header.Set("Content-Type", "application/json")
			a := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{OriginModelName: "grok-imagine-video"}
			ratios := a.EstimateBilling(c, info)
			assert.InDelta(t, tc.wantTier, ratios["tier"], 1e-9, "分档倍率")
			assert.InDelta(t, 5.0, ratios["seconds"], 1e-9)
		})
	}
}

// TestEstimateBillingHappyhorseTiers 校验 HappyHorse 分辨率分档：720p=0.9、1080p=1.6（官方明细）。
// base 取 1.6(=页面官方起价/1080p)，故 720p 走 0.5625 倍档；默认(不传)= 1080p。
// 之前平价 1.6 会把 720p 多收 78%，此测试锁死 720p 只收 0.9。
func TestEstimateBillingHappyhorseTiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const base = 1.6
	cases := []struct {
		name      string
		body      string
		wantYuanS float64 // 每秒官方价
	}{
		{"默认(不传)→1080p", `{"model":"happyhorse-1.0-t2v","prompt":"x","duration":5,"async":true}`, 1.6},
		{"显式 1080p", `{"model":"happyhorse-1.0-t2v","prompt":"x","duration":5,"resolution":"1080p","async":true}`, 1.6},
		{"显式 720p", `{"model":"happyhorse-1.0-t2v","prompt":"x","duration":5,"resolution":"720p","async":true}`, 0.9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(tc.body)))
			c.Request.Header.Set("Content-Type", "application/json")
			a := &TaskAdaptor{}
			info := &relaycommon.RelayInfo{OriginModelName: "happyhorse-1.0-t2v"}
			ratios := a.EstimateBilling(c, info)
			assert.InDelta(t, tc.wantYuanS, base*ratios["tier"], 1e-9, "每秒官方价")
			assert.InDelta(t, 5.0, ratios["seconds"], 1e-9)
		})
	}
}

// TestEstimateBillingDurationCap 校验用户可控的 duration/ref_duration 计费秒数被钳到
// MaxTaskDurationSeconds——防止无上界乘数导致额度溢出/异常多收（AGENTS.md 计费安全不变量）。
func TestEstimateBillingDurationCap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wantCap := float64(relaycommon.MaxTaskDurationSeconds)
	a := &TaskAdaptor{}
	run := func(model, body string) map[string]float64 {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(body)))
		c.Request.Header.Set("Content-Type", "application/json")
		return a.EstimateBilling(c, &relaycommon.RelayInfo{OriginModelName: model})
	}

	// 超大 duration → 钳到上界
	r1 := run("kling-video-v3", `{"model":"kling-video-v3","prompt":"x","duration":2000000000}`)
	assert.Equal(t, wantCap, r1["seconds"], "超大 duration 应钳到 MaxTaskDurationSeconds")

	// seedance 编辑档超大 ref_duration → 也钳到上界
	r2 := run("doubao-seedance-2.0", `{"model":"doubao-seedance-2.0","prompt":"x","video":"https://e/v.mp4","duration":10,"ref_duration":2000000000}`)
	assert.Equal(t, wantCap, r2["seconds"], "超大 ref_duration 应钳到 MaxTaskDurationSeconds")
}

// TestHasReferenceVideo 校验参考视频检测同时兼容字符串与数组形态，
// 并把 null / 空串 / 空数组 判为无参考视频（否则会把纯生成误判成编辑档而少收）。
func TestHasReferenceVideo(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`"https://x/v.mp4"`, true},         // 字符串形态
		{`["https://x/v.mp4"]`, true},       // 数组形态（API 手册）
		{`["https://a","https://b"]`, true}, // 多参考视频
		{``, false},                         // 字段缺失（RawMessage 为 nil）
		{`null`, false},                     // 显式 null
		{`""`, false},                       // 空字符串
		{`[]`, false},                       // 空数组
	}
	for _, c := range cases {
		var raw json.RawMessage
		if c.raw != "" {
			raw = json.RawMessage(c.raw)
		}
		assert.Equalf(t, c.want, hasReferenceVideo(raw), "hasReferenceVideo(%q)", c.raw)
	}
}

// TestEstimateBillingRefDuration 校验客户端传入参考视频时长后，视频编辑档按官方公式
// Max(参考+生成, 最低计费) 精确计费——复现文档「720p 生成 5 秒」两个示例。
func TestEstimateBillingRefDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const base = 0.5

	cases := []struct {
		name     string
		body     string
		wantYuan float64
	}{
		// 参考 3 秒：Max(3+5,9)=9 → 9×0.7 = 6.3（下限)
		{"ref 3s", `{"model":"doubao-seedance-2.0","prompt":"x","video":"https://e/in.mp4","duration":5,"resolution":"720p","ref_duration":3,"async":true}`, 6.3},
		// 参考 7 秒：Max(7+5,9)=12 → 12×0.7 = 8.4（实际)
		{"ref 7s", `{"model":"doubao-seedance-2.0","prompt":"x","video":"https://e/in.mp4","duration":5,"resolution":"720p","ref_duration":7,"async":true}`, 8.4},
		// extra_body.ref_duration 同样生效
		{"ref 7s via extra_body", `{"model":"doubao-seedance-2.0","prompt":"x","video":"https://e/in.mp4","duration":5,"resolution":"720p","extra_body":{"ref_duration":7},"async":true}`, 8.4},
		// 不传 ref_duration → 退化为下限 9 秒 → 6.3
		{"no ref_duration (floor)", `{"model":"doubao-seedance-2.0","prompt":"x","video":"https://e/in.mp4","duration":5,"resolution":"720p","async":true}`, 6.3},
	}

	for _, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(tc.body)))
		c.Request.Header.Set("Content-Type", "application/json")

		a := &TaskAdaptor{}
		info := &relaycommon.RelayInfo{OriginModelName: "doubao-seedance-2.0"}
		ratios := a.EstimateBilling(c, info)
		resRatio, ok := ratios["tier"]
		require.Truef(t, ok, "%s: 缺 resolution 倍率", tc.name)
		gotYuan := base * resRatio * ratios["seconds"]
		assert.InDeltaf(t, tc.wantYuan, gotYuan, 1e-9, "%s", tc.name)
	}
}

// TestParseTaskResult 用 aiai.ac「后台任务」文档给的原样响应，锁定查询解析契约。
func TestParseTaskResult(t *testing.T) {
	a := &TaskAdaptor{}

	t.Run("processing", func(t *testing.T) {
		info, err := a.ParseTaskResult([]byte(`{"task_id":"req_abc123","status":"processing","created_at":1699999999}`))
		require.NoError(t, err)
		assert.Equal(t, model.TaskStatusInProgress, info.Status)
		assert.Empty(t, info.Url)
	})

	t.Run("succeeded", func(t *testing.T) {
		info, err := a.ParseTaskResult([]byte(`{"task_id":"req_abc123","status":"succeeded","video_url":"https://example.com/videos/output.mp4","duration":5,"size":"720x720","created_at":1699999999,"completed_at":1700000050}`))
		require.NoError(t, err)
		assert.Equal(t, model.TaskStatusSuccess, info.Status)
		assert.Equal(t, "https://example.com/videos/output.mp4", info.Url)
	})

	t.Run("failed", func(t *testing.T) {
		info, err := a.ParseTaskResult([]byte(`{"task_id":"req_abc123","status":"failed","error_message":"内容违规检测失败","created_at":1699999999,"completed_at":1700000020}`))
		require.NoError(t, err)
		assert.Equal(t, model.TaskStatusFailure, info.Status)
		assert.Equal(t, "内容违规检测失败", info.Reason)
	})

	// API 手册的另一套字段名：task_status="succeed" + video_result[].url
	t.Run("api-manual schema succeed", func(t *testing.T) {
		info, err := a.ParseTaskResult([]byte(`{"task_id":"req_abc123","task_status":"succeed","video_result":[{"url":"https://bubblegeek.cn/api/v1/file/xxx/video.mp4","cover_image_url":"https://x/cover.jpg","duration":5}]}`))
		require.NoError(t, err)
		assert.Equal(t, model.TaskStatusSuccess, info.Status)
		assert.Equal(t, "https://bubblegeek.cn/api/v1/file/xxx/video.mp4", info.Url)
	})

}

// TestEstimateBillingNoModelPrice 未配 ModelPrice 的模型：EstimateBilling 返回 nil，不叠加任何
// OtherRatio（避免对未配价模型误加秒/分档倍率）。
func TestEstimateBillingNoModelPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const unpriced = "aiai-unpriced-model" // TestMain 未给它配 ModelPrice

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations",
		bytes.NewReader([]byte(`{"model":"`+unpriced+`","prompt":"x","resolution":"720p","duration":10}`)))
	c.Request.Header.Set("Content-Type", "application/json")

	a := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: unpriced}
	assert.Nil(t, a.EstimateBilling(c, info), "未配 ModelPrice 应返回 nil")
}
