package detector

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"
)

// Long-context needle-in-haystack detection (Veridrop core/long_context.py +
// per-protocol detectors). Probes whether a relay honors the model's advertised
// context window or silently truncates / routes to a smaller-window model.
// Opt-in (Config.IncludeLongContext); standard tiers 32k/100k/200k capped at the
// model's context limit. The count_tokens precise-sizing and adaptive "extreme"
// tiers of the reference are intentionally not ported (standard tiers catch the
// overwhelming majority of relay truncation).

const (
	lcMaxOutputTokens  = 256
	lcQuestionBuffer   = 1500
	lcPassThreshold    = 3
	lcPartialThreshold = 2
)

var lcStandardTiers = []int{32_000, 100_000, 200_000}

// tiersForModel ports core/long_context.tiers_for_model: adaptive tiers covering
// low/mid/high of the model's context window (~32k / 50% / 95%), deduping tiers
// within 1.5x. For a 1M model this yields roughly (32k, 500k, 950k) — the
// "极限档" that catches "advertised X but actually Y<X" fraud a 200k-capped
// standard run is blind to. Skips tiers below 1k.
func tiersForModel(ctxLimit int) []int {
	raw := []int{
		min32k(ctxLimit),
		int(float64(ctxLimit) * 0.50),
		int(float64(ctxLimit) * 0.95),
	}
	var out []int
	for _, t := range raw {
		if t < 1_000 {
			continue
		}
		if len(out) == 0 || float64(t) >= float64(out[len(out)-1])*1.5 {
			out = append(out, t)
		}
	}
	return out
}

func min32k(ctxLimit int) int {
	v := int(float64(ctxLimit) * 0.30)
	if v > 32_000 {
		return 32_000
	}
	return v
}

var lcCharsPerToken = map[string]float64{
	ProtocolOpenAI:    6.0,
	ProtocolAnthropic: 5.3,
	ProtocolGemini:    5.5,
}

func charsPerToken(protocol string) float64 {
	if v, ok := lcCharsPerToken[strings.ToLower(protocol)]; ok {
		return v
	}
	return 6.0
}

// lcModelContextLimits maps model alias prefixes to advertised input context, so
// the detector never probes past a model's real limit (which would 400 and be
// misread as relay truncation). 128k conservative default for unknowns.
var lcModelContextLimits = map[string]int{
	"gpt-3.5-turbo": 16_385, "gpt-4o-mini": 128_000, "gpt-4o": 128_000,
	"gpt-4.1-mini": 1_047_576, "gpt-4.1-nano": 1_047_576, "gpt-4.1": 1_047_576,
	"gpt-5-nano": 272_000, "gpt-5-mini": 272_000, "gpt-5": 272_000,
	"o1-mini": 128_000, "o1": 200_000, "o3-mini": 200_000, "o3": 200_000,
	"claude-haiku-4-5": 200_000, "claude-sonnet-4-6": 1_000_000,
	"claude-opus-4-8": 1_000_000, "claude-opus-4-7": 1_000_000, "claude-opus-4-6": 1_000_000,
	"claude-sonnet-4-5": 200_000, "claude-opus-4-5": 200_000, "claude-opus-4-1": 200_000,
	"claude-sonnet-4": 200_000, "claude-opus-4": 200_000,
	"gemini-3.1-pro": 1_048_576, "gemini-3-flash": 1_048_576, "gemini-3-pro": 1_048_576,
	"gemini-2.5-flash": 1_048_576, "gemini-2.5-pro": 1_048_576,
	"gemini-1.5-flash": 1_048_576, "gemini-1.5-pro": 1_048_576,
}

func modelContextLimit(model string) int {
	if model == "" {
		return 128_000
	}
	if v, ok := lcModelContextLimits[model]; ok {
		return v
	}
	// Longest-prefix wins so "claude-opus-4-8-…" resolves to claude-opus-4-8 (1M)
	// rather than the shorter claude-opus-4 (200k). Go map iteration is random,
	// so we cannot rely on insertion order like the Python reference does.
	best, bestLen := 128_000, -1
	for prefix, limit := range lcModelContextLimits {
		if len(prefix) > bestLen && strings.HasPrefix(model, prefix) {
			best, bestLen = limit, len(prefix)
		}
	}
	return best
}

