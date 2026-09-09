[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# Streaming STT: `saybench stream`

`saybench stream` feeds your audio at **real-time pace** (50 ms PCM chunks on a wall clock) over each vendor's streaming API and measures what batch mode can't:

- **Time-to-first-partial** — first audio byte sent → first interim received. When your captions can start moving.
- **Finalization lag** — end of audio → last final segment. The dead air before your LLM can even start thinking.
- **Interim word survival** — of the final transcript's distinct words, the fraction any interim previewed. High = interims are trustworthy (safe for captions, barge-in, early LLM starts); low = the stream rewrites itself until finalization. Stated openly: append-only-delta vendors (OpenAI Realtime) score ~100% *by construction* — the metric is surfacing that their interims never lie, while replacement-hypothesis vendors (Deepgram) reveal how much they revise. Interim count ships beside it as the raw churn number.

WER, categories, and keyterm recall still score the final transcript ([scoring doctrine](scoring.md)). Streaming and batch reports carry a `mode` field, and `compare` **refuses** to compare across modes — the two latencies measure different things, and a delta between them would be a lie.

```bash
saybench stream -providers fake-stream                              # offline, zero keys
saybench stream -providers deepgram,openai-realtime,assemblyai -report stream.json
```

## Real results

September 2026 — the bundled golden set, fed at real-time pace to the live APIs:

```
PROVIDER                                CLIPS  ERRORS  WER    KEYTERM RECALL  TTFP AVG  TTFP P95  FINAL LAG AVG  FINAL LAG P95
deepgram-stream:nova-3                  14     0       4.6%   90.3%           1151ms    1158ms    213ms          235ms
openai-realtime:gpt-4o-mini-transcribe  14     0       19.4%  67.7%           5633ms    7339ms    753ms          1017ms
```

**This is the measurement batch mode cannot see.** In batch, these two vendors were ~400ms apart; under streaming, the **time-to-first-partial gap is 5×** (1.15s vs 5.6s avg), and finalization lag — the dead air before your LLM can start thinking — is 3.5× apart. WER stays consistent with batch for both (the digit-vs-spelled formatting artifact included), which is a good consistency check on the pipeline. Configuration note for the OpenAI number: this run used `turn_detection: null` with an explicit end-of-audio commit — the deterministic-measurement setup; server-VAD configurations may pace deltas differently.

## Vendors and verification

Deepgram live and OpenAI Realtime (transcription intent) are **live-verified** (the table above); AssemblyAI Universal-Streaming is implemented against its documented protocol and CI-tested, awaiting a live run. Each adapter is exercised in CI against a local WebSocket server speaking that vendor's dialect — the OpenAI one earned its keep on first live contact, catching a beta→GA protocol change. Every endpoint takes a URL override (`SAYBENCH_DEEPGRAM_STREAM_URL`, `SAYBENCH_OPENAI_REALTIME_URL`, `SAYBENCH_ASSEMBLYAI_STREAM_URL`) so self-hosted or compatible servers bench without code changes. The full matrix, including how to add a provider, is in [Providers](providers.md).
