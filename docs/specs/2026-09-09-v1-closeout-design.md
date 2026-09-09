# v1.0 close-out — remaining roadmap — design

Blanket-approved 2026-09-09 ("just finish the remaining roadmap"); individual
gates waived, judgment calls recorded here for veto:

1. `-normalize digits` (stt/stream/s2s-echo): canonicalizes single-digit
   words ↔ digit strings before scoring ("4739028" ≡ "four seven three nine
   zero two eight"). SCOPE LIMIT stated: single-digit and digit-run
   equivalence only — full number semantics ("$247.63" ≡ "two hundred forty
   seven dollars") is real NLP and stays out; the flag's docs say so.
2. TTS mode: `saybench tts` measures time-to-first-audio-byte and total
   synthesis time over the llm prompt manifest's text (short voice-agent
   replies). Providers: fake-tts, openai (/v1/audio/speech, streamed),
   elevenlabs (stream endpoint), custom (OpenAI-compatible /audio/speech via
   SAYBENCH_TTS_BASE_URL). Fifth report mode.
3. Cost columns: built-in pricing table with an as-of date, `-pricing
   file.json` override; $ per run and per unit shown in llm and tts
   summaries (audio modes deferred to the pricing file since audio pricing
   varies by tier).
4. S2S turn-ending dimension: `-turn-ending commit|server_vad` — server_vad
   measures V2V from end-of-audio-feed (the VAD must detect the end), the
   production posture; recorded as a condition, compare warns on mixes.
5. MCP server mode: `saybench mcp` — stdio JSON-RPC 2.0 implementing the
   MCP tools surface (initialize, tools/list, tools/call) with zero new
   dependencies. Tools: run_stt, run_llm, compare_reports, read_report.
   Offline-testable by piping messages.
6. Pipecat integration ships as a DOCUMENTED GUIDE (service→provider
   mapping + commands), not code: Pipecat pipelines configure services in
   Python, there is no manifest to parse — a guide is the honest artifact.
7. DEFERRED with rationale (the judgment calls): Gemini Live adapter
   (fast-moving proprietary protocol + no key here to live-verify — an
   unverifiable adapter is worse than none; one-file contribution path
   documented) and barge-in latency (requires mid-response interrupt
   choreography; its own design). ROADMAP says both, plainly.
