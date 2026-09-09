// saybench benchmarks speech-to-text providers against your own audio: word
// error rate with a substitution/deletion/insertion breakdown, latency, and
// run-to-run comparison with a CI regression gate.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/renan-martini/saybench/internal/dashboard"
	"github.com/renan-martini/saybench/internal/manifest"
	"github.com/renan-martini/saybench/internal/mcp"
	"github.com/renan-martini/saybench/internal/pricing"
	"github.com/renan-martini/saybench/internal/provider"
	"github.com/renan-martini/saybench/internal/report"
	"github.com/renan-martini/saybench/internal/runner"
	"github.com/renan-martini/saybench/internal/wer"
)

const version = "1.0.0"

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
	case "stream":
		err = cmdStream(ctx, os.Args[2:])
	case "llm":
		err = cmdLLM(ctx, os.Args[2:])
	case "s2s":
		err = cmdS2S(ctx, os.Args[2:])
	case "tts":
		err = cmdTTS(ctx, os.Args[2:])
	case "mcp":
		err = mcp.Serve(os.Stdin, os.Stdout, version)
	case "compare":
		err = cmdCompare(os.Args[2:])
	case "html":
		err = cmdHTML(os.Args[2:])
	case "show":
		err = cmdShow(os.Args[2:])
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
  saybench stt     -providers fake,deepgram,openai [-manifest golden/manifest.jsonl] [-report out.json] [-format json]
  saybench stream  -providers fake-stream,deepgram,openai-realtime,assemblyai [same flags]
  saybench llm     -targets fake-llm,gpt-4o-mini,openai-ws:gpt-4o-mini,groq:<m>,openrouter:<m>,custom:<m> [-warmup=false]
  saybench s2s     -providers fake-s2s,openai,gemini,custom [-score echo] [-turn-ending server_vad] [-barge-in]
  saybench tts     -providers fake-tts,openai,elevenlabs,custom [-texts llm/golden.jsonl]
  saybench mcp     # Model Context Protocol server on stdio — for coding agents
  saybench compare old.json new.json [-max-wer-regression 2.0]
  saybench html    -o dashboard.html run1.json run2.json ...
  saybench show    report.json [-format json]
  saybench version

Providers read API keys from the environment only:
  deepgram          DEEPGRAM_API_KEY   (model: SAYBENCH_DEEPGRAM_MODEL, default nova-3)
  openai            OPENAI_API_KEY     (model: SAYBENCH_OPENAI_MODEL, default gpt-4o-mini-transcribe)
  openai-realtime   OPENAI_API_KEY     (model: SAYBENCH_OPENAI_REALTIME_MODEL)
  assemblyai        ASSEMBLYAI_API_KEY
  fake, fake-stream, fake-llm — no key; deterministic offline providers for CI and demos

LLM targets are [provider:]model — openai (default), openrouter (OPENROUTER_API_KEY),
groq (GROQ_API_KEY), or custom (SAYBENCH_LLM_BASE_URL + optional SAYBENCH_LLM_API_KEY
— any OpenAI-compatible server: vLLM, Ollama, self-hosted).

Streaming endpoints take URL overrides for self-hosted/compatible servers:
  SAYBENCH_DEEPGRAM_STREAM_URL, SAYBENCH_OPENAI_REALTIME_URL, SAYBENCH_ASSEMBLYAI_STREAM_URL
