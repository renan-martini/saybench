# CLAUDE.md — rules for AI-assisted work in this repo

This is a public repository and a professional work sample. Every commit is on
display. The rules in CONTRIBUTING.md are binding for AI sessions too; the ones
that matter most:

- **Stdlib only.** Do not add dependencies without an explicit human decision.
- **No secrets anywhere** — not in code, tests, fixtures, docs, or examples.
  Example keys in docs are `...`, never realistic-looking values.
- **Every HTTP call: context deadline + `io.LimitReader`.** No exceptions.
- **`gofmt -l .`, `go vet ./...`, `go test -race ./...` clean before any commit.**
- **README and docs/ examples must be real output**, pasted from an actual
  run — never hand-written or predicted.
- **Honest metrics framing** (batch vs streaming latency, synthetic vs real
  audio) is a product feature. Don't soften or blur it for marketing effect.
- Golden-set audio must be original: written for this repo and synthesized
  locally. Never copy audio or transcripts from other projects or datasets.
- Report schema changes bump `SchemaVersion` in `internal/report`.
