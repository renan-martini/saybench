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
