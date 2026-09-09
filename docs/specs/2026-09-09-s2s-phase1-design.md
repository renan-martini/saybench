# S2S phase 1 — voice-to-voice latency — design

Approved 2026-09-09 with one amendment: agnostic by construction.

- Metric: V2V-TTFA — the instant the user's turn ends (commit +
  response.create sent, deterministic) → first output audio byte. Plus
  response-done time and output audio duration (computed from bytes).
  Fourth report mode "s2s"; cross-mode compare refusal applies.
- Turn ending is deterministic (turn_detection null + commit +
  response.create), same doctrine as transcription benching. Server-VAD
  posture is a follow-up dimension, never blended.
- Vendors v1: `openai` (Realtime speech-to-speech, model default
  gpt-realtime, SAYBENCH_OPENAI_S2S_MODEL / SAYBENCH_OPENAI_S2S_URL
  overrides) and `fake-s2s` for CI. AGNOSTIC AMENDMENT: `custom` speaks the
  same OpenAI-Realtime dialect against SAYBENCH_S2S_URL (+ _API_KEY,
  _MODEL) — emerging S2S vendors clone that dialect; anything that doesn't
  is a one-file adapter behind the two-method S2SProvider interface,
  documented in the README (Gemini Live is the named next adapter).
- One fresh connection per clip, deliberately: realtime sessions are
  stateful conversations, and separate clips must be separate conversations
  (context contamination would corrupt the measurement). This differs from
  the LLM-WS one-connection rule for a reason, stated in code.
- Phase-2 groundwork: output audio TRANSCRIPT captured as the item's
  hypothesis; session instructions configurable (default "Respond briefly
  and naturally."). No scoring in phase 1.
- Event names parsed defensively across GA/beta renames
  (response.output_audio.delta AND response.audio.delta, likewise
  transcript deltas) — the Realtime rename bit us once already.
