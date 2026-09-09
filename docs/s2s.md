[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# S2S mode: `saybench s2s`

Speech-to-speech models collapse STT → LLM → TTS into one speech-native model — and their defining number is **voice-to-voice latency**: the instant the user's turn ends until the first byte of spoken reply. `saybench s2s` feeds each golden clip as a conversational turn at real-time pace and measures exactly that, plus response-done time and how long the agent talks back:

```
$ saybench s2s -providers fake-s2s

PROVIDER  TURNS  ERRORS  V2V FIRST AUDIO AVG  V2V P95  RESPONSE DONE AVG  SPEECH OUT AVG
fake-s2s  14     0       503ms                690ms    1917ms             1217ms
```

- **Deterministic turn ending by default** (`turn_detection: null` + explicit commit + `response.create`), same doctrine as streaming: V2V is measured from a known instant, not the server VAD's guess. `-turn-ending server_vad` opts into the production posture instead — the model detects end-of-speech itself, a 1.5s silence tail lets its VAD fire, and the measured V2V then *includes* VAD hangover, which is the point. The posture is recorded on the report and `compare` warns when the two mix.
- **One fresh connection per clip, deliberately** — the opposite of the LLM-WS rule, for a reason: realtime sessions are stateful conversations, and separate clips must be separate conversations.
- **Agnostic by construction:** `openai` (Realtime speech-to-speech, default `gpt-realtime`, overridable) works today; **`custom` points the same OpenAI-Realtime dialect at your own endpoint** via `SAYBENCH_S2S_URL` — emerging S2S vendors clone that dialect the way everyone cloned chat completions. Anything that doesn't is a one-file adapter behind the two-method `S2SProvider` interface — and **`gemini` speaks Google's Live API** (BidiGenerateContent over WS) natively: `GEMINI_API_KEY` (or `GOOGLE_API_KEY`), default model `gemini-live-2.5-flash` (`SAYBENCH_GEMINI_S2S_MODEL` overrides — Google renames models often, and the endpoint's own error is surfaced when that happens). The Live API's turn handling is automatic-VAD only, so `gemini` refuses `-turn-ending commit` with instructions rather than silently measuring something else. Protocol-tested against a local mock; awaiting live verification, the same ladder every adapter here climbed.
- **Phase 2 — comprehension, via echo elicitation (`-score echo`):** an S2S model never tells you what it heard, only how it replied — so the echo task instructs it to *repeat back verbatim what the caller said*, and the model's own reply transcript is scored with the same WER + keyterm machinery as everything else. The golden set's failure-mode categories become an S2S comprehension benchmark. Echo and conversational are recorded **conditions**: the report carries `s2s_scoring`, `compare` warns when they mix, and unscored runs show `—`, never 0%. Two limitations stated up front: the digit-formatting artifact applies here exactly as in batch STT (a perfect hearing may echo `4739028` against a spelled-out reference), and the scored text is the model's *own transcript of its own speech* — the metric scores the whole loop (hear → speak → self-transcribe), which is what a caller experiences anyway.

## Real results

September 2026 — the golden set as live conversational turns against `gpt-realtime`:

```
PROVIDER             TURNS  ERRORS  V2V FIRST AUDIO AVG  V2V P95  RESPONSE DONE AVG  SPEECH OUT AVG
openai:gpt-realtime  14     0       601ms                1318ms   2905ms             12410ms
```

Two findings worth the run. First, **the speech-native model out-turns the pipeline it replaces**: the composed stack's floor is ~808ms *before TTS even begins* (213ms [streaming-STT finalization lag](streaming.md) + 595ms [warm LLM TTFT](llm.md), both our own published numbers) — while `gpt-realtime`'s **first audio lands at 601ms average**. Different runs, different modes, deliberately not one merged table — and the comparison still understates the pipeline's cost because [TTS time-to-first-audio](tts.md) isn't counted. Second, a behavioral finding latency tables usually hide: **the model talks a lot** — 12.4s average spoken reply to one-utterance turns, up to 29s, despite "respond briefly" instructions. At audio-token prices, reply length is a cost and UX axis, which is exactly why `speech out` is a first-class column.

Live echo results, September 2026:

```
PROVIDER             TURNS  ERRORS  ECHO WER  KEYTERM RECALL  V2V FIRST AUDIO AVG  V2V P95  RESPONSE DONE AVG  SPEECH OUT AVG
openai:gpt-realtime  14     0       27.0%     77.4%           638ms                1054ms   1699ms             5903ms
```

Reading that 27% honestly, the per-clip transcripts decompose it into three classes. First, the **predicted formatting artifact** ("4 7 3 9 0 2 8" echoed against a spelled-out reference — heard perfectly, scored as error). Second — the finding only this task could surface — **the model can't stop being an assistant**: told to echo "wait, before you do that, check whether the previous order shipped," it replied *"Sure thing. Let me check whether the previous order ever shipped"* — it did the thing instead of repeating it. Audio-mode instruction non-compliance is a real deployment risk, and it's invisible to every latency benchmark. Third, **where it complied, hearing was flawless**: 0.0% on the names clip, the insurance acronyms, and the technical jargon. The keyterm column carries the punchline: as a *listener*, `gpt-realtime` at **77.4% keyterm recall beats `gpt-4o-mini-transcribe`'s 74.2%** on the same golden set — while `nova-3` still leads at 93.5%. Treat the echo WER as an upper bound on mishearing, not a measurement of it — the loop includes formatting, compliance, and self-transcription, and that composite is what a caller experiences.

The `openai` S2S adapter worked on first live contact (14/14) in both phases — the mock-server tests carry both GA and beta event names, because the Realtime rename has bitten this codebase before.

## Barge-in: how fast does it shut up?

In a real call the user *will* talk over the agent, and what happens next decides whether the product feels conversational or maddening. `saybench s2s -barge-in` measures it: the reply's first audio arrives, a fixed 700ms passes, then a clean-room interrupt clip ("Wait, wait — hold on, stop for a second," `golden/barge/interrupt.wav`) feeds at real-time pace over the model's own speech. The metric is **barge-in stop time**: first interrupting audio byte sent → last output-audio delta received — how long the model kept talking after being interrupted.

```
$ saybench s2s -providers fake-s2s -barge-in

PROVIDER  TURNS  ERRORS  BARGE-IN STOP AVG  STOP P95  V2V FIRST AUDIO AVG  SPEECH OUT AVG
fake-s2s  14     0       360ms              490ms     503ms                1217ms
```

`-barge-in` implies `-turn-ending server_vad` (the model must be able to *detect* the interruption) and refuses to combine with `-score echo` — interrupting a repeat-back task measures neither thing well. Barge-in runs are recorded as their own condition on the report, and `compare` warns when a barge-in run meets a normal one: they are different experiments entirely.
