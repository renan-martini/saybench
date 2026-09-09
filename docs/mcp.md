[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# For coding agents: `saybench mcp`

The whole tool is available as a **Model Context Protocol server** — stdio, zero configuration:

```json
{ "mcpServers": { "saybench": { "command": "saybench", "args": ["mcp"] } } }
```

Tools:

- `run_stt` — run a batch STT bench (providers, manifest) and get the structured result.
- `run_llm` — run an LLM TTFT bench against any configured target.
- `compare_reports` — deltas between two saved reports, the worst regression, and any condition warning.
- `read_report` — the agent-sized summary projection of any saved report.

This is the point of the whole `-format json` design ([Reports & CI](reports.md)): an LLM editing your voice pipeline can bench it, read which names it garbled, and gate its own change before shipping.
