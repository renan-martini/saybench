[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# LLM mode: `saybench llm`

After STT finalizes, nothing happens until the LLM's first token — TTS can't start speaking. `saybench llm` streams chat completions and measures **time-to-first-token**, completion time, and decode speed, per prompt and aggregated:

```
$ saybench llm -targets fake-llm

TARGET    PROMPTS  ERRORS  TTFT AVG  TTFT P95  COMPLETION AVG  TOK/S
fake-llm  8        0       208ms     273ms     533ms           42.6
```

**Warm by default:** every target gets one unmeasured throwaway request before the bench (`-warmup=false` to study cold starts instead) — the first live run showed cold TLS setup is a ~3× TTFT effect, and production voice agents run warm. Reports record which posture was measured, and `compare` warns when a warm run meets a cold one.

**Transport is a dimension:** `openai-ws:gpt-4o-mini` benches the same model over the Responses API's WebSocket mode — one persistent connection per target with prompts serialized over it, because connection reuse is the thing WS mode exists to provide (a fresh socket per prompt would erase what's being measured). Put both transports in one table — real run, September 2026, warm connections:

```
TARGET                 PROMPTS  ERRORS  TTFT AVG  TTFT P95  COMPLETION AVG  TOK/S
openai-ws:gpt-4o-mini  8        1       664ms     1142ms    849ms           115.6
openai:gpt-4o-mini     8        0       595ms     1103ms    762ms           109.9
```

**The honest verdict: with warm connections, WS mode buys nothing at voice-turn sizes** — SSE and WS land within ~70ms of each other (single 8-prompt run; treat as directional). The louder finding is what warmup did: the same SSE target measured **1109ms avg cold vs 595ms warm** — the transport you hold matters less than *that you hold it*. WS mode's documented wins are elsewhere (multiplexing, tool-call-heavy agentic loops), not raw single-turn TTFT. The one WS error in the table was the server cleanly reaping the persistent connection between prompts; the adapter now redials and resends once when that happens before any output (a production behavior, test-locked).

## Targets

Targets are `[provider:]model` against **any OpenAI-compatible endpoint** — `openai` (default), `openrouter`, `groq`, or `custom` via `SAYBENCH_LLM_BASE_URL` (vLLM, Ollama, self-hosted — no code changes):

```bash
saybench llm -targets gpt-4o-mini,groq:llama-3.3-70b-versatile,openrouter:google/gemini-2.5-flash -report llm.json
```

Keys and overrides per target family are in [Providers](providers.md).

## Real results

September 2026 — the bundled prompt set against the live API, before `-warmup` existed (cold connections included, which is the finding):

```
TARGET              PROMPTS  ERRORS  TTFT AVG  TTFT P95  COMPLETION AVG  TOK/S
openai:gpt-4o-mini  8        0       1109ms    1885ms    1271ms          115.2
```

Two honest readings of that table. First, **TTFT dominates**: completion lands only ~160ms after the first token at voice-turn lengths — the wait is almost entirely time-to-first-token, which is why it's the headline column. Second, a methodological finding the per-prompt data exposed: TTFT was **bimodal (~1.7s vs ~500ms), splitting exactly along worker waves** — the first four concurrent requests paid cold TLS connection setup, the second four reused warm connections. That finding is why `-warmup` now exists and defaults to on: real voice agents hold connections warm. The cold numbers stay published because benchmarks that don't tell you what's inside their averages are lying with them.

## The prompt set

The bundled `llm/golden.jsonl` is voice-agent-shaped — short system prompts, brief histories, disfluent user turns, small token caps — because that's the workload a conversation loop actually runs, not essay generation. Latency only, by design: outputs are captured in the report for reading, and LLM output quality stays out of scope (the [`-judge` rubric](scoring.md) scores transcript preservation, which is a different question).
