package aiai

import (
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseTaskResult_RealAiaiContract 用真机抓取的 aiai 视频响应锁定响应契约。
//
// 抓取自 2026-07-14/15 的真实调用，跨 4 家上游厂商（火山 Seedance / 快手 Kling /
// HappyHorse / xAI Grok）验证 aiai 归一化后的统一信封。
// 关键点：aiai 文档（文生视频页）给的示例是错的——它写 status:"succeeded" + 顶层 video_url +
// 顶层 error_message，而真机统一返回 task_status:"succeed"/"failed" + video_result[].url +
// 嵌套 error.message，且无 usage。一个只认文档的解析器会把每个成功任务判成未识别态、永远轮询
// 不结束。此测试保证我们吃的是真机契约而不是文档谎言，且契约跨厂商一致。
func TestParseTaskResult_RealAiaiContract(t *testing.T) {
	a := &TaskAdaptor{}

	cases := []struct {
		name       string
		body       string
		wantSt     string // TaskInfo.Status 是 string；model.TaskStatus* 多为无类型字符串常量
		wantURL    string
		wantReason string
	}{
		{
			name:   "submit_pending",
			body:   `{"model":"doubao-seedance-2.0-mini","task_id":"2077083266816409600","task_status":"pending"}`,
			wantSt: model.TaskStatusQueued,
		},
		{
			name:   "poll_running",
			body:   `{"model":"doubao-seedance-2.0-mini","task_id":"e2b9c527-4de1-40df-a21e-5a625ee90789","task_status":"running"}`,
			wantSt: model.TaskStatusInProgress,
		},
		{
			// 真机成功态：task_status:"succeed"（非文档的 "succeeded"）+ video_result[0].url（非文档顶层 video_url）+ 无 usage
			name:    "poll_succeed_real",
			body:    `{"model":"doubao-seedance-2.0-mini","task_id":"e2b9c527-4de1-40df-a21e-5a625ee90789","task_status":"succeed","video_result":[{"url":"https://ark-acg-cn-beijing.tos-cn-beijing.volces.com/doubao-seedance-2-0-mini/02178405021613900000000000000000000ffffac174420e33b92.mp4?X-Tos-Algorithm=TOS4-HMAC-SHA256&X-Tos-Expires=86400&X-Tos-Signature=e7339031ac6678610d9de062632779decf75e59454834cba9eb3877375b5936f&X-Tos-SignedHeaders=host"}]}`,
			wantSt:  model.TaskStatusSuccess,
			wantURL: "https://ark-acg-cn-beijing.tos-cn-beijing.volces.com/doubao-seedance-2-0-mini/02178405021613900000000000000000000ffffac174420e33b92.mp4?X-Tos-Algorithm=TOS4-HMAC-SHA256&X-Tos-Expires=86400&X-Tos-Signature=e7339031ac6678610d9de062632779decf75e59454834cba9eb3877375b5936f&X-Tos-SignedHeaders=host",
		},
		{
			// 跨厂商成功态（快手 Kling，2026-07-15 真机）——与火山 Seedance 完全同构，
			// 仅视频 URL 换成 aiai 自有 CDN（static1），字段仍是 task_status:"succeed" + video_result[].url。
			name:    "kling_succeed_crossvendor",
			body:    `{"model":"kling-video-v3","task_id":"01151433-67df-4622-8610-20d5a38654d0","task_status":"succeed","video_result":[{"url":"https://aiai.ac/static1/video/2026/07/15/3f11237b25339ba151b9d2ad6147cc83.mp4"}]}`,
			wantSt:  model.TaskStatusSuccess,
			wantURL: "https://aiai.ac/static1/video/2026/07/15/3f11237b25339ba151b9d2ad6147cc83.mp4",
		},
		{
			// 跨厂商失败态（xAI Grok，2026-07-15 真机）——失败信封是 task_status:"failed" +
			// 嵌套 error.message（非文档说的顶层 error_message）。errMsg() 须能从 error.message 抠出原因。
			name:       "grok_failed_crossvendor",
			body:       `{"model":"grok-imagine-video","task_id":"56115489-2ab3-484e-afb4-53c6699a8b6c","task_status":"failed","error":{"message":"video generation failed"}}`,
			wantSt:     model.TaskStatusFailure,
			wantReason: "video generation failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := a.ParseTaskResult([]byte(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.wantSt, info.Status)
			if tc.wantURL != "" {
				assert.Equal(t, tc.wantURL, info.Url, "视频 URL 必须从 video_result[0].url 抠出")
			}
			if tc.wantSt == model.TaskStatusSuccess {
				assert.Equal(t, 0, info.TotalTokens, "秒计费上游不返回 usage/token")
			}
			if tc.wantReason != "" {
				assert.Equal(t, tc.wantReason, info.Reason, "失败原因须从嵌套 error.message 抠出")
			}
		})
	}
}
