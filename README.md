# saybench

**Benchmark speech-to-text on *your* audio, not a vendor's lab.**

Every STT vendor publishes benchmarks showing they're the fastest and most accurate. None of them ran on your audio, your accents, your jargon, or your phone-line quality — and none of them will tell you when an API update quietly makes *your* pipeline worse. saybench does: point it at your providers and your clips, get word-error-rate and latency per provider and per failure mode, and wire the regression gate into CI so a degradation fails the build before your users notice.

```
$ saybench stt -providers deepgram,openai -manifest golden/manifest.jsonl -report today.json

PROVIDER  CLIPS  ERRORS  WER    AVG LATENCY  P95 LATENCY
fake      14     0       16.5%  47ms         73ms

PROVIDER  CATEGORY        CLIPS  WER
fake      acronyms        2      17.6%
fake      conversational  2      17.1%
fake      date_time       2      14.3%
fake      names           2      14.8%
fake      numbers         2      15.8%
fake      spelled_name    2      20.0%
fake      technical       2      14.3%
```

*(Output above is a real run of the bundled offline `fake` provider — reproducible on your machine with zero API keys. Vendor tables belong in your reports, not this README.)*

## Why categories, not just one number

Voice agents don't fail on clean prose. They fail on **names** ("Beatriz Nakamura"), **spelled-out codes** ("B as in bravo"), **numbers** ("policy four seven three nine…"), **dates**, **acronyms**, and **disfluent real speech**. A single corpus-level WER hides exactly the failures that end calls badly. saybench reports per-category WER so you can see that a vendor which wins overall *loses on the clips that matter to you* — and the bundled golden set is organized around those failure modes.

## Install

```
go install github.com/renan-martini/saybench/cmd/saybench@latest
```

Or clone and `go build ./cmd/saybench`. Single static binary, no runtime dependencies.

## Quickstart

```bash
# 1. Zero-key smoke test with the offline fake provider and bundled corpus:
saybench stt -providers fake

# 2. Real vendors — keys come from the environment, never from files or flags:
export DEEPGRAM_API_KEY=...
export OPENAI_API_KEY=...
saybench stt -providers deepgram,openai -report baseline.json

# 3. Your own audio: write a JSONL manifest next to your clips…
#    {"audio":"clips/refund-call.wav","reference":"I want a refund for order four two nine","category":"numbers"}
saybench stt -providers deepgram -manifest my-corpus/manifest.jsonl -report today.json

# 4. Catch regressions — in CI, fail if any provider got >2 points worse:
saybench compare baseline.json today.json -max-wer-regression 2.0
```

## Providers

| Spec | Needs | Model override |
|---|---|---|
| `fake` | nothing — deterministic offline provider for CI and demos | — |
| `deepgram` | `DEEPGRAM_API_KEY` | `SAYBENCH_DEEPGRAM_MODEL` (default `nova-3`) |
| `openai` | `OPENAI_API_KEY` | `SAYBENCH_OPENAI_MODEL` (default `gpt-4o-mini-transcribe`) |

Adding a provider is one file implementing a two-method interface — see `internal/provider/provider.go` and [CONTRIBUTING.md](CONTRIBUTING.md).

## The bundled golden set

`golden/` ships 14 short clips across seven agent-failure-mode categories, synthesized with macOS text-to-speech from originally written sentences (see `golden/README.md` for provenance). It exists so the tool works out of the box and CI has a stable corpus — **your own recorded audio is always the better benchmark**, and the manifest format makes that a five-minute job.

## Honest numbers, stated plainly

- **WER is corpus-level** (total edits ÷ total reference words), with per-clip substitution/deletion/insertion breakdowns in the JSON report — a score you can debug, not just rank by.
- **Both sides are normalized** (case, punctuation) before scoring, so vendor formatting choices don't count as errors.
- **Latency here is batch-API round-trip** including upload — comparable across providers, but *not* the same as streaming time-to-first-token. Streaming latency is on the roadmap and will be reported separately, never blended.

## Roadmap

- [ ] TTS benchmarking: time-to-first-audio per vendor
- [ ] LLM benchmarking: streamed time-to-first-token for the conversation loop
- [ ] Streaming STT latency (interim/final timing), reported separately from batch
- [ ] Pipecat adapter: benchmark the services in your Pipecat pipeline directly
- [ ] Cost columns: $/hour of audio per provider
- [ ] More providers (PRs welcome — the interface is deliberately tiny)

## Design principles

- **Standard library only.** Zero runtime dependencies: nothing to audit, nothing to break, `go install` just works. A new dependency needs a reason the stdlib can't answer.
- **Keys from the environment only** — never flags, never config files, never logs. See [SECURITY.md](SECURITY.md).
- **Every network call has a deadline**; every response read is size-bounded.
- **Deterministic where possible** — the fake provider, result ordering, and report schema are all stable so diffs mean something.

## License

MIT — see [LICENSE](LICENSE).
