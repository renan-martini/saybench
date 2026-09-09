package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/renan-martini/saybench/internal/manifest"
	"github.com/renan-martini/saybench/internal/provider"
	"github.com/renan-martini/saybench/internal/report"
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

type countingTarget struct{ calls int }

func (c *countingTarget) Name() string { return "counting" }
func (c *countingTarget) Complete(context.Context, provider.ChatPrompt) (provider.LLMResult, error) {
	c.calls++
	return provider.LLMResult{Text: "ok", TTFTMS: 1, CompletionMS: 2, OutputTokens: 1}, nil
}

func TestWarmup(t *testing.T) {
	ct := &countingTarget{}
	errs := Warmup(context.Background(), []provider.LLMTarget{ct, provider.NewFakeLLM()}, 0)
	if len(errs) != 0 {
		t.Fatalf("unexpected warmup errors: %v", errs)
	}
	if ct.calls != 1 {
		t.Fatalf("warmup calls = %d, want exactly 1", ct.calls)
	}
}

func TestRunS2SWithFake(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "clip.wav")
	if err := os.WriteFile(audio, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []manifest.Item{{Audio: audio, Reference: "what are your hours", Category: "faq"}}
	refs := map[string]string{audio: items[0].Reference}
	results := RunS2S(context.Background(), []provider.S2SProvider{provider.NewFakeS2S(refs)}, items, Options{Workers: 2})
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if r.V2VFirstAudioMS <= 0 || r.ResponseDoneMS <= 0 || r.OutputAudioMS <= 0 || r.Hypothesis == "" {
		t.Fatalf("s2s fields wrong: %+v", r)
	}
}

func TestRunS2SEchoScores(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "clip.wav")
	os.WriteFile(audio, []byte("x"), 0o644)
	items := []manifest.Item{{
		Audio:     audio,
		Reference: "one two three four five six seven eight nine ten",
		Category:  "numbers",
		Keyterms:  []string{"nine ten", "zzz-not-said"},
	}}
	refs := map[string]string{audio: items[0].Reference}
	f := provider.NewFakeS2S(refs)
	f.Echo = true
	results := RunS2S(context.Background(), []provider.S2SProvider{f}, items, Options{Workers: 1, EchoScore: true})
	r := results[0]
	if r.Error != "" {
		t.Fatalf("unexpected error: %s", r.Error)
	}
	if r.WER <= 0 || r.WER >= 1 || r.RefWords != 10 {
		t.Fatalf("echo WER not scored: %+v", r)
	}
	if r.KeytermsTotal != 2 || r.KeytermsHit != 1 {
		t.Fatalf("echo keyterms wrong: %+v", r)
	}
	// Latency-only run must NOT score.
	f2 := provider.NewFakeS2S(refs)
	conv := RunS2S(context.Background(), []provider.S2SProvider{f2}, items, Options{Workers: 1})
	if conv[0].RefWords != 0 || conv[0].KeytermsTotal != 0 {
		t.Fatalf("conversational run must not score: %+v", conv[0])
	}
}

func TestJudgeItems(t *testing.T) {
	items := []report.ItemResult{
		{Provider: "p", Reference: "pay two hundred dollars", Hypothesis: "pay two hundred dollars", RefWords: 4},
		{Provider: "p", Reference: "x", Hypothesis: "y", Error: "boom"}, // errored items skipped
		{Provider: "p", Reference: "", Hypothesis: "chatter"},           // unscored items skipped
	}
	errs := JudgeItems(context.Background(), provider.NewFakeLLM(), items, Options{})
	if len(errs) != 0 {
		t.Fatalf("unexpected judge errors: %v", errs)
	}
	if !items[0].JudgeScored || items[0].JudgeScore < 0 || items[0].JudgeScore > 1 {
		t.Fatalf("item 0 not judged sanely: %+v", items[0])
	}
	if items[1].JudgeScored || items[2].JudgeScored {
		t.Fatal("errored/unscored items must not be judged")
	}
	// determinism
	items2 := []report.ItemResult{{Provider: "p", Reference: "pay two hundred dollars", Hypothesis: "pay two hundred dollars", RefWords: 4}}
	JudgeItems(context.Background(), provider.NewFakeLLM(), items2, Options{})
	if items2[0].JudgeScore != items[0].JudgeScore {
		t.Fatal("fake judge must be deterministic")
	}
}
