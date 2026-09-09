[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# Providers

Keys come **from the environment only** — never flags, never config files, never logs (see [SECURITY.md](../SECURITY.md)). Every `fake*` provider is deterministic and needs nothing, so every mode runs offline in CI and demos. Every vendor endpoint takes a URL override, so self-hosted or compatible servers bench without code changes.

## Batch STT (`saybench stt -providers …`)

| Spec | Needs | Overrides |
|---|---|---|
| `fake` | nothing | — |
| `deepgram` | `DEEPGRAM_API_KEY` | `SAYBENCH_DEEPGRAM_MODEL` (default `nova-3`) |
| `openai` | `OPENAI_API_KEY` | `SAYBENCH_OPENAI_MODEL` (default `gpt-4o-mini-transcribe`) |

## Streaming STT (`saybench stream -providers …`)

| Spec | Needs | Overrides |
|---|---|---|
| `fake-stream` | nothing | — |
| `deepgram` | `DEEPGRAM_API_KEY` | `SAYBENCH_DEEPGRAM_MODEL`, `SAYBENCH_DEEPGRAM_STREAM_URL` |
| `openai-realtime` | `OPENAI_API_KEY` | `SAYBENCH_OPENAI_REALTIME_MODEL`, `SAYBENCH_OPENAI_REALTIME_URL` |
| `assemblyai` | `ASSEMBLYAI_API_KEY` | `SAYBENCH_ASSEMBLYAI_STREAM_URL` |

## LLM (`saybench llm -targets [provider:]model,…`)

| Target family | Needs | Overrides |
|---|---|---|
| `fake-llm` | nothing | — |
| `openai:` (default) | `OPENAI_API_KEY` | — |
| `openai-ws:` (Responses API over WebSocket) | `OPENAI_API_KEY` | `SAYBENCH_OPENAI_WS_URL` |
| `openrouter:` | `OPENROUTER_API_KEY` | — |
| `groq:` | `GROQ_API_KEY` | — |
| `custom:` (vLLM, Ollama, any OpenAI-compatible) | `SAYBENCH_LLM_BASE_URL` (+ `SAYBENCH_LLM_API_KEY`, optional for local servers) | — |

The `-judge` flag ([scoring](scoring.md)) takes any of these targets as the judge.

## S2S (`saybench s2s -providers …`)

| Spec | Needs | Overrides |
|---|---|---|
| `fake-s2s` | nothing | — |
| `openai` | `OPENAI_API_KEY` | `SAYBENCH_OPENAI_S2S_MODEL` (default `gpt-realtime`), `SAYBENCH_OPENAI_S2S_URL` |
| `gemini` | `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) | `SAYBENCH_GEMINI_S2S_MODEL` (default `gemini-live-2.5-flash`), `SAYBENCH_GEMINI_S2S_URL` |
| `custom` (any OpenAI-Realtime-dialect endpoint) | `SAYBENCH_S2S_URL` (+ `SAYBENCH_S2S_API_KEY`, `SAYBENCH_S2S_MODEL`) | — |

`gemini` requires `-turn-ending server_vad` — the Live API's turn handling is automatic-VAD only, and saybench refuses to pretend otherwise.

## TTS (`saybench tts -providers …`)

| Spec | Needs | Overrides |
|---|---|---|
| `fake-tts` | nothing | — |
| `openai` | `OPENAI_API_KEY` | `SAYBENCH_OPENAI_TTS_MODEL` (default `gpt-4o-mini-tts`), `SAYBENCH_OPENAI_TTS_VOICE` (default `alloy`) |
| `elevenlabs` | `ELEVENLABS_API_KEY` | `SAYBENCH_ELEVENLABS_MODEL` (default `eleven_turbo_v2_5`), `SAYBENCH_ELEVENLABS_VOICE` |
| `custom` (any OpenAI-compatible `/audio/speech`) | `SAYBENCH_TTS_BASE_URL` (+ `SAYBENCH_TTS_API_KEY`, `SAYBENCH_TTS_MODEL`, `SAYBENCH_TTS_VOICE`) | — |

## The verification ladder

Every adapter climbs the same ladder, and its rung is stated rather than implied:

1. **Protocol-tested** — exercised in CI against a local mock server speaking the vendor's documented dialect.
2. **Live-verified** — has produced published numbers against the real endpoint.

Live-verified today: Deepgram (batch + streaming), OpenAI (batch, streaming, LLM over SSE and WS, S2S conversational + echo). Protocol-tested, awaiting a first live run: AssemblyAI streaming, OpenAI TTS, ElevenLabs, Gemini Live S2S, `openrouter`/`groq` LLM targets. The mocks have earned their keep — the OpenAI Realtime adapter's first live contact caught a beta→GA protocol change the mock was then extended to cover.

## Adding your own provider

Implement two methods — `Name()` and the mode's run method (`Transcribe`, `StreamTranscribe`, `Complete`, `Converse`, or `Speak`) — and register the spec string. `internal/provider/assemblyai_stream.go` is the smallest streaming template: dial, feed paced chunks via the shared `feed` helper, map the vendor's interim/final messages onto the shared `collector`, done. PRs welcome; the protocol-test pattern in `stream_vendors_test.go` is the contract, and [CONTRIBUTING.md](../CONTRIBUTING.md) has the rules.
