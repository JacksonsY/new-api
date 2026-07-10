package service

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRateLimitReset(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name    string
		headers map[string]string
		wantOK  bool
		wantMin time.Duration // 允许区间下界（含）
		wantMax time.Duration // 允许区间上界（含）
	}{
		{
			name:    "retry-after seconds",
			headers: map[string]string{"Retry-After": "17"},
			wantOK:  true, wantMin: 17 * time.Second, wantMax: 17 * time.Second,
		},
		{
			name:    "retry-after http date",
			headers: map[string]string{"Retry-After": now.Add(90 * time.Second).UTC().Format(http.TimeFormat)},
			wantOK:  true, wantMin: 80 * time.Second, wantMax: 91 * time.Second,
		},
		{
			name:    "anthropic unified unix timestamp",
			headers: map[string]string{"anthropic-ratelimit-unified-reset": fmt.Sprintf("%d", now.Add(5*time.Minute).Unix())},
			wantOK:  true, wantMin: 4 * time.Minute, wantMax: 5 * time.Minute,
		},
		{
			name: "anthropic rfc3339 picks exhausted window",
			headers: map[string]string{
				"anthropic-ratelimit-requests-reset":     now.Add(10 * time.Second).Format(time.RFC3339),
				"anthropic-ratelimit-requests-remaining": "42",
				"anthropic-ratelimit-tokens-reset":       now.Add(2 * time.Minute).Format(time.RFC3339),
				"anthropic-ratelimit-tokens-remaining":   "0",
			},
			wantOK: true, wantMin: 110 * time.Second, wantMax: 2 * time.Minute,
		},
		{
			name: "openai durations without exhausted marker picks min",
			headers: map[string]string{
				"x-ratelimit-reset-requests": "6m0s",
				"x-ratelimit-reset-tokens":   "12s",
			},
			wantOK: true, wantMin: 12 * time.Second, wantMax: 12 * time.Second,
		},
		{
			name:    "retry-after takes precedence over provider pairs",
			headers: map[string]string{"Retry-After": "3", "x-ratelimit-reset-tokens": "10m"},
			wantOK:  true, wantMin: 3 * time.Second, wantMax: 3 * time.Second,
		},
		{
			name:    "expired reset is ignored",
			headers: map[string]string{"anthropic-ratelimit-requests-reset": now.Add(-time.Minute).Format(time.RFC3339)},
			wantOK:  false,
		},
		{
			name:    "no headers",
			headers: map[string]string{},
			wantOK:  false,
		},
		{
			name:    "garbage value",
			headers: map[string]string{"Retry-After": "soon-ish"},
			wantOK:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.headers {
				h.Set(k, v)
			}
			d, ok := ParseRateLimitReset(h)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.GreaterOrEqual(t, d, tc.wantMin)
				assert.LessOrEqual(t, d, tc.wantMax)
			}
		})
	}
}
