# Interim Stability Implementation Plan

Spec: docs/specs/2026-09-09-interim-stability-design.md. TDD per task.

1. wer.WordSurvival(final, interims) (hit, total int) — distinct normalized
   final words present in the union of interim words. Tests: exact, partial,
   no interims, repeated words count once, normalization applies.
2. StreamResult.InterimTexts; collector stores snapshots; fake-stream emits
   two deterministic cumulative snapshots. Tests updated.
3. Runner computes survival; ItemResult.InterimSurvivalHit/Total;
   Summary.InterimWordSurvival (word-weighted, -1 sentinel). Tests.
4. Streaming table + dashboard column; CI smoke greps the column.
5. Docs (README metric bullet + append-only note; ROADMAP tick), v0.5.0,
   full verification, push.
