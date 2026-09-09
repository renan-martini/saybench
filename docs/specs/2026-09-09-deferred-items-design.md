# v1.1 — the deferred items — design

Blanket-approved 2026-09-09 ("implement all of them"). Judgment call
recorded: Gemini Live's deferral rationale (no key here to live-verify)
still holds technically — the owner overrode it, so the adapter ships
protocol-tested against a local mock and clearly marked live-unverified,
the same ladder every adapter climbed.

1. **Judge scoring** — `-judge <llm-target>` on the reference-scored
   surfaces (stt, stream, s2s -score echo). After the bench, each scored
   item is rated by the judge LLM on a fixed rubric ("does the transcript
   preserve the meaning of the reference? answer one number 0-100").
   AvgJudgeScore beside WER — an ADDITION, never a replacement; the judge
   target is recorded on the report (different judges = different
   experiments; compare warns). Each judged item costs one LLM call; the
   README says so. fake-llm answers the rubric deterministically so the
   whole path runs offline in CI.
2. **Barge-in latency** — `saybench s2s -barge-in` (implies server_vad;
   the model must be able to DETECT the interruption). After the reply's
   first audio, a fixed 700ms passes, then a clean-room interrupt clip
   ("Wait, wait — stop for a second") feeds at real-time pace. Metric:
   barge_in_stop_ms = first interrupting byte sent → last output audio
   delta received (when the model actually shut up); response.done or a
   2s delta-silence closes the turn. Recorded as a condition.
3. **Gemini Live adapter** — s2s provider "gemini" speaking
   BidiGenerateContent over WS (setup / setupComplete / realtimeInput
   mediaChunks at 16kHz in / serverContent inlineData 24kHz out +
   outputTranscription). Gemini's turn handling is automatic-VAD-shaped:
   commit turn-ending is refused with a clear error; server_vad semantics
   apply (silence tail, V2V from end of speech). Key: GEMINI_API_KEY
   (GOOGLE_API_KEY fallback); model SAYBENCH_GEMINI_S2S_MODEL (default
   gemini-live-2.5-flash — Google renames models often; the error path
   surfaces their message). Live-unverified until someone runs a key.
