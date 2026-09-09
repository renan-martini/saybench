[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# Reports, compare, and the dashboard

## Machine-readable everything

Every command takes `-format json` and emits the full structured result to stdout; `-report out.json` saves it. This is deliberate: benchmarks should be consumable by **coding agents, not just humans** — an LLM debugging your voice pipeline can run `saybench stt -format json`, read the per-clip breakdown, and know *which names it garbled* before a feature ships. The [MCP server](mcp.md) is built on exactly this.

A report records not just the numbers but the **conditions** they were measured under: the mode, warm/cold posture (`warmup`), scoring normalization, s2s task (`s2s_scoring`), turn-ending, judge target, and barge-in status. Conditions exist so comparisons can be honest — see below.

## `saybench show` — revisit any saved report

```bash
saybench show today.json                 # re-render the summary tables
saybench show -format json today.json    # the full report, pretty-printed
```

Every published table in these docs was generated this way — literally re-rendered real output.

## `saybench compare` — the regression gate

```bash
saybench compare baseline.json today.json -max-wer-regression 2.0
```

Prints per-provider WER and latency deltas; with `-max-wer-regression N` it **exits 1** if any provider's WER worsened by more than N percentage points — wire it into CI and a quiet vendor-side degradation fails the build before your users notice.

Two safety rails:

- **Cross-mode comparison is refused outright.** Batch and streaming latency measure different things; a delta between them would be a lie.
- **Cross-condition comparison warns.** A warm run vs a cold one, `-normalize digits` vs literal scoring, echo vs conversational s2s, `commit` vs `server_vad` turn ending, different `-judge` targets, or a barge-in run vs a normal one — each prints a warning explaining exactly which conditions are mixed and why the delta is suspect.

## The dashboard

`saybench html -o dashboard.html run1.json run2.json ...` renders any set of reports into a **single self-contained HTML file** — no server, no CDN, no build step. Latest-run summary, A/B comparison between any two runs, WER trend across runs, per-category and latency charts, keyterm recall, and a worst-clips table with the exact missed terms. Commit it as a CI artifact and every PR gets a visual diff of its voice pipeline.

![saybench dashboard](dashboard.png)

## Cost columns — from your pricing table, never ours

Add `-pricing pricing.json` to any bench and reports gain real dollar costs: audio minutes (STT/streaming/S2S — clip durations parsed locally), input+output tokens (LLM), characters (TTS). **saybench ships no built-in prices, deliberately**: vendor pricing drifts monthly, and a benchmark that ships stale rates lies with authority. `pricing.example.json` carries illustrative values and says exactly that. Unknown providers cost 0, never a guess.
