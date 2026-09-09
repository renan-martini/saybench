package report

import (
	"path/filepath"
	"testing"
)

func items() []ItemResult {
	return []ItemResult{
		{Provider: "fake", Category: "names", WER: 0.2, Sub: 1, RefWords: 5, LatencyMS: 30},
		{Provider: "fake", Category: "numbers", WER: 0.0, RefWords: 10, LatencyMS: 50},
		{Provider: "fake", Category: "numbers", Error: "boom"},
		{Provider: "real", Category: "names", WER: 0.5, Sub: 1, Del: 1, RefWords: 4, LatencyMS: 200},
	}
}

func TestBuildAggregates(t *testing.T) {
	r := Build("test", "m.jsonl", items())
	if len(r.Summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(r.Summaries))
	}
	// fake: 1 edit over 15 ref words, one error, latencies 30+50
	f := r.Summaries[0]
	if f.Provider != "fake" || f.Items != 3 || f.Errors != 1 {
		t.Fatalf("fake summary wrong: %+v", f)
	}
	if want := 1.0 / 15; f.WER != want {
		t.Fatalf("fake corpus WER = %v, want %v", f.WER, want)
	}
	if f.AvgLatencyMS != 40 {
		t.Fatalf("avg latency = %d, want 40", f.AvgLatencyMS)
	}
	if got := len(r.Categories); got != 3 {
		t.Fatalf("got %d category rows, want 3", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	r := Build("test", "m.jsonl", items())
	p := filepath.Join(t.TempDir(), "r.json")
	if err := r.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != len(r.Items) || got.Summaries[0].WER != r.Summaries[0].WER {
		t.Fatal("round trip mismatch")
	}
}

func TestCompareAndRegression(t *testing.T) {
	old := Build("t", "m", []ItemResult{{Provider: "p", WER: 0.10, Sub: 1, RefWords: 10, LatencyMS: 10}})
	new_ := Build("t", "m", []ItemResult{{Provider: "p", WER: 0.20, Sub: 2, RefWords: 10, LatencyMS: 10}})
	deltas := Compare(old, new_)
	if len(deltas) != 1 {
		t.Fatalf("got %d deltas", len(deltas))
	}
	worst, who := WorstRegression(deltas)
	if who != "p" || worst < 9.9 || worst > 10.1 {
		t.Fatalf("worst regression = %v by %q, want ~10 by p", worst, who)
	}
}

func TestAllErrorsNeverReportsPerfectWER(t *testing.T) {
	r := Build("t", "m", []ItemResult{
		{Provider: "p", Error: "boom"},
		{Provider: "p", Error: "boom"},
	})
	if r.Summaries[0].WER >= 0 {
		t.Fatalf("all-error summary WER = %v, want -1 sentinel", r.Summaries[0].WER)
	}
	if got := FormatPct(r.Summaries[0].WER); got != "—" {
		t.Fatalf("FormatPct(-1) = %q, want em dash", got)
	}
	// And the regression gate must not treat unscored as a regression.
	ok := Build("t", "m", []ItemResult{{Provider: "p", WER: 0.1, Sub: 1, RefWords: 10}})
	worst, _ := WorstRegression(Compare(ok, r))
	if worst != 0 {
		t.Fatalf("unscored run counted as regression: %v", worst)
	}
}

func TestStreamingAggregation(t *testing.T) {
	items := []ItemResult{
		{Provider: "p", WER: 0.1, Sub: 1, RefWords: 10, LatencyMS: 900, TTFPartialMS: 200, FinalLagMS: 300, Interims: 5},
		{Provider: "p", WER: 0.0, RefWords: 10, LatencyMS: 1100, TTFPartialMS: 400, FinalLagMS: 500, Interims: 7},
	}
	r := BuildMode("t", "m", ModeStreaming, items)
	if r.Mode != ModeStreaming {
		t.Fatalf("mode = %q", r.Mode)
	}
	s := r.Summaries[0]
	if s.AvgTTFPartialMS != 300 || s.AvgFinalLagMS != 400 {
		t.Fatalf("streaming averages wrong: %+v", s)
	}
	if s.P95TTFPartialMS != 400 || s.P95FinalLagMS != 500 {
		t.Fatalf("streaming p95 wrong: %+v", s)
	}
}

func TestBatchModeDefaultAndRoundTrip(t *testing.T) {
	r := Build("t", "m", []ItemResult{{Provider: "p", RefWords: 5, LatencyMS: 10}})
	if r.Mode != ModeBatch {
		t.Fatalf("Build must produce batch mode, got %q", r.Mode)
	}
	p := filepath.Join(t.TempDir(), "r.json")
	if err := r.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ModeBatch {
		t.Fatalf("mode lost in round trip: %q", got.Mode)
	}
}

func TestCompareRefusesMixedModes(t *testing.T) {
	batch := Build("t", "m", []ItemResult{{Provider: "p", RefWords: 5}})
	stream := BuildMode("t", "m", ModeStreaming, []ItemResult{{Provider: "p", RefWords: 5}})
	if err := CheckComparable(batch, stream); err == nil {
		t.Fatal("expected error comparing batch vs streaming")
	}
	if err := CheckComparable(batch, batch); err != nil {
		t.Fatalf("same-mode compare must pass: %v", err)
	}
	// A pre-mode report (empty string) counts as batch.
	old := batch
	old.Mode = ""
	if err := CheckComparable(old, batch); err != nil {
		t.Fatalf("legacy empty mode must equal batch: %v", err)
	}
}
