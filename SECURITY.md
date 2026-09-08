# Security

## Reporting

Found a vulnerability? Email **renan.martiniduarte@gmail.com** — please don't
open a public issue for anything sensitive. You'll get a reply within a week.

## Posture

- **API keys are read from environment variables only** and are never accepted
  via flags or config files, never written to reports or logs, and never echoed
  in error messages.
- **All network calls have deadlines** (per-item context timeout plus an HTTP
  client backstop); response bodies are read through `io.LimitReader`.
- **Input bounds:** audio files are capped at 100 MiB; manifest lines at 1 MiB.
- **Zero runtime dependencies** — the supply-chain surface is the Go standard
  library.
- Reports contain your reference transcripts and hypotheses. If your corpus is
  sensitive, treat report files accordingly — saybench never uploads anything
  anywhere except the audio you explicitly send to the provider you selected.
