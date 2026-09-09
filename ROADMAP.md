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

- **v1.0** — the close-out: **TTS mode** (time-to-first-audio, streamed
  24 kHz PCM: openai, elevenlabs, custom, fake-tts) · **cost columns** from
  a user-supplied pricing table (no built-in prices, deliberately) ·
  **`-normalize digits`** (the formatting artifact becomes a recorded,
  compare-warned choice) · **s2s `-turn-ending server_vad`** (production
  posture, VAD hangover measured, silence tail appended) · **`saybench
  mcp`** (the whole tool as an MCP server for coding agents) · a Pipecat
  integration guide.

## Next

### Judge-scored semantic quality
Optional LLM-judge scoring beside literal WER for "meaning survived, words
differ" — an addition, clearly labeled, never a replacement.

### Barge-in latency (s2s)
How fast the model stops when interrupted mid-response. Deferred
deliberately: it needs mid-response interrupt choreography (feed a second
utterance while output audio is streaming, measure output cessation), which
is its own design, not an afternoon.

### Gemini Live adapter (and other non-OpenAI-dialect S2S vendors)
Deferred deliberately, not forgotten: the protocol moves fast and this repo
does not ship adapters it cannot live-verify — an unverifiable adapter is
worse than none. It is a one-file contribution behind the two-method
`S2SProvider` interface for anyone with a key; the AssemblyAI and
ElevenLabs adapters await live verification the same way.

### More providers everywhere
Every provider interface in this repo is two methods. PRs welcome.
