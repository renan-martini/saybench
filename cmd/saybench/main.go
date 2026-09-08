// saybench benchmarks speech-to-text providers against your own audio: word
// error rate with a substitution/deletion/insertion breakdown, latency, and
// run-to-run comparison with a CI regression gate.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/renan-martini/saybench/internal/manifest"
	"github.com/renan-martini/saybench/internal/provider"
	"github.com/renan-martini/saybench/internal/report"
	"github.com/renan-martini/saybench/internal/runner"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "stt":
		err = cmdSTT(ctx, os.Args[2:])
	case "compare":
		err = cmdCompare(os.Args[2:])
	case "version":
		fmt.Println("saybench", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "saybench:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`saybench — benchmark speech-to-text on your own audio

Usage:
  saybench stt     -providers fake,deepgram,openai [-manifest golden/manifest.jsonl] [-report out.json]
  saybench compare old.json new.json [-max-wer-regression 2.0]
  saybench version

Providers read API keys from the environment only:
  deepgram   DEEPGRAM_API_KEY   (model: SAYBENCH_DEEPGRAM_MODEL, default nova-3)
  openai     OPENAI_API_KEY     (model: SAYBENCH_OPENAI_MODEL, default gpt-4o-mini-transcribe)
  fake       no key — deterministic offline provider for CI and demos
`)
}

func cmdSTT(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stt", flag.ExitOnError)
	providers := fs.String("providers", "fake", "comma-separated providers: fake, deepgram, openai")
	manifestPath := fs.String("manifest", "golden/manifest.jsonl", "path to a JSONL corpus manifest")
	reportPath := fs.String("report", "", "write the full JSON report here")
	workers := fs.Int("workers", 4, "concurrent transcriptions")
	timeout := fs.Duration("timeout", 60*time.Second, "per-clip timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	items, err := manifest.Load(*manifestPath)
	if err != nil {
		return err
	}
	refs := make(map[string]string, len(items))
	for _, it := range items {
		refs[it.Audio] = it.Reference
	}
	ps, err := provider.FromSpecs(*providers, refs)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "saybench: %d clips × %d providers\n", len(items), len(ps))
	results := runner.Run(ctx, ps, items, runner.Options{
		Workers:     *workers,
		ItemTimeout: *timeout,
		Progress: func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r%d/%d", done, total)
			if done == total {
				fmt.Fprintln(os.Stderr)
			}
		},
	})
	rep := report.Build(version, *manifestPath, results)
	printSummary(rep)

	if *reportPath != "" {
		if err := rep.Save(*reportPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "report written to %s\n", *reportPath)
	}
	return nil
}

func printSummary(r report.Report) {
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tCLIPS\tERRORS\tWER\tAVG LATENCY\tP95 LATENCY")
	for _, s := range r.Summaries {
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%dms\t%dms\n",
			s.Provider, s.Items, s.Errors, report.FormatPct(s.WER), s.AvgLatencyMS, s.P95LatencyMS)
	}
	w.Flush()

	fmt.Println()
	w = tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tCATEGORY\tCLIPS\tWER")
	for _, c := range r.Categories {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", c.Provider, c.Category, c.Items, report.FormatPct(c.WER))
	}
	w.Flush()
}

func cmdCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	maxRegression := fs.Float64("max-wer-regression", -1,
		"fail (exit 1) if any provider's WER worsens by more than this many percentage points")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Accept flags on either side of the two file arguments, so
	// "compare old.json new.json -max-wer-regression 2" also works.
	rest := fs.Args()
	if len(rest) > 2 {
		if err := fs.Parse(rest[2:]); err != nil {
			return err
		}
		rest = rest[:2]
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: saybench compare old.json new.json [-max-wer-regression N]")
	}
	old, err := report.LoadFile(rest[0])
	if err != nil {
		return err
	}
	new_, err := report.LoadFile(rest[1])
	if err != nil {
		return err
	}
	deltas := report.Compare(old, new_)

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tWER OLD\tWER NEW\tCHANGE\tLATENCY OLD\tLATENCY NEW")
	for _, d := range deltas {
		switch {
		case d.OnlyInNew:
			fmt.Fprintf(w, "%s\t—\t%s\tnew\t—\t%dms\n", d.Provider, report.FormatPct(d.NewWER), d.NewLatMS)
		case d.OnlyInOld:
			fmt.Fprintf(w, "%s\t%s\t—\tremoved\t%dms\t—\n", d.Provider, report.FormatPct(d.OldWER), d.OldLatMS)
		default:
			fmt.Fprintf(w, "%s\t%s\t%s\t%+.1fpp\t%dms\t%dms\n",
				d.Provider, report.FormatPct(d.OldWER), report.FormatPct(d.NewWER), d.WERChange, d.OldLatMS, d.NewLatMS)
		}
	}
	w.Flush()

	if *maxRegression >= 0 {
		worst, who := report.WorstRegression(deltas)
		if worst > *maxRegression {
			return fmt.Errorf("WER regression gate failed: %s worsened by %.1fpp (limit %.1fpp)", who, worst, *maxRegression)
		}
		fmt.Fprintf(os.Stderr, "regression gate passed (worst change %.1fpp)\n", worst)
	}
	return nil
}
