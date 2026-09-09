[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# Batch STT: `saybench stt`

The original mode: send each clip to each provider's batch transcription API, score the transcript against your reference, and report corpus-level WER with per-failure-mode categories, keyterm recall, and round-trip latency (avg/p95). Latency here is batch-API round-trip **including upload** — comparable across providers, but not the same thing as streaming time-to-first-partial, which is [its own mode](streaming.md) and is never blended with this one.

```
$ saybench stt -providers fake,deepgram,openai -manifest golden/manifest.jsonl -report today.json

PROVIDER                       CLIPS  ERRORS  WER    KEYTERM RECALL  AVG LATENCY  P95 LATENCY
deepgram:nova-3                14     0       3.8%   93.5%           926ms        2139ms
fake                           14     0       16.5%  67.7%           47ms         73ms
openai:gpt-4o-mini-transcribe  14     0       19.0%  74.2%           1319ms       1930ms

PROVIDER                       CATEGORY        CLIPS  WER
deepgram:nova-3                acronyms        2      8.8%
deepgram:nova-3                conversational  2      5.7%
deepgram:nova-3                date_time       2      0.0%
deepgram:nova-3                names           2      7.4%
deepgram:nova-3                numbers         2      0.0%
deepgram:nova-3                spelled_name    2      2.5%
deepgram:nova-3                technical       2      3.6%
fake                           acronyms        2      17.6%
fake                           conversational  2      17.1%
fake                           date_time       2      14.3%
fake                           names           2      14.8%
fake                           numbers         2      15.8%
fake                           spelled_name    2      20.0%
fake                           technical       2      14.3%
openai:gpt-4o-mini-transcribe  acronyms        2      0.0%
openai:gpt-4o-mini-transcribe  conversational  2      2.9%
openai:gpt-4o-mini-transcribe  date_time       2      25.7%
openai:gpt-4o-mini-transcribe  names           2      3.7%
openai:gpt-4o-mini-transcribe  numbers         2      55.3%
openai:gpt-4o-mini-transcribe  spelled_name    2      17.5%
openai:gpt-4o-mini-transcribe  technical       2      21.4%
```

*(Real output: the bundled golden set against live vendor APIs, September 2026 — plus the offline `fake` provider that runs with zero keys. Your audio will rank them differently; that is the point of the tool.)*

## What one real run teaches

That table hides a better story, which is exactly why saybench reports more than one number:

- **The headline flatters and slanders at the same time.** `gpt-4o-mini-transcribe` scores 19% overall WER — but per category it *beats* nova-3 on acronyms (0.0% vs 8.8%), names, and conversational speech. Its catastrophic categories are numbers (55.3%) and dates (25.7%) — and reading the hypotheses shows why: **it heard the digits perfectly and wrote `4739028` and `$247.63`** while the reference spells the words out. Literal WER counts formatting as error. (Deepgram has the inverse artifact: it spelled out "four hundred one" where the reference said `401`.) This is exactly what [`-normalize digits`](scoring.md) exists for — rescored with it, `gpt-4o-mini-transcribe` drops from 19.0% to 12.2% — and the per-clip hypotheses in the report let you see the difference between *misheard* and *reformatted* either way.
- **Keyterm recall catches what WER buries.** One vendor transcribed the Kubernetes deployment as "the Cuba Needs deployment" — a 15% WER clip, but a 100% useless transcript for a technical support call. Both vendors dropped the name "Priya Raghunathan". That's the metric that decides whether a voice agent can say your customer's name back. Definition and doctrine in [Scoring](scoring.md).
- **Latency is a real axis:** ~926ms vs ~1319ms average round-trip on identical clips.

## The manifest: your audio in five minutes

A corpus is a JSONL file, one line per clip:

```json
{"audio":"clips/refund-call.wav","reference":"I want a refund for order four two nine","category":"numbers","keyterms":["four two nine"]}
```

- `audio` — path to the clip, relative to the manifest file.
- `reference` — what was actually said; the transcript is scored against this.
- `category` — free-form failure-mode label (`names`, `numbers`, `acronyms`, …); WER is reported per category as well as overall.
- `keyterms` — optional list of the words that matter; [keyterm recall](scoring.md) reports how many survived, naming the misses per clip.

Batch mode sends clips as-is (`wav`, `mp3`, `flac`, `ogg`, `m4a`); the [streaming](streaming.md) and [S2S](s2s.md) modes feed PCM and take WAV input.

## The bundled golden set

`golden/` ships 14 short clips across seven agent-failure-mode categories, synthesized with macOS text-to-speech from originally written sentences (see [`golden/README.md`](../golden/README.md) for provenance). It exists so the tool works out of the box and CI has a stable corpus — **your own recorded audio is always the better benchmark**, and the manifest format makes that a five-minute job.
