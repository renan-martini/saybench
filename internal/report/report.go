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
const SchemaVersion = 1

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
	Error      string  `json:"error,omitempty"`
}

// Summary aggregates one provider across the corpus. WER is corpus-level:
// total edits over total reference words, not an average of per-clip rates —
// so long clips weigh more, which is the standard convention.
type Summary struct {
	Provider     string  `json:"provider"`
	Items        int     `json:"items"`
	Errors       int     `json:"errors"`
	WER          float64 `json:"wer"`
	AvgLatencyMS int64   `json:"avg_latency_ms"`
	P95LatencyMS int64   `json:"p95_latency_ms"`
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
	SchemaVersion int               `json:"schema_version"`
	Tool          string            `json:"tool"`
	ToolVersion   string            `json:"tool_version"`
	CreatedAt     time.Time         `json:"created_at"`
	Manifest      string            `json:"manifest"`
	Summaries     []Summary         `json:"summaries"`
	Categories    []CategorySummary `json:"categories"`
	Items         []ItemResult      `json:"items"`
}

// Build assembles a report, computing summaries from items.
func Build(toolVersion, manifestPath string, items []ItemResult) Report {
	type agg struct {
		edits, words, errs, n int
		latencies             []int64
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
			x.latencies = append(x.latencies, it.LatencyMS)
		}
	}

	rate := func(a *agg) float64 {
		if a.words == 0 {
			return 0
		}
		return float64(a.edits) / float64(a.words)
	}

	var summaries []Summary
	for p, a := range byProvider {
		s := Summary{Provider: p, Items: a.n, Errors: a.errs, WER: rate(a)}
		if len(a.latencies) > 0 {
			sort.Slice(a.latencies, func(i, j int) bool { return a.latencies[i] < a.latencies[j] })
			var sum int64
			for _, l := range a.latencies {
				sum += l
			}
			s.AvgLatencyMS = sum / int64(len(a.latencies))
			s.P95LatencyMS = a.latencies[(len(a.latencies)-1)*95/100]
		}
		summaries = append(summaries, s)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Provider < summaries[j].Provider })

	var cats []CategorySummary
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
