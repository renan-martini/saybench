package pricing

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/renan-martini/saybench/internal/report"
)

func writeTable(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pricing.json")
	os.WriteFile(p, []byte(`{
	  "providers": {
	    "deepgram-stream:nova-3": {"per_audio_min": 0.0060},
	    "openai:gpt-4o-mini":     {"per_1m_input_tokens": 0.15, "per_1m_output_tokens": 0.60},
	    "openai:tts-x":           {"per_1m_chars": 12.0}
	  }
	}`), 0o644)
	return p
}

func TestCostMath(t *testing.T) {
	tbl, err := Load(writeTable(t))
	if err != nil {
		t.Fatal(err)
	}
	// 2 minutes of audio at $0.006/min
	audio := report.ItemResult{Provider: "deepgram-stream:nova-3", AudioDurationMS: 120000}
	if c := tbl.CostFor(audio, ""); c < 0.01199 || c > 0.01201 {
		t.Fatalf("audio cost = %v, want 0.012", c)
	}
	// llm: 1000 in + 500 out
	llm := report.ItemResult{Provider: "openai:gpt-4o-mini", InputTokens: 1000, OutputTokens: 500}
	want := 1000*0.15/1e6 + 500*0.60/1e6
	if c := tbl.CostFor(llm, ""); c < want*0.999 || c > want*1.001 {
		t.Fatalf("llm cost = %v, want %v", c, want)
	}
	// tts: chars from the synthesized text length
	tts := report.ItemResult{Provider: "openai:tts-x"}
	if c := tbl.CostFor(tts, "hello world"); c <= 0 {
		t.Fatalf("tts cost = %v, want > 0 for 11 chars", c)
	}
	// unknown provider costs nothing rather than guessing
	if c := tbl.CostFor(report.ItemResult{Provider: "mystery"}, "x"); c != 0 {
		t.Fatalf("unknown provider cost = %v, want 0", c)
	}
}

func TestLoadRejectsGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(p, []byte("not json"), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error")
	}
}
