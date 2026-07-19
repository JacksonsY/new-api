package xai

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBillingContext(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", req)
	return ctx
}

// EstimateBilling 折算出的是相对基准价的总计费单位:
// 秒数 × 分辨率倍率 + 图片张数 × 0.02(每张 $0.002 / 基准每秒 $0.1)。
// 默认分辨率 720p 的倍率为 0.7。
func TestEstimateBillingTotalUnits(t *testing.T) {
	tests := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want float64
	}{
		{
			name: "duration from request",
			req:  relaycommon.TaskSubmitReq{Duration: 10},
			want: 10 * 0.7,
		},
		{
			name: "metadata duration overrides request",
			req: relaycommon.TaskSubmitReq{
				Duration: 6,
				Metadata: map[string]any{"duration": 12},
			},
			want: 12 * 0.7,
		},
		{
			name: "resolution drives the multiplier",
			req: relaycommon.TaskSubmitReq{
				Duration: 10,
				Metadata: map[string]any{"resolution": "1080p"},
			},
			want: 10 * 1.0,
		},
		{
			name: "input image adds a surcharge",
			req: relaycommon.TaskSubmitReq{
				Duration: 10,
				Images:   []string{"data:image/png;base64,aVZCTw=="},
			},
			want: 10*0.7 + 0.02,
		},
		{
			// 上游只接受一张输入图,BuildRequestBody 也只发 Images[0]。多传的图片
			// 不产生成本,不能按张数收费——否则传 N 张就被收 N 份。
			name: "extra images are not charged",
			req: relaycommon.TaskSubmitReq{
				Duration: 10,
				Images: []string{
					"data:image/png;base64,aVZCTw==",
					"data:image/png;base64,aVZCTw==",
					"data:image/png;base64,aVZCTw==",
				},
			},
			want: 10*0.7 + 0.02,
		},
	}

	adaptor := &TaskAdaptor{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratios := adaptor.EstimateBilling(newBillingContext(t, tt.req), nil)
			require.Contains(t, ratios, "total_units")
			assert.InDelta(t, tt.want, ratios["total_units"], 1e-9)
		})
	}
}

// 时长是计费乘数,任何来源都必须钳到 MaxTaskDurationSeconds,
// 否则用户可用超大 duration 放大额度（AGENTS.md 计费安全不变量）。
func TestEstimateBillingClampsDuration(t *testing.T) {
	wantCap := float64(relaycommon.MaxTaskDurationSeconds) * 0.7

	tests := []struct {
		name string
		req  relaycommon.TaskSubmitReq
	}{
		{
			name: "float metadata duration",
			req:  relaycommon.TaskSubmitReq{Metadata: map[string]any{"duration": 1e9}},
		},
		{
			name: "int metadata duration",
			req:  relaycommon.TaskSubmitReq{Metadata: map[string]any{"duration": 1 << 40}},
		},
		{
			name: "string metadata duration",
			req:  relaycommon.TaskSubmitReq{Metadata: map[string]any{"duration": "99999999"}},
		},
		{
			name: "request duration field",
			req:  relaycommon.TaskSubmitReq{Duration: 1 << 30},
		},
		{
			name: "seconds field",
			req:  relaycommon.TaskSubmitReq{Seconds: "99999999"},
		},
	}

	adaptor := &TaskAdaptor{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratios := adaptor.EstimateBilling(newBillingContext(t, tt.req), nil)
			assert.InDelta(t, wantCap, ratios["total_units"], 1e-9)
		})
	}
}
