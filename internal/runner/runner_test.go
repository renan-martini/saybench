package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/renan-martini/saybench/internal/manifest"
	"github.com/renan-martini/saybench/internal/provider"
)

func TestRunWithFake(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "clip.wav")
	if err := os.WriteFile(audio, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []manifest.Item{{
		Audio:     audio,
		Reference: "one two three four five six seven eight nine ten",
		Category:  "numbers",
	}}
	refs := map[string]string{audio: items[0].Reference}
	results := Run(context.Background(), []provider.Provider{provider.NewFake(refs)}, items, Options{Workers: 2})

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if r.WER <= 0 || r.WER >= 1 {
		t.Fatalf("fake WER = %v, want (0,1)", r.WER)
	}
	if r.RefWords != 10 || r.LatencyMS <= 0 {
		t.Fatalf("bad result: %+v", r)
	}
}

func TestRunStreamWithFake(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "clip.wav")
	if err := os.WriteFile(audio, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []manifest.Item{{
		Audio:     audio,
		Reference: "one two three four five six seven eight nine ten",
		Category:  "numbers",
		Keyterms:  []string{"nine ten"},
	}}
	refs := map[string]string{audio: items[0].Reference}
	results := RunStream(context.Background(), []provider.StreamingProvider{provider.NewFakeStream(refs)}, items, Options{Workers: 2})
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if r.TTFPartialMS <= 0 || r.FinalLagMS <= 0 || r.Interims <= 0 {
		t.Fatalf("streaming fields missing: %+v", r)
	}
	if r.WER <= 0 || r.RefWords != 10 || r.KeytermsTotal != 1 {
		t.Fatalf("scoring missing: %+v", r)
	}
	if r.InterimSurvivalTotal <= 0 || r.InterimSurvivalHit <= 0 || r.InterimSurvivalHit >= r.InterimSurvivalTotal {
		t.Fatalf("interim survival not scored (want 0 < hit < total): %+v", r)
	}
}

func TestRunLLMWithFake(t *testing.T) {
	prompts := []manifest.Prompt{{Name: "greet", User: "hello there", Category: "greeting", MaxTokens: 60}}
	results := RunLLM(context.Background(), []provider.LLMTarget{provider.NewFakeLLM()}, prompts, Options{Workers: 2})
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if r.Prompt != "greet" || r.Category != "greeting" || r.TTFTMS <= 0 || r.CompletionMS <= 0 || r.OutputTokens <= 0 {
		t.Fatalf("llm fields wrong: %+v", r)
	}
	if r.Hypothesis == "" {
		t.Fatal("output text not captured")
	}
}