`)
}

func cmdSTT(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stt", flag.ExitOnError)
	providers := fs.String("providers", "fake", "comma-separated providers: fake, deepgram, openai")
	manifestPath := fs.String("manifest", "golden/manifest.jsonl", "path to a JSONL corpus manifest")
	reportPath := fs.String("report", "", "write the full JSON report here")
	format := fs.String("format", "table", "stdout format: table or json (json is the full report, machine- and LLM-readable)")
	normalize := fs.String("normalize", "", `"" (literal scoring) | "digits" — canonicalize digit strings vs spelled digits before scoring`)
	pricingPath := fs.String("pricing", "", "pricing table JSON (see pricing.example.json); adds cost columns")
	judgeSpec := fs.String("judge", "", "LLM target that rates semantic preservation per item (e.g. gpt-4o-mini, fake-llm); one LLM call per scored item")
	workers := fs.Int("workers", 4, "concurrent transcriptions")
	timeout := fs.Duration("timeout", 60*time.Second, "per-clip timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	items, err := manifest.Load(*manifestPath)
	if err != nil {
		if os.IsNotExist(err) && *manifestPath == "golden/manifest.jsonl" {
			return fmt.Errorf("default corpus not found (run from a clone of the repo, or point -manifest at your own JSONL corpus): %w", err)
		}
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
	score, err := scoreOpts(*normalize)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "saybench: %d clips × %d providers\n", len(items), len(ps))
	results := runner.Run(ctx, ps, items, runner.Options{
		Workers:     *workers,
		ItemTimeout: *timeout,
		Score:       score,
		Progress: func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r%d/%d", done, total)
			if done == total {
				fmt.Fprintln(os.Stderr)
			}
		},
	})
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted — no report written (partial results would be misleading)")
	}
	var judgeName string
	if *judgeSpec != "" {
		tmp := report.Report{}
		if err := applyJudge(ctx, *judgeSpec, results, &tmp); err != nil {
			return err
		}
		judgeName = tmp.Judge
	}
	if err := applyPricing(*pricingPath, results, nil); err != nil {
		return err
	}
	rep := report.Build(version, *manifestPath, results)
	rep.Normalization = *normalize
	rep.Judge = judgeName
	switch *format {
	case "table":
		printSummary(rep)
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown -format %q (table or json)", *format)
	}

	if *reportPath != "" {
		if err := rep.Save(*reportPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "report written to %s\n", *reportPath)
	}
	return nil
}

func cmdStream(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stream", flag.ExitOnError)
	providers := fs.String("providers", "fake-stream", "comma-separated: fake-stream, deepgram, openai-realtime, assemblyai")
	manifestPath := fs.String("manifest", "golden/manifest.jsonl", "path to a JSONL corpus manifest")
	reportPath := fs.String("report", "", "write the full JSON report here")
	format := fs.String("format", "table", "stdout format: table or json")
	workers := fs.Int("workers", 4, "concurrent streams")
	timeout := fs.Duration("timeout", 120*time.Second, "per-clip timeout (must exceed clip duration — audio feeds at real-time pace)")
	normalize := fs.String("normalize", "", `"" | "digits" — canonicalize digit strings vs spelled digits before scoring`)
	pricingPath := fs.String("pricing", "", "pricing table JSON (see pricing.example.json); adds cost columns")
	judgeSpec := fs.String("judge", "", "LLM target that rates semantic preservation per item")
	if err := fs.Parse(args); err != nil {
		return err
	}
	items, err := manifest.Load(*manifestPath)
	if err != nil {
		if os.IsNotExist(err) && *manifestPath == "golden/manifest.jsonl" {
			return fmt.Errorf("default corpus not found (run from a clone of the repo, or point -manifest at your own JSONL corpus): %w", err)
		}
		return err
	}
	refs := make(map[string]string, len(items))
	for _, it := range items {
		refs[it.Audio] = it.Reference
	}
	ps, err := provider.StreamFromSpecs(*providers, refs)
	if err != nil {
		return err
	}
	score, err := scoreOpts(*normalize)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "saybench: streaming %d clips × %d providers (real-time pace)\n", len(items), len(ps))
	results := runner.RunStream(ctx, ps, items, runner.Options{
		Workers:     *workers,
		ItemTimeout: *timeout,
		Score:       score,
		Progress: func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r%d/%d", done, total)
			if done == total {
				fmt.Fprintln(os.Stderr)
			}
		},
	})
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted — no report written (partial results would be misleading)")
	}
	var judgeName string
	if *judgeSpec != "" {
		tmp := report.Report{}
		if err := applyJudge(ctx, *judgeSpec, results, &tmp); err != nil {
			return err
		}
		judgeName = tmp.Judge
	}
	if err := applyPricing(*pricingPath, results, nil); err != nil {
		return err
	}
	rep := report.BuildMode(version, *manifestPath, report.ModeStreaming, results)
	rep.Normalization = *normalize
	rep.Judge = judgeName
	switch *format {
	case "table":
		printSummary(rep)
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown -format %q (table or json)", *format)
	}
	if *reportPath != "" {
		if err := rep.Save(*reportPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "report written to %s\n", *reportPath)
	}
	return nil
}

func cmdS2S(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("s2s", flag.ExitOnError)
	providers := fs.String("providers", "fake-s2s", "comma-separated: fake-s2s, openai, custom (OpenAI-Realtime-dialect endpoint via SAYBENCH_S2S_URL)")
	manifestPath := fs.String("manifest", "golden/manifest.jsonl", "path to a JSONL corpus manifest (clips are the user's turns)")
	reportPath := fs.String("report", "", "write the full JSON report here")
	format := fs.String("format", "table", "stdout format: table or json")
	workers := fs.Int("workers", 2, "concurrent conversations")
	timeout := fs.Duration("timeout", 120*time.Second, "per-turn timeout (audio feeds at real-time pace)")
	score := fs.String("score", "", `"" = conversational (latency only) | "echo" = repeat-back task, comprehension-scored with WER + keyterm recall`)
	normalize := fs.String("normalize", "", `"" | "digits" — canonicalize digit strings vs spelled digits before echo scoring`)
	turnEnding := fs.String("turn-ending", "commit", `"commit" (deterministic) | "server_vad" (production posture — V2V includes VAD hangover; a 1.5s silence tail is appended so the VAD can fire)`)
	pricingPath := fs.String("pricing", "", "pricing table JSON (see pricing.example.json); adds cost columns")
	judgeSpec := fs.String("judge", "", "LLM target that rates echo semantic preservation per item (requires -score echo)")
	bargeIn := fs.Bool("barge-in", false, "interrupt the model's reply mid-speech and measure how fast it stops (implies -turn-ending server_vad; not combinable with -score echo)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *score != "" && *score != "echo" {
		return fmt.Errorf("unknown -score %q (only \"echo\" or empty)", *score)
	}
	echo := *score == "echo"
	if *turnEnding != "commit" && *turnEnding != "server_vad" {
		return fmt.Errorf("unknown -turn-ending %q", *turnEnding)
	}
	if *bargeIn {
		if echo {
			return fmt.Errorf("-barge-in and -score echo are different experiments; run them separately")
		}
		*turnEnding = "server_vad"
	}
	items, err := manifest.Load(*manifestPath)
	if err != nil {
		if os.IsNotExist(err) && *manifestPath == "golden/manifest.jsonl" {
			return fmt.Errorf("default corpus not found (run from a clone of the repo, or point -manifest at your own JSONL corpus): %w", err)
		}
		return err
	}
	refs := make(map[string]string, len(items))
	for _, it := range items {
		refs[it.Audio] = it.Reference
	}
	ps, err := provider.S2SFromSpecsOpts(*providers, refs, provider.S2SOpts{Echo: echo, TurnEnding: *turnEnding, BargeIn: *bargeIn})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "saybench: %d turns × %d providers (real-time pace, one conversation per clip)\n", len(items), len(ps))
	scoreO, err := scoreOpts(*normalize)
	if err != nil {
		return err
	}
	results := runner.RunS2S(ctx, ps, items, runner.Options{
		Workers:     *workers,
		ItemTimeout: *timeout,
		EchoScore:   echo,
		Score:       scoreO,
		Progress: func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r%d/%d", done, total)
			if done == total {
				fmt.Fprintln(os.Stderr)
			}
		},
	})
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted — no report written (partial results would be misleading)")
	}
	var judgeName string
	if *judgeSpec != "" {
		if !echo {
			return fmt.Errorf("-judge requires -score echo (there is no reference-scored transcript otherwise)")
		}
		tmp := report.Report{}
		if err := applyJudge(ctx, *judgeSpec, results, &tmp); err != nil {
			return err
		}
		judgeName = tmp.Judge
	}
	if err := applyPricing(*pricingPath, results, nil); err != nil {
		return err
	}
	rep := report.BuildMode(version, *manifestPath, report.ModeS2S, results)
	rep.Judge = judgeName
	rep.S2SScoring = "conversational"
	if echo {
		rep.S2SScoring = "echo"
	}
	rep.Normalization = *normalize
	rep.S2STurnEnding = *turnEnding
	rep.S2SBargeIn = *bargeIn
	switch *format {
	case "table":
		printSummary(rep)
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown -format %q (table or json)", *format)
	}
	if *reportPath != "" {
		if err := rep.Save(*reportPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "report written to %s\n", *reportPath)
	}
	return nil
}

func cmdTTS(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tts", flag.ExitOnError)
	providers := fs.String("providers", "fake-tts", "comma-separated: fake-tts, openai, elevenlabs, custom")
	textsPath := fs.String("texts", "llm/golden.jsonl", "JSONL prompt manifest; each entry's user text is synthesized")
	reportPath := fs.String("report", "", "write the full JSON report here")
	format := fs.String("format", "table", "stdout format: table or json")
	workers := fs.Int("workers", 4, "concurrent syntheses")
	timeout := fs.Duration("timeout", 60*time.Second, "per-utterance timeout")
	pricingPath := fs.String("pricing", "", "pricing table JSON (see pricing.example.json); adds cost columns")
	if err := fs.Parse(args); err != nil {
		return err
	}
	texts, err := manifest.LoadPrompts(*textsPath)
	if err != nil {
		if os.IsNotExist(err) && *textsPath == "llm/golden.jsonl" {
			return fmt.Errorf("default text set not found (run from a clone of the repo, or point -texts at your own JSONL): %w", err)
		}
		return err
	}
	ps, err := provider.TTSFromSpecs(*providers)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "saybench: %d utterances × %d providers\n", len(texts), len(ps))
	results := runner.RunTTS(ctx, ps, texts, runner.Options{
		Workers:     *workers,
		ItemTimeout: *timeout,
		Progress: func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r%d/%d", done, total)
			if done == total {
				fmt.Fprintln(os.Stderr)
			}
		},
	})
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted — no report written (partial results would be misleading)")
	}
	if err := applyPricing(*pricingPath, results, func(it report.ItemResult) string { return textOf(texts, it.Prompt) }); err != nil {
		return err
	}
	rep := report.BuildMode(version, *textsPath, report.ModeTTS, results)
	switch *format {
	case "table":
		printSummary(rep)
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown -format %q (table or json)", *format)
	}
	if *reportPath != "" {
		if err := rep.Save(*reportPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "report written to %s\n", *reportPath)
	}
	return nil
}

func cmdLLM(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("llm", flag.ExitOnError)
	targets := fs.String("targets", "fake-llm", "comma-separated [provider:]model targets")
	promptsPath := fs.String("prompts", "llm/golden.jsonl", "path to a JSONL prompt manifest")
	reportPath := fs.String("report", "", "write the full JSON report here")
	format := fs.String("format", "table", "stdout format: table or json")
	workers := fs.Int("workers", 4, "concurrent requests")
	timeout := fs.Duration("timeout", 60*time.Second, "per-prompt timeout")
	warmup := fs.Bool("warmup", true, "one unmeasured request per target first, so TTFT reflects warm connections (production posture); -warmup=false measures cold starts")
	pricingPath := fs.String("pricing", "", "pricing table JSON (see pricing.example.json); adds cost columns")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prompts, err := manifest.LoadPrompts(*promptsPath)
	if err != nil {
		if os.IsNotExist(err) && *promptsPath == "llm/golden.jsonl" {
			return fmt.Errorf("default prompt set not found (run from a clone of the repo, or point -prompts at your own JSONL): %w", err)
		}
		return err
	}
	ts, err := provider.FromLLMSpecs(*targets)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "saybench: %d prompts × %d targets\n", len(prompts), len(ts))
	if *warmup {
		fmt.Fprintln(os.Stderr, "saybench: warming up targets (disable with -warmup=false)")
		for _, werr := range runner.Warmup(ctx, ts, *timeout) {
			fmt.Fprintf(os.Stderr, "saybench: warning: %v (continuing — the measured run will show the full error)\n", werr)
		}
	}
	results := runner.RunLLM(ctx, ts, prompts, runner.Options{
		Workers:     *workers,
		ItemTimeout: *timeout,
		Progress: func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r%d/%d", done, total)
			if done == total {
				fmt.Fprintln(os.Stderr)
			}
		},
	})
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted — no report written (partial results would be misleading)")
	}
	if err := applyPricing(*pricingPath, results, nil); err != nil {
		return err
	}
	rep := report.BuildMode(version, *promptsPath, report.ModeLLM, results)
	rep.Warmup = *warmup
	switch *format {
	case "table":
		printSummary(rep)
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown -format %q (table or json)", *format)
	}
	if *reportPath != "" {
		if err := rep.Save(*reportPath); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "report written to %s\n", *reportPath)
	}
	return nil
}

// applyJudge runs judge scoring over items when a judge target is named.
func applyJudge(ctx context.Context, spec string, items []report.ItemResult, rep *report.Report) error {
	if spec == "" {
		return nil
	}
	ts, err := provider.FromLLMSpecs(spec)
	if err != nil {
		return err
	}
	if len(ts) != 1 {
		return fmt.Errorf("-judge takes exactly one LLM target")
	}
	fmt.Fprintf(os.Stderr, "saybench: judging %s\n", ts[0].Name())
	for _, werr := range runner.JudgeItems(ctx, ts[0], items, runner.Options{}) {
		fmt.Fprintf(os.Stderr, "saybench: warning: %v (item left unjudged)\n", werr)
	}
	rep.Judge = ts[0].Name()
	return nil
}

// textOf finds a prompt's user text by scenario name (tts char pricing).
func textOf(prompts []manifest.Prompt, name string) string {
	for _, p := range prompts {
		if p.Name == name {
			return p.User
		}
	}
	return ""
}

// applyPricing loads a user-supplied rate table (if any) and stamps item
// costs. texts maps item index -> synthesized text for tts char pricing.
func applyPricing(path string, items []report.ItemResult, textFor func(report.ItemResult) string) error {
	if path == "" {
		return nil
	}
	tbl, err := pricing.Load(path)
	if err != nil {
		return err
	}
	for i := range items {
		txt := ""
		if textFor != nil {
			txt = textFor(items[i])
		}
		items[i].CostUSD = tbl.CostFor(items[i], txt)
	}
	return nil
}

// scoreOpts maps the -normalize flag to scoring options.
func scoreOpts(normalize string) (wer.Opts, error) {
	switch normalize {
	case "":
		return wer.Opts{}, nil
	case "digits":
		return wer.Opts{DigitNormalize: true}, nil
	default:
		return wer.Opts{}, fmt.Errorf("unknown -normalize %q (only \"digits\" or empty)", normalize)
	}
}

func printCost(r report.Report) {
	var total float64
	var any bool
	for _, s := range r.Summaries {
		if s.TotalCostUSD > 0 {
			any = true
			total += s.TotalCostUSD
		}
	}
	if !any {
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(w, "\nCOST (from your pricing table)\ttotal $%.4f\n", total)
	for _, s := range r.Summaries {
		if s.TotalCostUSD > 0 {
			fmt.Fprintf(w, "  %s\t$%.4f\n", s.Provider, s.TotalCostUSD)
		}
	}
	w.Flush()
}

func printSummary(r report.Report) {
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	if r.Mode == report.ModeTTS {
		fmt.Fprintln(w, "PROVIDER\tUTTERANCES\tERRORS\tTTFA AVG\tTTFA P95\tSYNTH TOTAL AVG\tAUDIO OUT AVG")
		for _, s := range r.Summaries {
			fmt.Fprintf(w, "%s\t%d\t%d\t%dms\t%dms\t%dms\t%dms\n",
				s.Provider, s.Items, s.Errors, s.AvgTTFAudioMS, s.P95TTFAudioMS, s.AvgCompletionMS, s.AvgOutputAudioMS)
		}
		w.Flush()
		printCost(r)
		return
	}
	if r.Mode == report.ModeS2S {
		if r.S2SBargeIn {
			fmt.Fprintln(w, "PROVIDER\tTURNS\tERRORS\tBARGE-IN STOP AVG\tSTOP P95\tV2V FIRST AUDIO AVG\tSPEECH OUT AVG")
			for _, s := range r.Summaries {
				fmt.Fprintf(w, "%s\t%d\t%d\t%dms\t%dms\t%dms\t%dms\n",
					s.Provider, s.Items, s.Errors, s.AvgBargeInStopMS, s.P95BargeInStopMS, s.AvgV2VFirstAudioMS, s.AvgOutputAudioMS)
			}
			w.Flush()
			printCost(r)
			return
		}
		fmt.Fprintln(w, "PROVIDER\tTURNS\tERRORS\tECHO WER\tKEYTERM RECALL\tV2V FIRST AUDIO AVG\tV2V P95\tRESPONSE DONE AVG\tSPEECH OUT AVG")
		for _, s := range r.Summaries {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%dms\t%dms\t%dms\t%dms\n",
				s.Provider, s.Items, s.Errors, report.FormatPct(s.WER), report.FormatPct(s.KeytermRecall),
				s.AvgV2VFirstAudioMS, s.P95V2VFirstAudioMS, s.AvgResponseDoneMS, s.AvgOutputAudioMS)
		}
		w.Flush()
		printCost(r)
		return
	}
	if r.Mode == report.ModeLLM {
		fmt.Fprintln(w, "TARGET\tPROMPTS\tERRORS\tTTFT AVG\tTTFT P95\tCOMPLETION AVG\tTOK/S")
		for _, s := range r.Summaries {
			fmt.Fprintf(w, "%s\t%d\t%d\t%dms\t%dms\t%dms\t%.1f\n",
				s.Provider, s.Items, s.Errors, s.AvgTTFTMS, s.P95TTFTMS, s.AvgCompletionMS, s.AvgTokensPerSec)
		}
		w.Flush()
		printCost(r)
		return
	}
	if r.Mode == report.ModeStreaming {
		fmt.Fprintln(w, "PROVIDER\tCLIPS\tERRORS\tWER\tKEYTERM RECALL\tTTFP AVG\tTTFP P95\tFINAL LAG AVG\tFINAL LAG P95\tINTERIM SURVIVAL")
		for _, s := range r.Summaries {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%dms\t%dms\t%dms\t%dms\t%s\n",
				s.Provider, s.Items, s.Errors, report.FormatPct(s.WER), report.FormatPct(s.KeytermRecall),
				s.AvgTTFPartialMS, s.P95TTFPartialMS, s.AvgFinalLagMS, s.P95FinalLagMS, report.FormatPct(s.InterimWordSurvival))
		}
	} else {
		fmt.Fprintln(w, "PROVIDER\tCLIPS\tERRORS\tWER\tKEYTERM RECALL\tJUDGE\tAVG LATENCY\tP95 LATENCY")
		for _, s := range r.Summaries {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\t%s\t%dms\t%dms\n",
				s.Provider, s.Items, s.Errors, report.FormatPct(s.WER), report.FormatPct(s.KeytermRecall), report.FormatPct(s.AvgJudgeScore), s.AvgLatencyMS, s.P95LatencyMS)
		}
	}
	w.Flush()
	printCost(r)

	fmt.Println()
	w = tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tCATEGORY\tCLIPS\tWER")
	for _, c := range r.Categories {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", c.Provider, c.Category, c.Items, report.FormatPct(c.WER))
	}
	w.Flush()
}

// cmdShow re-renders a saved report as the standard summary tables.
func cmdShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	format := fs.String("format", "table", "stdout format: table or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: saybench show report.json")
	}
	r, err := report.LoadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	printSummary(r)
	return nil
}

func cmdHTML(args []string) error {
	fs := flag.NewFlagSet("html", flag.ExitOnError)
	out := fs.String("o", "dashboard.html", "output HTML file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Accept flags and file arguments in any order.
	var files []string
	rest := fs.Args()
	for len(rest) > 0 {
		if strings.HasPrefix(rest[0], "-") {
			if err := fs.Parse(rest); err != nil {
				return err
			}
			rest = fs.Args()
			continue
		}
		files = append(files, rest[0])
		rest = rest[1:]
	}
	if len(files) == 0 {
		return fmt.Errorf("usage: saybench html -o dashboard.html run1.json [run2.json ...] (oldest first)")
	}
	runs := make([]report.Report, 0, len(files))
	for _, f := range files {
		r, err := report.LoadFile(f)
		if err != nil {
			return err
		}
		runs = append(runs, r)
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := dashboard.Render(f, runs, version); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "dashboard written to %s (%d runs)\n", *out, len(runs))
	return nil
}

func cmdCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	maxRegression := fs.Float64("max-wer-regression", -1,
		"fail (exit 1) if any provider's WER worsens by more than this many percentage points")
	format := fs.String("format", "table", "stdout format: table or json")
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
	if err := report.CheckComparable(old, new_); err != nil {
		return err
	}
	if note := report.ConditionNote(old, new_); note != "" {
		fmt.Fprintln(os.Stderr, "saybench:", note)
	}
	deltas := report.Compare(old, new_)

	if *format == "json" {
		worst, who := report.WorstRegression(deltas)
		out := struct {
			Deltas         []report.Delta `json:"deltas"`
			WorstWERChange float64        `json:"worst_wer_change_pp"`
			WorstProvider  string         `json:"worst_provider,omitempty"`
		}{deltas, worst, who}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	if *format == "json" {
		w = tabwriter.NewWriter(io.Discard, 2, 4, 2, ' ', 0)
	}
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
