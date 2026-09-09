# S2S phase 2 — comprehension via echo elicitation — design

Approved 2026-09-09.

- `saybench s2s -score echo` swaps session instructions to a FIXED echo task
  ("Repeat back exactly, word for word, what the caller just said. Say
  nothing else.") and scores the model's own reply transcript against the
  clip's reference with the existing WER + keyterm machinery.
- Echo vs conversational is a recorded CONDITION (`s2s_scoring` on the
  report); ConditionNote warns when a compare mixes them. Conversational
  runs render WER/keyterm as "—" (nothing scored), never 0%.
- Fixed instructions, deliberately: comparability across runs and vendors
  depends on the task being identical. No custom instructions in v1.
- Stated limitations (README, not user-discovered): the digit-formatting
  artifact applies (heard-perfectly may echo "4739028" against a spelled
  reference); the scored text is the model's own transcript of its own
  speech, so the metric scores the WHOLE loop — hear, speak, self-transcribe
  — which is what a caller experiences anyway.
