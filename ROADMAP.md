# Roadmap

Grounded in real voice-agent stacks: platforms running live AI phone calls, and
meeting/conversation pipelines built on frameworks like Pipecat + LiveKit. Each
item exists because a real system needed it, not because a benchmark could
measure it. Order is intent, not promise.

## Shipped

- **v0.1** — STT benchmarking: corpus-level WER with sub/del/ins breakdown,
  batch latency (avg/p95), per-failure-mode categories, JSON reports,
  `compare` with a CI regression gate, deterministic offline `fake` provider,
  bundled clean-room golden set.
- **v0.2** — **Keyterm recall** (did the names/codes/terms survive?), `-format
  json` on every command, and the **HTML dashboard**: single self-contained
  file with A/B run comparison, WER trends, category/latency charts, and a
  worst-clips table.

## Next

### 1. Streaming STT latency
Batch round-trip is comparable but it is not what a live call feels like. Add
streaming benchmarks: time-to-first-partial, finalization lag after end of
speech, and interim-result stability — the numbers that decide whether an
agent talks over its caller. Reported separately from batch, never blended.

### 2. LLM conversation-loop benchmarking
Streamed time-to-first-token and full completion time for the models driving
the conversation — against **any OpenAI-compatible endpoint** (OpenAI,
OpenRouter, Groq, local servers), because that is how real stacks actually
route models. Same report/compare/dashboard machinery; transport (HTTP vs
WebSocket) as a dimension where the endpoint supports both.

### 3. TTS time-to-first-audio
The other half of response latency: how long from send to the first audible
byte, per vendor and voice. TTFA is the number a caller hears.

### 4. MCP server mode (`saybench mcp`)
Benchmarks should be usable by coding agents, not just humans. An MCP server
exposing `run_stt_bench`, `compare_reports`, and `read_report` lets an LLM
assistant benchmark the pipeline it is editing — before a feature ships, as
part of its own loop. The `-format json` output is the foundation; this makes
it native.

### 5. Pipecat adapter
Point saybench at a Pipecat pipeline's configured STT/TTS/LLM services and
bench exactly what the pipeline runs, not a hand-maintained parallel config.

### 6. Cost columns
$/hour of audio per provider next to WER and latency, from a maintained
pricing table — model choices are three-axis trade-offs; the report should
show all three axes.

### 7. Scoring depth
- Text-normalization options (number formats: "401" vs "four oh one" —
  today's literal scoring counts formatting as error; make that a choice).
- Optional judge-based semantic scoring beside literal WER, for "meaning
  survived, words differ" cases. Never a replacement for WER — an addition,
  clearly labeled.

### 8. More providers
The interface is two methods. PRs welcome — see CONTRIBUTING.md.
