# Streaming STT benchmarking — design

Approved 2026-09-09. Decisions made with the project owner; recorded here per
the brainstorming → plan → TDD workflow.

## Problem

Batch round-trip latency is a proxy. Live voice agents care about three
streaming numbers: how fast the first partial arrives, how long after the
audio ends the final transcript lands, and how much interims churn. saybench
must measure these against real streaming APIs, fed at real-time pace, and
must never blend them with batch numbers.

## Metrics (v1)

- **time_to_first_partial_ms** — first audio byte sent → first non-empty
  interim received.
- **final_lag_ms** — end of audio feed → last final segment received.
- **interims** — count of interim updates (churn proxy).
- Existing pipeline unchanged on the final transcript: WER, categories,
  keyterm recall.

**Deferred — interim stability** (recorded so the definition doesn't drift):
of the words in the final transcript, the fraction that appeared, unchanged,
in at least one earlier interim ("word-survival rate"). High = interims are
trustworthy previews; low = the stream flip-flops and any UI built on interims
lies to users until finalization. Additive to the schema; ships in a later
round.

## Decisions

1. **WebSocket via `github.com/coder/websocket`** (decision: option B).
   Justification recorded in CONTRIBUTING.md: the stdlib has no WebSocket
   client; a hand-rolled RFC 6455 implementation would be ~300 lines of
   security-sensitive framing code maintained forever, versus one small,
   widely-used, actively-maintained library that also provides the server
   side our protocol tests are written against. This is the stdlib-only
   rule's documented-exception path working as intended.
2. **Vendors this round:** Deepgram live, OpenAI Realtime (transcription
   intent), AssemblyAI Universal-Streaming v3 — plus a deterministic
   `fake-stream` for CI. Every vendor adapter is written against a local
   in-test WebSocket server speaking that vendor's documented protocol;
   live verification happens with the operator's keys, same as batch.
3. **Provider-agnostic by construction:** the `StreamingProvider` interface
   is two methods; keys come from env only; each vendor takes a model AND a
   URL override env var (`SAYBENCH_<VENDOR>_MODEL` / `..._URL`) so
   self-hosted or compatible endpoints can be benched without code changes.
   README gains an "adding your own provider" section pointing at the
   smallest adapter as the template.
4. **No blending, structurally:** reports carry `mode` ("batch" |
   "streaming"; absent = batch for old files). `compare` refuses to compare
   across modes. The dashboard labels the mode and switches its latency
   panel to TTFP / final-lag for streaming runs. Schema change is additive —
   SchemaVersion stays 1.
5. **Pacing:** audio is fed on a wall-clock ticker in 50 ms PCM chunks
   (real-time), because blasting the file measures nothing. OpenAI Realtime
   expects 24 kHz pcm16, so the feeder resamples 16 kHz → 24 kHz with linear
   interpolation (documented; resampling quality is not part of what we're
   measuring).

## Out of scope this round

Interim stability scoring; streaming TTS; per-word timestamps; diarization.
