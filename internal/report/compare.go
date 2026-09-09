package report

import "fmt"

// Delta is the change in one provider's summary between two runs.
type Delta struct {
	Provider  string  `json:"provider"`
	OldWER    float64 `json:"old_wer"`
	NewWER    float64 `json:"new_wer"`
	WERChange float64 `json:"wer_change_pp"` // percentage points, positive = worse
	OldLatMS  int64   `json:"old_avg_latency_ms"`
	NewLatMS  int64   `json:"new_avg_latency_ms"`
	OnlyInOld bool    `json:"only_in_old,omitempty"`
	OnlyInNew bool    `json:"only_in_new,omitempty"`
}

// CheckComparable rejects cross-mode comparison: batch and streaming latency
// measure different things and a delta between them is a lie.
func CheckComparable(a, b Report) error {
	ma, mb := a.Mode, b.Mode
	if ma == "" {
		ma = ModeBatch
	}
	if mb == "" {
		mb = ModeBatch
	}
	if ma != mb {
		return fmt.Errorf("cannot compare a %s report with a %s report — the latency semantics differ", ma, mb)
	}
	return nil
}

// Compare aligns two reports by provider name.
func Compare(old, cur Report) []Delta {
	oldBy := map[string]Summary{}
	for _, s := range old.Summaries {
		oldBy[s.Provider] = s
	}
	seen := map[string]bool{}
	var out []Delta
	for _, n := range cur.Summaries {
		seen[n.Provider] = true
		o, ok := oldBy[n.Provider]
		d := Delta{Provider: n.Provider, NewWER: n.WER, NewLatMS: n.AvgLatencyMS}
		if !ok {
			d.OnlyInNew = true
		} else {
			d.OldWER = o.WER
			d.OldLatMS = o.AvgLatencyMS
			d.WERChange = (n.WER - o.WER) * 100
		}
		out = append(out, d)
	}
	for _, o := range old.Summaries {
		if !seen[o.Provider] {
			out = append(out, Delta{Provider: o.Provider, OldWER: o.WER, OldLatMS: o.AvgLatencyMS, OnlyInOld: true})
		}
	}
	return out
}

// WorstRegression returns the largest WER increase in percentage points
// across providers present in both runs (0 if none got worse).
func WorstRegression(deltas []Delta) (float64, string) {
	worst, who := 0.0, ""
	for _, d := range deltas {
		if d.OnlyInOld || d.OnlyInNew || d.OldWER < 0 || d.NewWER < 0 {
			continue
		}
		if d.WERChange > worst {
			worst, who = d.WERChange, d.Provider
		}
	}
	return worst, who
}

// FormatPct renders a WER as "12.3%", or "—" for the nothing-was-scored
// sentinel (-1).
func FormatPct(w float64) string {
	if w < 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", w*100)
}

// ConditionNote returns a human warning when two same-mode reports were
// measured under different conditions (today: warmup posture). Empty when
// the comparison is clean.
func ConditionNote(a, b Report) string {
	if a.S2SScoring != b.S2SScoring && (a.S2SScoring != "" || b.S2SScoring != "") {
		return fmt.Sprintf("warning: comparing a %q s2s run with a %q s2s run — the tasks differ (echo replies are short and scored; conversational replies are free-form), so latency and speech-out deltas mix conditions", orConv(a.S2SScoring), orConv(b.S2SScoring))
	}
	if a.S2STurnEnding != b.S2STurnEnding && (a.S2STurnEnding != "" || b.S2STurnEnding != "") {
		return fmt.Sprintf("warning: comparing s2s runs with different turn-ending (%q vs %q) — server_vad V2V includes VAD hangover; deltas mix semantics", orCommit(a.S2STurnEnding), orCommit(b.S2STurnEnding))
	}
	if a.Normalization != b.Normalization {
		return fmt.Sprintf("warning: comparing runs with different scoring normalization (%q vs %q) — WER deltas mix conditions", a.Normalization, b.Normalization)
	}
	if a.Warmup != b.Warmup {
		name := func(w bool) string {
			if w {
				return "warm (with warmup)"
			}
			return "cold (no warmup)"
		}
		return fmt.Sprintf("warning: comparing a %s run with a %s run — cold TLS setup is a multi-x TTFT effect; deltas below mix conditions", name(a.Warmup), name(b.Warmup))
	}
	return ""
}

func orConv(s string) string {
	if s == "" {
		return "conversational"
	}
	return s
}

func orCommit(s string) string {
	if s == "" {
		return "commit"
	}
	return s
}