type needle struct {
	positionPct float64
	sentence    string
	answer      string
	label       string
}

func seededRand(s string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

func randID(rng *rand.Rand, prefix string) string {
	return fmt.Sprintf("%s-%04X-%04X", prefix, rng.Intn(0x10000), rng.Intn(0x10000))
}

// makeNeedles builds three deterministic needles (seeded) at 10/50/90% depth.
func makeNeedles(seed string) []needle {
	rng := seededRand(seed)
	passcode := randID(rng, "ALPHA")
	codename := randID(rng, "ATLAS")
	contact := randID(rng, "BRAVO")
	return []needle{
		{0.10, "Important administrative note: the conference room access passcode for the duration of this document is " + passcode + ". All attendees should retain this code for badge verification.", passcode, "conference room access passcode"},
		{0.50, "Editor's note inserted mid-document: the internal project codename for the upcoming deliverable is " + codename + ". This codename supersedes any prior placeholder identifiers.", codename, "internal project codename"},
		{0.90, "Final operational note before closing: in case of emergency, the on-call engineer's reference identifier is " + contact + ". This identifier should be quoted in all incident reports.", contact, "on-call engineer reference identifier"},
	}
}

func buildQuestion(needles []needle) string {
	var b strings.Builder
	b.WriteString("\n\n---\nCarefully review the document above and locate the following identifiers, which were embedded as administrative notes:\n")
	for _, n := range needles {
		b.WriteString("  - The " + n.label + "\n")
	}
	b.WriteString("\nReply with each identifier on its own line, in the order asked. If any identifier is not present in the document, write 'NOT FOUND' for that one. Do not guess or invent identifiers.")
	return b.String()
}

// countRecalls returns how many needle answers appear verbatim (case-insensitive)
// in the model's response.
func countRecalls(text string, needles []needle) int {
	if text == "" {
		return 0
	}
	up := strings.ToUpper(text)
	found := 0
	for _, n := range needles {
		if strings.Contains(up, strings.ToUpper(n.answer)) {
			found++
		}
	}
	return found
}

var lcVocab = map[string][]string{
	"company":    {"Acme Industries", "Blackstar Logistics", "Cardinal Robotics", "Delphi Technologies", "Evergreen Pharmaceuticals", "Falcon Aerospace", "Granite Holdings", "Halcyon Capital", "Iron Peak Mining", "Juno Biosciences", "Kestrel Manufacturing", "Lighthouse Energy", "Meridian Foods", "Northgate Construction", "Olympia Software"},
	"department": {"finance", "operations", "research and development", "human resources", "legal", "marketing", "supply chain", "engineering", "compliance"},
	"metric":     {"operating revenue", "gross margin", "year-over-year growth", "customer acquisition cost", "throughput rate", "cycle time", "defect density", "utilization", "satisfaction score"},
	"city":       {"Geneva", "Singapore", "Buenos Aires", "Reykjavik", "Cape Town", "Vancouver", "Tashkent", "Auckland", "Edinburgh", "Marrakech", "Stockholm", "Hanoi", "Lima", "Tbilisi", "Kuala Lumpur"},
	"topic":      {"supply chain resilience", "regulatory compliance", "energy efficiency", "talent retention", "data governance", "risk management", "operational continuity", "quality assurance", "stakeholder engagement"},
	"verb":       {"outlined", "evaluated", "presented", "documented", "summarized", "introduced", "compared", "analyzed", "described", "highlighted"},
}

var lcTemplates = []string{
	"In the {quarter} quarter of fiscal year {year}, the {department} department of {company} {verb} a {metric} review covering {topic}, with findings circulated to leadership for ratification.",
	"At the {year} industry symposium held in {city}, delegates {verb} the role of {topic} in shaping medium-term policy, citing data from the {company} compliance archive and the {department} working group.",
	"The internal {department} memorandum dated {month} {year} {verb} revisions to {company}'s {topic} guidelines, intended to align with the upcoming {metric} reporting cycle.",
	"Researchers at {company} {verb} the relationship between {topic} and {metric}, documenting interim results in a working paper circulated within the {department} unit during {quarter} of {year}.",
	"A cross-functional task force consisting of {department} and operations staff at {company} {verb} the {year} {topic} report, noting that the {metric} trajectory diverged from earlier projections.",
}

func pick(rng *rand.Rand, key string) string {
	l := lcVocab[key]
	return l[rng.Intn(len(l))]
}

func fillerSentence(rng *rand.Rand) string {
	tmpl := lcTemplates[rng.Intn(len(lcTemplates))]
	months := []string{"January", "March", "May", "July", "September", "November"}
	quarters := []string{"first", "second", "third", "fourth"}
	r := strings.NewReplacer(
		"{quarter}", quarters[rng.Intn(len(quarters))],
		"{year}", fmt.Sprintf("%d", 2014+rng.Intn(13)),
		"{company}", pick(rng, "company"),
		"{department}", pick(rng, "department"),
		"{metric}", pick(rng, "metric"),
		"{city}", pick(rng, "city"),
		"{topic}", pick(rng, "topic"),
		"{verb}", pick(rng, "verb"),
		"{month}", months[rng.Intn(len(months))],
	)
	return r.Replace(tmpl)
}

// assembleHaystack builds ~targetTokens of synthetic filler with the needles
// inserted at their target depths.
func assembleHaystack(targetTokens int, needles []needle, seed, protocol string) string {
	targetChars := int(float64(targetTokens) * charsPerToken(protocol))
	rng := seededRand(seed + ":haystack")
	var sentences []string
	charCount := 0
	for charCount < targetChars {
		s := fillerSentence(rng)
		sentences = append(sentences, s)
		charCount += len(s) + 1
	}
	type insert struct {
		idx int
		s   string
	}
	inserts := make([]insert, 0, len(needles))
	for _, n := range needles {
		inserts = append(inserts, insert{int(n.positionPct * float64(len(sentences))), n.sentence})
	}
	sort.Slice(inserts, func(i, j int) bool { return inserts[i].idx > inserts[j].idx }) // desc
	for _, ins := range inserts {
		idx := ins.idx
		if idx > len(sentences) {
			idx = len(sentences)
		}
		sentences = append(sentences[:idx], append([]string{ins.s}, sentences[idx:]...)...)
	}
	return strings.Join(sentences, " ")
}

// lcTier is one tier's outcome.
type lcTier struct {
	target int
	found  int
	total  int
	status string // pass | partial | fail | skip | rate_limited
}

var lcRateLimitMarkers = []string{"http 429", "rate limit", "rate_limit_exceeded", "tokens per min", "tpm", "requests per min", "overloaded_error"}

func looksRateLimited(msg string) bool {
	if msg == "" {
		return false
	}
	l := strings.ToLower(msg)
	for _, m := range lcRateLimitMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// longContextSend probes the relay with the full haystack prompt and returns the
// recall text. ok=false + errMsg on failure.
type longContextSend func(prompt string) (text string, ok bool, errMsg string)

// runLongContext orchestrates the standard-tier needle probes and aggregates.
func runLongContext(cfg Config, protocol string, send longContextSend) DetectorResult {
	seed := cfg.BaseURL + ":" + cfg.Model
	ctxLimit := modelContextLimit(cfg.Model)

	// 极限档：按模型上限自适应档（~32k/50%/95%）；否则标准档 32k/100k/200k。
	tierSet := lcStandardTiers
	strategy := "standard"
	if cfg.IncludeLongContextExtreme {
		tierSet = tiersForModel(ctxLimit)
		strategy = "extreme"
	}

	var tiers []lcTier
	for _, target := range tierSet {
		if target > ctxLimit {
			tiers = append(tiers, lcTier{target, 0, 3, "skip"})
			continue
		}
		tierSeed := fmt.Sprintf("%s:%d", seed, target)
		needles := makeNeedles(tierSeed)
		haystack := assembleHaystack(target-lcQuestionBuffer, needles, tierSeed, protocol)
		text, ok, errMsg := send(haystack + buildQuestion(needles))
		if !ok {
			if looksRateLimited(errMsg) {
				tiers = append(tiers, lcTier{target, 0, 3, "rate_limited"})
			} else {
				tiers = append(tiers, lcTier{target, 0, 3, "fail"})
			}
			break // stop probing higher tiers
		}
		found := countRecalls(text, needles)
		status := "fail"
		if found >= lcPassThreshold {
			status = "pass"
		} else if found >= lcPartialThreshold {
			status = "partial"
		}
		tiers = append(tiers, lcTier{target, found, 3, status})
		if status != "pass" {
			break // fail/partial: don't probe higher tiers
		}
	}

	score, status, summary := aggregateLongContext(tiers)
	tierDetails := make([]map[string]interface{}, 0, len(tiers))
	for _, t := range tiers {
		tierDetails = append(tierDetails, map[string]interface{}{
			"target_tokens": t.target, "needles_found": t.found,
			"needles_total": t.total, "status": t.status,
		})
	}
	return DetectorResult{Status: status, Score: score, Details: map[string]interface{}{
		"summary": summary, "tiers_tested": tierDetails, "tier_strategy": strategy,
		"model_context_limit": ctxLimit, "opt_in": true,
	}}
}

// aggregateLongContext ports _aggregate: average of probed tiers (pass 100 /
// partial 66 / fail 0), status fail on any truncation (fail tier), else pass;
// skip when nothing conclusive was probed.
func aggregateLongContext(tiers []lcTier) (float64, string, string) {
	if len(tiers) == 0 {
		return 0, "error", "no tiers probed"
	}
	var probed []lcTier
	rateLimited := false
	for _, t := range tiers {
		switch t.status {
		case "skip":
		case "rate_limited":
			rateLimited = true
		default:
			probed = append(probed, t)
		}
	}
	if len(probed) == 0 {
		if rateLimited {
			return 0, "skip", "long-context probe hit an upstream rate limit (TPM/RPM), not a relay defect — retry later"
		}
		return 0, "skip", "model context limit is below the minimum 32k tier; skipped"
	}

	sum := 0.0
	hasFail := false
	var failTier lcTier
	for _, t := range probed {
		switch t.status {
		case "pass":
			sum += 100
		case "partial":
			sum += 66
		default:
			sum += 0
			if !hasFail {
				hasFail = true
				failTier = t
			}
		}
	}
	score := sum / float64(len(probed))
	if hasFail {
		return score, "fail", fmt.Sprintf("recall failed at %dk tokens (%d/%d needles) — the relay likely truncates or routes to a smaller-window model at this scale", failTier.target/1000, failTier.found, failTier.total)
	}
	highest := probed[len(probed)-1].target / 1000
	return score, "pass", fmt.Sprintf("passed long-context detection up to %dk tokens with no truncation evidence", highest)
}

// longContextDetector is the shared entry wired into the anthropic/openai
// registries. It self-skips unless opted in, then dispatches the protocol-
// specific probe.
func longContextDetector(ctx context.Context, p *prober, cfg Config) DetectorResult {
	if !cfg.IncludeLongContext && !cfg.IncludeLongContextExtreme {
		return detectorSkip("long-context detection is opt-in")
	}
	if cfg.Protocol == ProtocolAnthropic {
		return runLongContext(cfg, ProtocolAnthropic, func(prompt string) (string, bool, string) {
			body := map[string]interface{}{
				"model": cfg.Model, "max_tokens": lcMaxOutputTokens,
				"messages": []map[string]interface{}{{"role": "user", "content": prompt}},
			}
			if !anthropicOmitsTemperature(cfg.Model) {
				body["temperature"] = 0
			}
			res := p.postJSON(ctx, anthropicMessagesPath, anthropicHeaders(p.apiKey), body)
			if res.err != nil {
				return "", false, res.err.Error()
			}
			if !res.ok() {
				return "", false, upstreamErrorText(res)
			}
			return anthropicText(res.parsed), true, ""
		})
	}
	return runLongContext(cfg, cfg.Protocol, func(prompt string) (string, bool, string) {
		res := p.postJSON(ctx, openaiChatPath, openaiHeaders(p.apiKey), openaiPayload(cfg.Model, prompt, lcMaxOutputTokens))
		if res.err != nil {
			return "", false, res.err.Error()
		}
		if !res.ok() {
			return "", false, upstreamErrorText(res)
		}
		return openaiContent(res.parsed), true, ""
	})
}
