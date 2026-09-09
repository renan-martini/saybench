// Package report defines the benchmark report schema, its aggregation, and
// run-to-run comparison. Reports are plain JSON so anything can consume them.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// SchemaVersion is bumped on breaking changes to the report format.
// Streaming fields (mode, ttfp, final lag) were additive and did not bump it.
const SchemaVersion = 1

// Report modes. They measure different things and must never be blended:
// batch latency includes upload of the whole file; streaming latency is
// about partials and finalization under real-time pacing.
const (
	ModeBatch     = "batch"
	ModeStreaming = "streaming"
	ModeLLM       = "llm"
	ModeS2S       = "s2s"
	ModeTTS       = "tts"
)

// ItemResult is one (provider, clip) outcome.
type ItemResult struct {
	Provider   string  `json:"provider"`
	Audio      string  `json:"audio"`
	Category   string  `json:"category"`
	Reference  string  `json:"reference"`
	Hypothesis string  `json:"hypothesis,omitempty"`
	WER        float64 `json:"wer"`
	Sub        int     `json:"sub"`
	Del        int     `json:"del"`
	Ins        int     `json:"ins"`
	RefWords   int     `json:"ref_words"`
	LatencyMS  int64   `json:"latency_ms"`
	// Streaming-mode fields (zero in batch mode).
	TTFPartialMS int `json:"ttf_partial_ms,omitempty"`
	FinalLagMS   int `json:"final_lag_ms,omitempty"`
	Interims     int `json:"interims,omitempty"`
	// Interim word survival: of the final's distinct words, how many any
	// interim previewed. Zero totals mean "no interims" — not a bad score.
	InterimSurvivalHit   int `json:"interim_survival_hit,omitempty"`
	InterimSurvivalTotal int `json:"interim_survival_total,omitempty"`
	// LLM-mode fields.
	Prompt       string `json:"prompt,omitempty"` // scenario name
	TTFTMS       int    `json:"ttft_ms,omitempty"`
	CompletionMS int    `json:"completion_ms,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	// TTS-mode field (CompletionMS and OutputAudioMS are shared).
	TTFAudioMS int `json:"ttf_audio_ms,omitempty"`
	// S2S-mode fields: the turn-taking numbers.
	V2VFirstAudioMS int `json:"v2v_first_audio_ms,omitempty"`
	ResponseDoneMS  int `json:"response_done_ms,omitempty"`
	OutputAudioMS   int `json:"output_audio_ms,omitempty"`
	// Keyterm recall: of the clip's important terms, how many survived
	// transcription intact. MissedKeyterms names the casualties.
	KeytermsTotal  int      `json:"keyterms_total,omitempty"`
	KeytermsHit    int      `json:"keyterms_hit,omitempty"`
	MissedKeyterms []string `json:"missed_keyterms,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// Summary aggregates one provider across the corpus. WER is corpus-level:
// total edits over total reference words, not an average of per-clip rates —
// so long clips weigh more, which is the standard convention. When every item
// errored there is no corpus to score: WER is -1 and renders as "—", never as
// a flattering 0%.
type Summary struct {
	Provider string  `json:"provider"`
	Items    int     `json:"items"`
	Errors   int     `json:"errors"`
	WER      float64 `json:"wer"`
	// KeytermRecall is corpus-level (total hits over total terms); -1 when
	// the corpus defines no keyterms.
	KeytermRecall float64 `json:"keyterm_recall"`
	AvgLatencyMS  int64   `json:"avg_latency_ms"`
	P95LatencyMS  int64   `json:"p95_latency_ms"`
	// Streaming-mode aggregates (zero in batch mode). InterimWordSurvival
	// is word-weighted (sum hits over sum totals); -1 = no interim data.
	InterimWordSurvival float64 `json:"interim_word_survival,omitempty"`
	AvgTTFPartialMS     int64   `json:"avg_ttf_partial_ms,omitempty"`
	P95TTFPartialMS     int64   `json:"p95_ttf_partial_ms,omitempty"`
	AvgFinalLagMS       int64   `json:"avg_final_lag_ms,omitempty"`
	P95FinalLagMS       int64   `json:"p95_final_lag_ms,omitempty"`
	// LLM-mode aggregates.
	AvgTTFTMS       int64   `json:"avg_ttft_ms,omitempty"`
	P95TTFTMS       int64   `json:"p95_ttft_ms,omitempty"`
	AvgCompletionMS int64   `json:"avg_completion_ms,omitempty"`
	AvgTokensPerSec float64 `json:"avg_tokens_per_sec,omitempty"`
	// TTS-mode aggregates.
	AvgTTFAudioMS int64 `json:"avg_ttf_audio_ms,omitempty"`
	P95TTFAudioMS int64 `json:"p95_ttf_audio_ms,omitempty"`
	// S2S-mode aggregates.
	AvgV2VFirstAudioMS int64 `json:"avg_v2v_first_audio_ms,omitempty"`
	P95V2VFirstAudioMS int64 `json:"p95_v2v_first_audio_ms,omitempty"`
	AvgResponseDoneMS  int64 `json:"avg_response_done_ms,omitempty"`
	AvgOutputAudioMS   int64 `json:"avg_output_audio_ms,omitempty"`
}

// CategorySummary aggregates one provider within one failure-mode category.
type CategorySummary struct {
	Provider string  `json:"provider"`
	Category string  `json:"category"`
	Items    int     `json:"items"`
	WER      float64 `json:"wer"`
}

// Report is a full benchmark run.
type Report struct {
	SchemaVersion int    `json:"schema_version"`
	Mode          string `json:"mode,omitempty"` // empty in old files = batch
	// Warmup records whether targets got an unmeasured throwaway request
	// before measurement (llm mode). Cold and warm runs are different
	// experiments; compare warns when they meet.
	Warmup bool `json:"warmup,omitempty"`
	// S2SScoring records the s2s task condition: "conversational" (latency
	// only) or "echo" (comprehension-scored). Different experiments;
	// compare warns when they meet.
	S2SScoring string `json:"s2s_scoring,omitempty"`
	// Normalization records the scoring canonicalization ("", "digits").
	// A different normalization is a different experiment.
	Normalization string            `json:"normalization,omitempty"`
	Tool          string            `json:"tool"`
	ToolVersion   string            `json:"tool_version"`
	CreatedAt     time.Time         `json:"created_at"`
	Manifest      string            `json:"manifest"`
	Summaries     []Summary         `json:"summaries"`
	Categories    []CategorySummary `json:"categories"`
	Items         []ItemResult      `json:"items"`
}

// Build assembles a batch-mode report, computing summaries from items.
func Build(toolVersion, manifestPath string, items []ItemResult) Report {
	return BuildMode(toolVersion, manifestPath, ModeBatch, items)
}

// BuildMode assembles a report in the given mode.
func BuildMode(toolVersion, manifestPath, mode string, items []ItemResult) Report {
	type agg struct {
		edits, words, errs, n int
		ktHit, ktTotal        int
		survHit, survTotal    int
		latencies             []int64
		ttfps, lags           []int64
		ttfts, completions    []int64
		tokRates              []float64
		v2vs, dones, outAudio []int64
		ttfas, ttsTotals      []int64
	}
	byProvider := map[string]*agg{}
	type catKey struct{ p, c string }
	byCat := map[catKey]*agg{}

	for _, it := range items {
		a := byProvider[it.Provider]
		if a == nil {
			a = &agg{}
			byProvider[it.Provider] = a
		}
		ca := byCat[catKey{it.Provider, it.Category}]
		if ca == nil {
			ca = &agg{}
			byCat[catKey{it.Provider, it.Category}] = ca
		}
		for _, x := range []*agg{a, ca} {
			x.n++
			if it.Error != "" {
				x.errs++
				continue
			}
			x.edits += it.Sub + it.Del + it.Ins
			x.words += it.RefWords
			x.ktHit += it.KeytermsHit
			x.ktTotal += it.KeytermsTotal
			x.latencies = append(x.latencies, it.LatencyMS)
			if mode == ModeStreaming {
				x.ttfps = append(x.ttfps, int64(it.TTFPartialMS))
				x.lags = append(x.lags, int64(it.FinalLagMS))
				x.survHit += it.InterimSurvivalHit
				x.survTotal += it.InterimSurvivalTotal
			}
			if mode == ModeLLM {
				x.ttfts = append(x.ttfts, int64(it.TTFTMS))
				x.completions = append(x.completions, int64(it.CompletionMS))
				if it.OutputTokens > 0 && it.CompletionMS > it.TTFTMS {
					decode := float64(it.CompletionMS-it.TTFTMS) / 1000
					x.tokRates = append(x.tokRates, float64(it.OutputTokens)/decode)
				}
			}
			if mode == ModeS2S {
				x.v2vs = append(x.v2vs, int64(it.V2VFirstAudioMS))
				x.dones = append(x.dones, int64(it.ResponseDoneMS))
				x.outAudio = append(x.outAudio, int64(it.OutputAudioMS))
			}
			if mode == ModeTTS {
				x.ttfas = append(x.ttfas, int64(it.TTFAudioMS))
				x.ttsTotals = append(x.ttsTotals, int64(it.CompletionMS))
				x.outAudio = append(x.outAudio, int64(it.OutputAudioMS))
			}
		}
	}

	rate := func(a *agg) float64 {
		if a.words == 0 {
			if a.errs > 0 || mode == ModeS2S {
				// Nothing was scored (errors, or an unscored s2s
				// condition) — never report a perfect 0%.
				return -1
			}
			return 0
		}
		return float64(a.edits) / float64(a.words)
	}

	recall := func(a *agg) float64 {
		if a.ktTotal == 0 {
			return -1
		}
		return float64(a.ktHit) / float64(a.ktTotal)
	}

	var summaries []Summary
	for p, a := range byProvider {
		s := Summary{Provider: p, Items: a.n, Errors: a.errs, WER: rate(a), KeytermRecall: recall(a)}
		s.AvgLatencyMS, s.P95LatencyMS = avgP95(a.latencies)
		s.AvgTTFPartialMS, s.P95TTFPartialMS = avgP95(a.ttfps)
		s.AvgFinalLagMS, s.P95FinalLagMS = avgP95(a.lags)
		if mode == ModeStreaming {
			s.InterimWordSurvival = -1
			if a.survTotal > 0 {
				s.InterimWordSurvival = float64(a.survHit) / float64(a.survTotal)
			}
		}
		if mode == ModeLLM {
			s.AvgTTFTMS, s.P95TTFTMS = avgP95(a.ttfts)
			s.AvgCompletionMS, _ = avgP95(a.completions)
			if len(a.tokRates) > 0 {
				var sum float64
				for _, r := range a.tokRates {
					sum += r
				}
				s.AvgTokensPerSec = sum / float64(len(a.tokRates))
			}
		}
		if mode == ModeS2S {
			s.AvgV2VFirstAudioMS, s.P95V2VFirstAudioMS = avgP95(a.v2vs)
			s.AvgResponseDoneMS, _ = avgP95(a.dones)
			s.AvgOutputAudioMS, _ = avgP95(a.outAudio)
		}
		if mode == ModeTTS {
			s.AvgTTFAudioMS, s.P95TTFAudioMS = avgP95(a.ttfas)
			s.AvgCompletionMS, _ = avgP95(a.ttsTotals)
			s.AvgOutputAudioMS, _ = avgP95(a.outAudio)
		}
		summaries = append(summaries, s)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Provider < summaries[j].Provider })

	var cats []CategorySummary
	if mode == ModeLLM || mode == ModeS2S || mode == ModeTTS {
		byCat = nil // per-category WER does not exist here; item rows keep categories
	}
	for k, a := range byCat {
		cats = append(cats, CategorySummary{Provider: k.p, Category: k.c, Items: a.n, WER: rate(a)})
	}
	sort.Slice(cats, func(i, j int) bool {
		if cats[i].Provider != cats[j].Provider {
			return cats[i].Provider < cats[j].Provider
		}
		return cats[i].Category < cats[j].Category
	})

	return Report{
		SchemaVersion: SchemaVersion,
		Mode:          mode,
		Tool:          "saybench",
		ToolVersion:   toolVersion,
		CreatedAt:     time.Now().UTC(),
		Manifest:      manifestPath,
		Summaries:     summaries,
		Categories:    cats,
		Items:         items,
	}
}

// Save writes the report as indented JSON.
func (r Report) Save(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// LoadFile reads a report back and checks the schema version.
func LoadFile(path string) (Report, error) {
	var r Report
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	if r.SchemaVersion != SchemaVersion {
		return r, fmt.Errorf("%s: schema version %d, this build reads %d", path, r.SchemaVersion, SchemaVersion)
	}
	return r, nil
}

// avgP95 uses the nearest-rank definition for p95: the smallest value with
// at least 95% of observations at or below it. For small n this is the max,
// which is the honest reading of "p95" on a 14-clip corpus.
func avgP95(v []int64) (avg, p95 int64) {
	if len(v) == 0 {
		return 0, 0
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	var sum int64
	for _, x := range v {
		sum += x
	}
	idx := (95*len(v) + 99) / 100 // ceil(0.95n)
	return sum / int64(len(v)), v[idx-1]
}
