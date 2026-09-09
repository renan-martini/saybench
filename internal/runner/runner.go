// Package runner executes providers against a corpus with bounded
// concurrency and per-item timeouts.
package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/renan-martini/saybench/internal/manifest"
	"github.com/renan-martini/saybench/internal/provider"
	"github.com/renan-martini/saybench/internal/report"
	"github.com/renan-martini/saybench/internal/wer"
)

// Options tune a run.
type Options struct {
	Workers     int           // concurrent transcriptions (default 4)
	ItemTimeout time.Duration // per-clip deadline (default 60s)
	Progress    func(done, total int)
	// EchoScore turns on comprehension scoring for s2s echo runs: the
	// model's reply transcript is scored against the clip's reference.
	EchoScore bool
}

// Run benchmarks every provider against every item. Item order in the result
// is deterministic regardless of scheduling.
func Run(ctx context.Context, providers []provider.Provider, items []manifest.Item, opts Options) []report.ItemResult {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.ItemTimeout <= 0 {
		opts.ItemTimeout = 60 * time.Second
	}

	type job struct {
		p    provider.Provider
		item manifest.Item
		idx  int
	}
	jobs := make([]job, 0, len(providers)*len(items))
	for _, p := range providers {
		for _, it := range items {
			jobs = append(jobs, job{p: p, item: it, idx: len(jobs)})
		}
	}

	results := make([]report.ItemResult, len(jobs))
	ch := make(chan job)
	var done int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for range opts.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				results[j.idx] = runOne(ctx, j.p, j.item, opts.ItemTimeout)
				if opts.Progress != nil {
					mu.Lock()
					done++
					opts.Progress(done, len(jobs))
					mu.Unlock()
				}
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return results
}

// RunStream is Run for streaming providers: same fan-out and scoring, plus
// the streaming timing fields. Per-item timeout must exceed clip duration —
// streaming feeds audio at real-time pace by design.
func RunStream(ctx context.Context, providers []provider.StreamingProvider, items []manifest.Item, opts Options) []report.ItemResult {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.ItemTimeout <= 0 {
		opts.ItemTimeout = 120 * time.Second
	}
	type job struct {
		p    provider.StreamingProvider
		item manifest.Item
		idx  int
	}
	jobs := make([]job, 0, len(providers)*len(items))
	for _, p := range providers {
		for _, it := range items {
			jobs = append(jobs, job{p: p, item: it, idx: len(jobs)})
		}
	}
	results := make([]report.ItemResult, len(jobs))
	ch := make(chan job)
	var done int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range opts.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				results[j.idx] = runOneStream(ctx, j.p, j.item, opts.ItemTimeout)
				if opts.Progress != nil {
					mu.Lock()
					done++
					opts.Progress(done, len(jobs))
					mu.Unlock()
				}
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return results
}

func runOneStream(ctx context.Context, p provider.StreamingProvider, it manifest.Item, timeout time.Duration) report.ItemResult {
	res := report.ItemResult{
		Provider:  p.Name(),
		Audio:     it.Audio,
		Category:  it.Category,
		Reference: it.Reference,
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := p.StreamTranscribe(cctx, it.Audio)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Hypothesis = out.Text
	res.TTFPartialMS = out.TTFPartialMS
	res.FinalLagMS = out.FinalLagMS
	res.Interims = out.Interims
	if len(out.InterimTexts) > 0 {
		res.InterimSurvivalHit, res.InterimSurvivalTotal = wer.WordSurvival(out.Text, out.InterimTexts)
	}
	c := wer.Compute(it.Reference, out.Text)
	res.Sub, res.Del, res.Ins, res.RefWords = c.Sub, c.Del, c.Ins, c.RefWords
	res.WER = c.WER()
	if len(it.Keyterms) > 0 {
		hit, missed := wer.KeytermHits(out.Text, it.Keyterms)
		res.KeytermsTotal = len(it.Keyterms)
		res.KeytermsHit = len(hit)
		res.MissedKeyterms = missed
	}
	return res
}

// RunLLM benchmarks LLM targets against a prompt set: same fan-out pattern,
// latency-only scoring (see the llm-bench design spec).
func RunLLM(ctx context.Context, targets []provider.LLMTarget, prompts []manifest.Prompt, opts Options) []report.ItemResult {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.ItemTimeout <= 0 {
		opts.ItemTimeout = 60 * time.Second
	}
	type job struct {
		t   provider.LLMTarget
		p   manifest.Prompt
		idx int
	}
	jobs := make([]job, 0, len(targets)*len(prompts))
	for _, t := range targets {
		for _, p := range prompts {
			jobs = append(jobs, job{t: t, p: p, idx: len(jobs)})
		}
	}
	results := make([]report.ItemResult, len(jobs))
	ch := make(chan job)
	var done int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range opts.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				results[j.idx] = runOneLLM(ctx, j.t, j.p, opts.ItemTimeout)
				if opts.Progress != nil {
					mu.Lock()
					done++
					opts.Progress(done, len(jobs))
					mu.Unlock()
				}
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return results
}

// Warmup issues one unmeasured throwaway request per target, so measured
// TTFT reflects warm connections — the posture production voice loops run
// in. Errors come back for display as warnings; the measured run surfaces
// the real failure with full context if a target is actually broken.
func Warmup(ctx context.Context, targets []provider.LLMTarget, timeout time.Duration) []error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			_, err := t.Complete(cctx, provider.ChatPrompt{User: "Say ok.", MaxTokens: 5})
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: warmup: %w", t.Name(), err))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errs
}

