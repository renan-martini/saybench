# LLM transport dimension + warmup — design

Approved 2026-09-09.

1. `-warmup` on `saybench llm`, DEFAULT ON: one unmeasured throwaway request
   per target before benchmarking. The first live run proved cold TLS is a
   ~3x TTFT distortion and production voice agents run warm, so warm is the
   representative default; `-warmup=false` preserves the cold-start study.
   Reports record `warmup: true|false`; compare warns (not refuses) when the
   conditions differ. A failed warmup is a stderr warning, never fatal — the
   measured run will surface the real error with full context.
2. WS transport as provider prefix `openai-ws:model` → the Responses API
   WebSocket mode (`response.create` → `response.output_text.delta` →
   `response.completed`/`response.failed`), reusing the justified
   coder/websocket dependency. ONE persistent connection per target for the
   whole run, prompts serialized over it — connection reuse is what WS mode
   exists to provide, and sequential turns over a warm socket is exactly the
   voice-loop shape. URL override: SAYBENCH_OPENAI_WS_URL (default
   wss://api.openai.com/v1/responses; the public docs wrap the raw URL in
   SDKs, so the adapter ships protocol-tested against a local mock and
   live-unverified until a keyed run).
3. Usage over WS is under-documented: parse output tokens from
   response.completed when present, otherwise the ws target's tok/s renders
   as absent rather than a guess.
