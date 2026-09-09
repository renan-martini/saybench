# Interim stability ("word survival") — design

Approved 2026-09-09 (option A: distinct-word counting).

## Metric

Of the distinct normalized words in the final transcript, the fraction that
appeared in at least one earlier interim. High = interims are trustworthy
previews; low = the stream revises itself and anything built on interims
(captions, barge-in, early LLM starts) lies until finalization.

- Distinct-word counting (option A). A word repeated in the final counts
  once. Chosen over per-occurrence multisets because vendors stream with
  different semantics — Deepgram sends replacement hypotheses, OpenAI sends
  append-only deltas — and occurrence accounting would need per-vendor
  accumulation rules. Documented limitation, revisitable.
- Consequence stated openly: append-only delta vendors score ~100% by
  construction. That is the point — the metric surfaces the semantic
  difference ("these interims never lie; those get rewritten").
- Sentinel -1 (renders "—") when a clip produced no interims or no final
  words. Corpus aggregation is word-weighted (sum survived / sum distinct),
  matching how WER aggregates.

## Mechanics

Collector keeps each interim's text (it previously kept only a count);
scoring lives beside WER in the wer package; the runner computes per-item
survival; report carries hit/total ints plus the summary rate; streaming
table and dashboard grow a column. Batch mode untouched; schema additive.
