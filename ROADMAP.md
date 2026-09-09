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
- **v0.6** — **LLM conversation-loop benchmarking**: `saybench llm` streams
  chat completions against any OpenAI-compatible endpoint (openai /
  openrouter / groq / custom base URL) and measures TTFT, completion time,
  and decode tok/s over a voice-agent-shaped prompt set. Third report mode;
  dashboard follows. (WebSocket-transport comparison stays below.)

- **v0.9** — **S2S phase 2**: `-score echo` — the repeat-back task, scored
  with WER + keyterm recall against the model's own reply transcript;
  echo vs conversational recorded as a compare-warned condition.
- **v0.8** — **S2S phase 1**: `saybench s2s` measures voice-to-voice
  latency (turn end → first output audio), response-done time, and speech-out
  duration against speech-to-speech models — OpenAI Realtime today, any
  OpenAI-Realtime-dialect endpoint via `custom`, one-file adapters for the
  rest. Deterministic turn ending; the reply transcript is captured per turn
  as phase-2 raw material.
- **v0.7** — **Transport dimension + warmup**: `openai-ws:model` benches the
  same model over the Responses API WebSocket mode (one persistent
  connection, prompts serialized — the voice-loop shape); `-warmup` (default
  on) gives every target an unmeasured first request so TTFT reflects warm
  connections, recorded in the report, with compare warning on
  warm-vs-cold.

## Next

### S2S follow-ups
Server-VAD posture as a labeled dimension, barge-in latency, and a Gemini
Live adapter. (Phase 2 — echo-elicitation comprehension scoring — shipped
in v0.9.)

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
