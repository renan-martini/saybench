[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# TTS mode: `saybench tts`

`saybench tts` synthesizes short voice-agent-shaped utterances (the llm golden set's user turns, or your own) and measures **time-to-first-audio-byte** — the number that gates when the caller starts hearing the reply — plus total synthesis time and audio duration (all streams are requested as 24 kHz PCM so duration is computable, never guessed):

```
$ saybench tts -providers fake-tts

PROVIDER  UTTERANCES  ERRORS  TTFA AVG  TTFA P95  SYNTH TOTAL AVG  AUDIO OUT AVG
fake-tts  8           0       140ms     198ms     533ms            3187ms
```

Providers: `openai` (`/audio/speech`), `elevenlabs` (stream endpoint), `custom` (any OpenAI-compatible `/audio/speech` via `SAYBENCH_TTS_BASE_URL`), `fake-tts` for CI. Both vendor adapters are protocol-tested against local mocks and await live verification. Keys, voice and model overrides in [Providers](providers.md).
