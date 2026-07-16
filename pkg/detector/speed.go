package detector

// speed.go ports the speed-benchmark idea from nexmoe/lm-speed
// (github.com/nexmoe/lm-speed, MIT, Copyright (c) 2025 Nexmoe). lm-speed times a
// non-streaming call and derives tokens/sec, so its "first token latency" is
// really the whole response time. This port improves on that by STREAMING and
// measuring the true time-to-first-token (TTFT) plus the output-phase throughput,
// which are the metrics that actually characterize relay speed. It reports the
// numbers as evidence (speed is a measurement, not a genuine/fake verdict), so
// the detector passes and surfaces a one-line summary. Attribution retained per
// the MIT license; additive, does not replace any new-api / QuantumNous identity.

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	speedPrompt     = "Explain what an API gateway does and why it is useful, in about 80 words."
	speedMaxTokens  = 256
	speedRuns       = 3
	speedRunTimeout = 60 * time.Second
)

type speedRun struct {
	ttftMs       int64
	totalMs      int64
	outputTokens int
}

type speedMetrics struct {
	runs         int
	avgTTFTMs    int64
	avgTotalMs   int64
	avgOutTokens int
	tps          float64 // output-phase tokens/sec (excludes the wait to first token)
	totalTPS     float64 // tokens/sec over the whole request
}

func computeSpeedMetrics(runs []speedRun) speedMetrics {
	n := len(runs)
	if n == 0 {
		return speedMetrics{}
	}
	var sumTTFT, sumTotal, sumOutMs int64
	sumTokens := 0
	for _, r := range runs {
		sumTTFT += r.ttftMs
		sumTotal += r.totalMs
		out := r.totalMs - r.ttftMs
		if out < 0 {
			out = 0
		}
		sumOutMs += out
		sumTokens += r.outputTokens
	}
	round2 := func(f float64) float64 { return math.Round(f*100) / 100 }
	m := speedMetrics{
		runs:         n,
		avgTTFTMs:    sumTTFT / int64(n),
		avgTotalMs:   sumTotal / int64(n),
		avgOutTokens: sumTokens / n,
	}
	if sumOutMs > 0 {
		m.tps = round2(float64(sumTokens) / (float64(sumOutMs) / 1000.0))
	}
	if sumTotal > 0 {
		m.totalTPS = round2(float64(sumTokens) / (float64(sumTotal) / 1000.0))
	}
	return m
}

func (m speedMetrics) details() map[string]interface{} {
	return map[string]interface{}{
		"runs":                    m.runs,
		"avg_ttft_ms":             m.avgTTFTMs,
		"avg_total_ms":            m.avgTotalMs,
		"avg_output_tokens":       m.avgOutTokens,
		"tokens_per_second":       m.tps,
		"tokens_per_second_total": m.totalTPS,
	}
}

func (m speedMetrics) summary() string {
	return fmt.Sprintf("首字 %dms · %.0f tok/s · 单次 %dms", m.avgTTFTMs, m.tps, m.avgTotalMs)
}

// runSpeed streams speedRuns requests, times each, and reports averaged metrics.
func runSpeed(ctx context.Context, p *prober, path string, headers map[string]string,
	makeBody func() map[string]interface{}, firstToken func(map[string]interface{}) bool,
	parseOutput func(body string) int) DetectorResult {
	var rs []speedRun
	for i := 0; i < speedRuns; i++ {
		ttft, total, body, err := p.streamTimed(ctx, path, headers, makeBody(), firstToken, speedRunTimeout)
		if err != nil {
			continue
		}
		rs = append(rs, speedRun{ttftMs: ttft, totalMs: total, outputTokens: parseOutput(body)})
	}
	if len(rs) == 0 {
		return detectorSkip("speed probes could not run")
	}
	m := computeSpeedMetrics(rs)
	details := m.details()
	details["summary"] = m.summary()
	return DetectorResult{Status: "pass", Score: 100, Details: details}
}

// --- chat (openai / gemini) ---

func chatFirstToken(o map[string]interface{}) bool {
	for _, c := range subSlice(o, "choices") {
		cm, _ := c.(map[string]interface{})
		if strField(subMap(cm, "delta"), "content") != "" {
			return true
		}
	}
	return false
}

func chatStreamOutputTokens(body string) int {
	s := openaiParseStream(sseDataObjects(body))
	if v, ok := intField(s.usage, "completion_tokens"); ok && v > 0 {
		return v
	}
	return len(strings.Fields(s.text)) // approximate when the stream omits usage
}

func chatSpeed(ctx context.Context, p *prober, cfg Config) DetectorResult {
	// Gemini streams over native streamGenerateContent; openai/grok over
	// chat/completions — each measured with its own first-token predicate and
	// output-token parser.
	if p.protocol == ProtocolGemini {
		makeBody := func() map[string]interface{} { return geminiNativeBody(speedPrompt, speedMaxTokens) }
		return runSpeed(ctx, p, geminiNativePath(cfg.Model, true), geminiNativeHeaders(p.apiKey),
			makeBody, geminiFirstToken, geminiStreamOutputTokens)
	}
	makeBody := func() map[string]interface{} {
		b := openaiPayload(cfg.Model, speedPrompt, speedMaxTokens)
		b["stream"] = true
		b["stream_options"] = map[string]interface{}{"include_usage": true}
		return b
	}
	return runSpeed(ctx, p, openaiChatPath, openaiHeaders(p.apiKey), makeBody, chatFirstToken, chatStreamOutputTokens)
}

// --- anthropic ---

func anthropicFirstToken(o map[string]interface{}) bool {
	return strField(o, "type") == "content_block_delta" && strField(subMap(o, "delta"), "type") == "text_delta"
}

func anthropicStreamOutputTokens(body string) int {
	s := parseAnthropicStream(sseDataObjects(body))
	if s.deltaOutput != nil && *s.deltaOutput > 0 {
		return *s.deltaOutput
	}
	return len(strings.Fields(s.text))
}

func anthropicSpeed(ctx context.Context, p *prober, cfg Config) DetectorResult {
	makeBody := func() map[string]interface{} {
		b := anthropicPayload(cfg.Model, speedPrompt, speedMaxTokens)
		b["stream"] = true
		return b
	}
	return runSpeed(ctx, p, anthropicMessagesPath, anthropicHeaders(p.apiKey), makeBody, anthropicFirstToken, anthropicStreamOutputTokens)
}
