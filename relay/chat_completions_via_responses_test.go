package relay

import (
	"io"
	"math"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsResponsesEventStreamSSEBody(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantSSE bool
	}{
		{name: "event prefix", body: "event: ping\n\ndata: {}\n\n", wantSSE: true},
		{name: "data prefix", body: "data: {\"x\":1}\n\n", wantSSE: true},
		{name: "leading whitespace then data", body: "  \r\ndata: {}\n\n", wantSSE: true},
		{name: "BOM then event", body: "\xef\xbb\xbfevent: x\n\n", wantSSE: true},
		{name: "json object", body: `{"error":"nope"}`, wantSSE: false},
		{name: "json array", body: `[1,2,3]`, wantSSE: false},
		{name: "empty", body: "", wantSSE: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out io.ReadCloser
			got := isResponsesEventStreamSSEBody(io.NopCloser(strings.NewReader(tt.body)), &out)
			assert.Equal(t, tt.wantSSE, got)

			// 关键不变式：无论判定结果如何，消费过的字节都要通过 out 完整还原，
			// 下游 handler 才能读到完整原始 body。
			require.NotNil(t, out)
			restored, err := io.ReadAll(out)
			require.NoError(t, err)
			assert.Equal(t, tt.body, string(restored))
		})
	}
}

func TestIsResponsesEventStreamContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{name: "plain", contentType: "text/event-stream", want: true},
		{name: "mixed case with charset", contentType: "Text/Event-Stream; charset=utf-8", want: true},
		{name: "json", contentType: "application/json", want: false},
		{name: "empty", contentType: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isResponsesEventStreamContentType(tt.contentType))
		})
	}
}

func TestRecalcQuotaFromRatiosIgnoresInvalidMultipliers(t *testing.T) {
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			Quota: 100,
		},
	}
	info.PriceData.AddOtherRatio("duration", 2)

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{
		"duration": 3,
		"zero":     0,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	})

	require.True(t, ok)
	assert.Equal(t, 150, quota)
	assert.True(t, info.PriceData.HasOtherRatio("duration"))
}

func TestRecalcQuotaFromRatiosRejectsAllInvalidAdjustedRatios(t *testing.T) {
	info := &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			Quota: 100,
		},
	}
	info.PriceData.AddOtherRatio("duration", 2)

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{
		"zero":     0,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	})

	require.False(t, ok)
	assert.Equal(t, 0, quota)
	assert.True(t, info.PriceData.HasOtherRatio("duration"))
}
