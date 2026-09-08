// Package runner executes providers against a corpus with bounded
// concurrency and per-item timeouts.
package runner

import (
	"context"
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
