// Package runner executes providers against a corpus with bounded
// concurrency and per-item timeouts.
package runner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/renan-martini/saybench/internal/manifest"
	"github.com/renan-martini/saybench/internal/provider"
	"github.com/renan-martini/saybench/internal/report"
	"github.com/renan-martini/saybench/internal/wav"
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
	// Scoring options (digit normalization etc.) applied wherever WER and
	// keyterms are computed.
	Score wer.Opts
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
				results[j.idx] = runOne(ctx, j.p, j.item, opts)
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
				results[j.idx] = runOneStream(ctx, j.p, j.item, opts)
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

func runOneStream(ctx context.Context, p provider.StreamingProvider, it manifest.Item, opts Options) report.ItemResult {
	res := report.ItemResult{
		Provider:  p.Name(),
		Audio:     it.Audio,
		Category:  it.Category,
		Reference: it.Reference,
	}
	res.AudioDurationMS = clipDurationMS(it.Audio)
	cctx, cancel := context.WithTimeout(ctx, opts.ItemTimeout)
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
	c := wer.ComputeOpts(it.Reference, out.Text, opts.Score)
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
				results[j.idx] = runOneLLM(ctx, j.t, j.p, opts)
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
				results[j.idx] = runOneS2S(ctx, j.p, j.item, opts)
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

func runOneS2S(ctx context.Context, p provider.S2SProvider, it manifest.Item, opts Options) report.ItemResult {
	res := report.ItemResult{Provider: p.Name(), Audio: it.Audio, Category: it.Category, Reference: it.Reference}
	res.AudioDurationMS = clipDurationMS(it.Audio)
	cctx, cancel := context.WithTimeout(ctx, opts.ItemTimeout)
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
	if opts.EchoScore {
		c := wer.ComputeOpts(it.Reference, out.Transcript, opts.Score)
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

// RunTTS benchmarks TTS providers over a set of short texts.
func RunTTS(ctx context.Context, providers []provider.TTSProvider, texts []manifest.Prompt, opts Options) []report.ItemResult {
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	if opts.ItemTimeout <= 0 {
		opts.ItemTimeout = 60 * time.Second
	}
	type job struct {
		p   provider.TTSProvider
		t   manifest.Prompt
		idx int
	}
	jobs := make([]job, 0, len(providers)*len(texts))
	for _, p := range providers {
		for _, t := range texts {
			jobs = append(jobs, job{p: p, t: t, idx: len(jobs)})
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
				res := report.ItemResult{Provider: j.p.Name(), Prompt: j.t.Name, Category: j.t.Category}
				cctx, cancel := context.WithTimeout(ctx, opts.ItemTimeout)
				out, err := j.p.Speak(cctx, j.t.User)
				cancel()
				if err != nil {
					res.Error = err.Error()
				} else {
					res.TTFAudioMS = out.TTFAudioMS
					res.CompletionMS = out.TotalMS
					res.OutputAudioMS = out.AudioMS
				}
				results[j.idx] = res
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

func runOneLLM(ctx context.Context, t provider.LLMTarget, p manifest.Prompt, opts Options) report.ItemResult {
	res := report.ItemResult{Provider: t.Name(), Prompt: p.Name, Category: p.Category}
	cctx, cancel := context.WithTimeout(ctx, opts.ItemTimeout)
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
	res.InputTokens = out.InputTokens
	return res
}

func runOne(ctx context.Context, p provider.Provider, it manifest.Item, opts Options) report.ItemResult {
	res := report.ItemResult{
		Provider:  p.Name(),
		Audio:     it.Audio,
		Category:  it.Category,
		Reference: it.Reference,
	}
	res.AudioDurationMS = clipDurationMS(it.Audio)
	cctx, cancel := context.WithTimeout(ctx, opts.ItemTimeout)
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

	c := wer.ComputeOpts(it.Reference, out.Text, opts.Score)
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

// judgeRubric is fixed: comparability across runs depends on every judge
// getting the same task. The response must be a bare number 0-100.
const judgeRubric = "You are grading a speech transcript. Reference (what was actually said):\n%s\n\nTranscript under test:\n%s\n\nDoes the transcript preserve the meaning of the reference? Ignore punctuation, casing, and formatting differences (digits vs spelled-out numbers are equivalent). Answer with exactly one integer from 0 to 100, where 100 means the meaning is fully preserved and 0 means it is lost. Answer with the number only."

// JudgeItems rates each scored item's hypothesis against its reference with
// an LLM judge, filling JudgeScore/JudgeScored in place. An addition beside
// WER, never a replacement: literal scoring stays untouched. Each judged
// item costs one LLM call. Per-item failures are returned as warnings and
// leave the item unjudged rather than failing the run.
func JudgeItems(ctx context.Context, judge provider.LLMTarget, items []report.ItemResult, opts Options) []error {
	if opts.ItemTimeout <= 0 {
		opts.ItemTimeout = 60 * time.Second
	}
	var errs []error
	for i := range items {
		it := &items[i]
		if it.Error != "" || it.Reference == "" || it.RefWords == 0 {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, opts.ItemTimeout)
		out, err := judge.Complete(cctx, provider.ChatPrompt{
			User:      fmt.Sprintf(judgeRubric, it.Reference, it.Hypothesis),
			MaxTokens: 8,
		})
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("judge %s on %s/%s: %w", judge.Name(), it.Provider, it.Audio, err))
			continue
		}
		score, err := parseJudgeScore(out.Text)
		if err != nil {
			errs = append(errs, fmt.Errorf("judge %s returned %q for %s: %w", judge.Name(), out.Text, it.Audio, err))
			continue
		}
		it.JudgeScore = score
		it.JudgeScored = true
	}
	return errs
}

// parseJudgeScore extracts the 0-100 integer the rubric demands.
func parseJudgeScore(text string) (float64, error) {
	num := ""
	for _, r := range strings.TrimSpace(text) {
		if r >= '0' && r <= '9' {
			num += string(r)
			if len(num) > 3 {
				break
			}
			continue
		}
		if num != "" {
			break
		}
	}
	if num == "" {
		return 0, fmt.Errorf("no number in judge response")
	}
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 || n > 100 {
		return 0, fmt.Errorf("judge score %q out of range", num)
	}
	return float64(n) / 100, nil
}

// clipDurationMS parses the clip locally for cost accounting; 0 when the
// file is not parseable PCM (never an error — duration is auxiliary).
func clipDurationMS(path string) int {
	f, err := wav.Parse(path)
	if err != nil {
		return 0
	}
	return int(f.Duration().Milliseconds())
}
