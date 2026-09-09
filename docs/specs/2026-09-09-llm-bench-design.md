# LLM conversation-loop benchmarking — design

Approved 2026-09-09 (target-spec scheme option 1; HTTP SSE only in v1;
latency-only, no quality scoring in v1).

## What and why

After STT finalizes, the LLM turn gates the whole voice loop — TTS cannot
start until the first token arrives. Metrics: streamed time-to-first-token,
completion time, and decode speed (output tokens/sec), per prompt and
aggregated (avg/p95). Third report mode: "llm"; cross-mode compare refusal
already applies.

## Decisions

1. Targets are `[provider:]model` specs — `openai` (default), `openrouter`,
   `groq`, `custom` — one generic OpenAI-compatible SSE client under all of
   them. Keys per provider env (OPENAI_API_KEY, OPENROUTER_API_KEY,
   GROQ_API_KEY); `custom` reads SAYBENCH_LLM_BASE_URL +
   SAYBENCH_LLM_API_KEY, covering vLLM/Ollama/self-hosted without code.
   `fake-llm` is the offline CI target.
2. HTTP SSE streaming only in v1 (`stream:true`,
   `stream_options.include_usage`); WebSocket transport comparison is a
   follow-up dimension.
3. Latency only. Outputs are captured in the report for reading; no judge,
   no correctness assertions (quality scoring is its own roadmap item).

## Workload

`llm/golden.jsonl` — originally written, voice-agent-shaped chat scenarios:
short system prompt, optional brief history, one user turn, small
max_tokens, categorized (greeting / faq / scheduling / extraction). BYO
manifests are first-class. In llm mode per-category summaries are omitted
in v1 (they would render WER, which does not exist here); the per-item rows
carry categories for external analysis.

## Testing

The SSE client is exercised against a local mock OpenAI-compatible server
(chunk delays, usage frame, [DONE], auth header assertions) — which is also
the proof that the `custom` target works. Live verification with operator
keys afterwards, as with batch and streaming.
