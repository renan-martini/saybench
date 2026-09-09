# LLM Transport + Warmup Implementation Plan

1. runner.Warmup(ctx, targets, timeout) — per-target throwaway request,
   errors returned for warning display. Test with a counting fake.
2. Report.Warmup bool (round-trip) + report.ConditionNote(a,b) for the
   compare warning. Tests first.
3. openai-ws adapter: lazy dial, mutex-serialized Completes over one conn,
   response.create with max_output_tokens, TTFT at first output_text.delta,
   completion + optional usage at response.completed, response.failed
   surfaces the server message. Mock-server tests assert model,
   max_output_tokens, TWO Completes over ONE connection, and failure
   surfacing.
4. CLI: -warmup flag (default true), rep.Warmup, compare warning print,
   usage text; dashboard meta shows warm/cold in llm mode. CI smoke covers
   -warmup=false and the ws prefix rejection-without-key path.
5. README (transport + warmup + one-connection semantics), ROADMAP tick,
   v0.7.0, full verification, push.
