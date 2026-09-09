# Streaming STT Implementation Plan

Spec: docs/specs/2026-09-09-streaming-stt-design.md. TDD per task: write the
failing test, watch it fail, implement, watch it pass, commit.

## File map

- `internal/wav/wav.go` — PCM WAV parsing (rate/channels/bits/data/duration),
  chunking, 16k→24k linear resample.
- `internal/provider/stream.go` — `StreamingProvider` interface,
  `StreamResult`, paced feeder helper, `StreamFromSpecs`.
- `internal/provider/fake_stream.go` — deterministic offline provider.
- `internal/provider/deepgram_stream.go`, `openai_realtime.go`,
  `assemblyai_stream.go` — vendor adapters (each tested against a local WS
  server speaking the vendor protocol).
- `internal/report/report.go` — `Mode`, streaming item/summary fields,
  aggregation; compare mode-guard.
- `internal/runner/runner.go` — `RunStream`.
- `cmd/saybench/main.go` — `stream` subcommand, streaming summary table.
- `internal/dashboard/template.html` — mode label + TTFP/final-lag panel.
- CI: offline `fake-stream` smoke + mode-mismatch compare must fail.

## Tasks

1. wav package: tests for header parse (valid/truncated/non-PCM), duration
   math, chunk boundaries, resample length+endpoints → implement.
2. report: tests for Mode round-trip, streaming aggregation (avg/p95 TTFP,
   final lag), compare mode-guard error → implement.
3. provider: StreamResult/interface; fake-stream determinism tests →
   implement.
4. runner.RunStream test with fake-stream → implement.
5. Deepgram adapter: local WS server test (handshake auth header, binary
   chunks in, Results JSON out, CloseStream, timings) → implement.
6. OpenAI Realtime adapter: local WS server test (session events, base64
   append, delta/completed) → implement (24 kHz resample path).
7. AssemblyAI adapter: local WS server test (Turn messages, Terminate) →
   implement.
8. CLI stream command + streaming table; compare guard wiring; dashboard.
9. Docs (README streaming section + add-a-provider; CONTRIBUTING dependency
   justification; ROADMAP tick) + CI smoke. Version 0.4.0.
10. Full verification: gofmt, vet, race tests, offline e2e, dashboard
    screenshot review, push, CI green.