// RunS2S benchmarks speech-to-speech providers: one conversational turn per
// clip, latency-only (phase 1 — see the s2s design spec).
func RunS2S(ctx context.Context, providers []provider.S2SProvider, items []manifest.Item, opts Options) []report.ItemResult {
	if opts.Workers <= 0 {
		opts.Workers = 2 // realtime sessions are heavy; be polite by default
	}
	if opts.ItemTimeout <= 0 {
		opts.ItemTimeout = 120 * time.Second
	}
	type job struct {
		p    provider.S2SProvider
		item manifest.Item
		idx  int
	}
	jobs := make([]job, 0, len(providers)*len(items))
	for _, p := range providers {
		for _, it := range items {
			jobs = append(jobs, job{p: p, item: it, idx: len(jobs)})
		}
	}
	results := make([]report.ItemResult, len(jobs))
	ch := make(chan job)
	var done int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range opts.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				results[j.idx] = runOneS2S(ctx, j.p, j.item, opts.ItemTimeout, opts.EchoScore)
				if opts.Progress != nil {
					mu.Lock()
					done++
					opts.Progress(done, len(jobs))
					mu.Unlock()
				}
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	return results
}

func runOneS2S(ctx context.Context, p provider.S2SProvider, it manifest.Item, timeout time.Duration, echoScore bool) report.ItemResult {
	res := report.ItemResult{Provider: p.Name(), Audio: it.Audio, Category: it.Category, Reference: it.Reference}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := p.Converse(cctx, it.Audio)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Hypothesis = out.Transcript
	res.V2VFirstAudioMS = out.V2VFirstAudioMS
	res.ResponseDoneMS = out.ResponseDoneMS
	res.OutputAudioMS = out.OutputAudioMS
	if echoScore {
		c := wer.Compute(it.Reference, out.Transcript)
		res.Sub, res.Del, res.Ins, res.RefWords = c.Sub, c.Del, c.Ins, c.RefWords
		res.WER = c.WER()
		if len(it.Keyterms) > 0 {
			hit, missed := wer.KeytermHits(out.Transcript, it.Keyterms)
			res.KeytermsTotal = len(it.Keyterms)
			res.KeytermsHit = len(hit)
			res.MissedKeyterms = missed
		}
	}
	return res
}

func runOneLLM(ctx context.Context, t provider.LLMTarget, p manifest.Prompt, timeout time.Duration) report.ItemResult {
	res := report.ItemResult{Provider: t.Name(), Prompt: p.Name, Category: p.Category}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hist := make([][2]string, 0, len(p.History))
	for _, m := range p.History {
		hist = append(hist, [2]string{m.Role, m.Content})
	}
	out, err := t.Complete(cctx, provider.ChatPrompt{System: p.System, History: hist, User: p.User, MaxTokens: p.MaxTokens})
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Hypothesis = out.Text
	res.TTFTMS = out.TTFTMS
	res.CompletionMS = out.CompletionMS
	res.OutputTokens = out.OutputTokens
	return res
}

func runOne(ctx context.Context, p provider.Provider, it manifest.Item, timeout time.Duration) report.ItemResult {
	res := report.ItemResult{
		Provider:  p.Name(),
		Audio:     it.Audio,
		Category:  it.Category,
		Reference: it.Reference,
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	out, err := p.Transcribe(cctx, it.Audio)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	latency := out.Latency
	if latency == 0 {
		latency = time.Since(start)
	}
	res.LatencyMS = latency.Milliseconds()
	res.Hypothesis = out.Text

	c := wer.Compute(it.Reference, out.Text)
	res.Sub, res.Del, res.Ins, res.RefWords = c.Sub, c.Del, c.Ins, c.RefWords
	res.WER = c.WER()
	if len(it.Keyterms) > 0 {
		hit, missed := wer.KeytermHits(out.Text, it.Keyterms)
		res.KeytermsTotal = len(it.Keyterms)
		res.KeytermsHit = len(hit)
		res.MissedKeyterms = missed
	}
	return res
}
