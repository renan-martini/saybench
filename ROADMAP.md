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

- **v0.4** — **Streaming STT**: `saybench stream` feeds audio at real-time
  pace over vendor WebSocket APIs (Deepgram live, OpenAI Realtime,
  AssemblyAI Universal-Streaming) and measures time-to-first-partial,
  finalization lag, and interim count. Mode-tagged reports; `compare`
  refuses cross-mode deltas; adapters tested against local protocol
  servers.
- **v0.5** — **Interim word survival**: of the final's distinct words, the
  fraction any interim previewed — the "were the interims telling the
  truth?" score, word-weighted per provider, in the table and dashboard.

## Next

### 1. LLM conversation-loop benchmarking
Streamed time-to-first-token and full completion time for the models driving
the conversation — against **any OpenAI-compatible endpoint** (OpenAI,
OpenRouter, Groq, local servers), because that is how real stacks actually
route models. Same report/compare/dashboard machinery; transport (HTTP vs
WebSocket) as a dimension where the endpoint supports both.

### 2. TTS time-to-first-audio
The other half of response latency: how long from send to the first audible
byte, per vendor and voice. TTFA is the number a caller hears.

### 3. MCP server mode (`saybench mcp`)
Benchmarks should be usable by coding agents, not just humans. An MCP server
exposing `run_stt_bench`, `compare_reports`, and `read_report` lets an LLM
assistant benchmark the pipeline it is editing — before a feature ships, as
part of its own loop. The `-format json` output is the foundation; this makes
it native.

### 4. Pipecat adapter
Point saybench at a Pipecat pipeline's configured STT/TTS/LLM services and
bench exactly what the pipeline runs, not a hand-maintained parallel config.

### 5. Cost columns
$/hour of audio per provider next to WER and latency, from a maintained
pricing table — model choices are three-axis trade-offs; the report should
show all three axes.

### 6. Scoring depth
- Text-normalization options (number formats: "401" vs "four oh one" —
  today's literal scoring counts formatting as error; make that a choice).
- Optional judge-based semantic scoring beside literal WER, for "meaning
  survived, words differ" cases. Never a replacement for WER — an addition,
  clearly labeled.

### 7. More providers
The interface is two methods. PRs welcome — see CONTRIBUTING.md.
