[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# Scoring: honest numbers, stated plainly

Every metric in saybench is designed to be debuggable and hard to accidentally lie with. The rules, and where each one comes from:

## WER is corpus-level

Total edits ÷ total reference words — not an average of per-clip rates, so long clips weigh more, which is the standard convention. Per-clip substitution/deletion/insertion breakdowns live in the JSON report: a score you can debug, not just rank by. When every item in a run errored there is no corpus to score — WER is `-1` in the JSON and renders as `—`, never as a flattering 0%.

## Both sides are normalized before scoring

Case and punctuation are stripped from reference and hypothesis alike, so vendor formatting choices don't count as errors.

## Digit formatting is a choice, not a surprise

`-normalize digits` canonicalizes digit strings against spelled-out digits ("4739028" ≡ "four seven three nine zero two eight") before scoring — opt-in, recorded on the report, warned about in `compare` when runs mix normalizations. Full number semantics ("$247.63" vs "two hundred forty seven dollars") stays out of scope, and the flag's docs say so. Rescoring the published runs with it: `gpt-4o-mini-transcribe` drops from 19.0% to **12.2%** (a third of its "errors" were formatting), the S2S echo run from 27.0% to 20.7%, and `nova-3` from 3.8% to 3.0%.

## Keyterm recall: the metric WER hides

A transcript can score 95% on WER and still be useless — because the 5% it missed was the customer's name, the policy number, and the callback date. Add `"keyterms": ["Beatriz Nakamura", "four seven three nine"]` to any manifest line and saybench reports **keyterm recall** separately: of the words that matter, how many survived transcription intact — with the misses named per clip. Corpus-level (total hits over total terms); `—` when the corpus defines no keyterms.

## Why categories, not just one number

Voice agents don't fail on clean prose. They fail on **names** ("Beatriz Nakamura"), **spelled-out codes** ("B as in bravo"), **numbers** ("policy four seven three nine…"), **dates**, **acronyms**, and **disfluent real speech**. A single corpus-level WER hides exactly the failures that end calls badly. saybench reports per-category WER so you can see that a vendor which wins overall *loses on the clips that matter to you* — and the [bundled golden set](stt.md) is organized around those failure modes.

## Judge scoring is opt-in and additive, never a replacement

`-judge <llm-target>` (on `stt`, `stream`, and `s2s -score echo`) has an LLM rate each transcript against its reference on a fixed rubric — "does the meaning survive?", one number 0–100 — and a JUDGE column lands beside WER, which stays exactly where it was. One LLM call per scored item, so it costs what your judge costs. The judge target is recorded on the report and `compare` warns when runs used different judges: judge scores are judge-relative. `fake-llm` answers the rubric deterministically, so the whole path runs offline in CI.

## Latencies from different modes never blend

Batch latency is API round-trip including upload; streaming latency is time-to-first-partial and finalization lag under real-time pacing; S2S latency is voice-to-voice. They measure different things, each mode reports its own, and `compare` refuses to diff reports from different modes outright. Within a mode, runs measured under different conditions — warm vs cold, echo vs conversational, commit vs server_vad, normalized vs not, judged by different judges, barge-in vs normal — are flagged with a warning, because a delta across conditions mixes experiments. The full list of recorded conditions is in [Reports & CI](reports.md).
