# Contributing

Thanks for the interest — small, sharp PRs are very welcome. The bar:

## Hard rules

1. **Standard library only, with documented exceptions.** A new dependency
   needs a written justification for why the stdlib can't do it. "It's
   convenient" is not one. Current exceptions: `github.com/coder/websocket` —
   the stdlib has no WebSocket client, a hand-rolled RFC 6455 implementation
   would be ~300 lines of security-sensitive framing code maintained forever,
   and the library also provides the server side our vendor-protocol tests
   are written against.
2. **No secrets, ever.** API keys come from environment variables only. Nothing
   secret in code, tests, fixtures, or git history. CI runs with no keys at all.
3. **Every HTTP request carries a context deadline.** Every response body read
   is bounded with `io.LimitReader`.
4. **Tests are required**, table-driven where it fits. `go test -race ./...`,
   `go vet ./...`, and `gofmt -l .` must all be clean — CI enforces it.
5. **Determinism is a feature.** The fake provider, result ordering, and report
   schema stay stable; breaking the report schema bumps `SchemaVersion`.
6. **Honest metrics only.** Anything that could mislead (batch latency vs
   streaming, synthetic vs real audio) gets stated in the docs, not buried —
   the doctrine lives in [docs/scoring.md](docs/scoring.md).

## Adding a provider

One file in `internal/provider/` implementing `Name()` and
`Transcribe(ctx, audioPath)`, wired into `FromSpecs`. Follow `deepgram.go` as
the template: env-only key via `requireEnv`, `newHTTPClient()`, bounded reads,
error snippets capped by `snippet()`. Include the env var names in the
providers table in [docs/providers.md](docs/providers.md).

## Style

- `gofmt` is the formatter; comments explain *why*, not *what*.
- Errors say what failed and in which file/provider; never include key material.
- Commit messages: `type(scope): summary` (e.g. `feat(provider): add azure`).
