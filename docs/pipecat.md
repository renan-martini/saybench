[← saybench](../README.md) · [Batch STT](stt.md) · [Streaming](streaming.md) · [LLM](llm.md) · [S2S](s2s.md) · [TTS](tts.md) · [Scoring](scoring.md) · [Providers](providers.md) · [Reports & CI](reports.md) · [MCP](mcp.md) · [Pipecat](pipecat.md)

# Benchmarking a Pipecat stack

Pipecat pipelines configure services in Python, so there is no config file to parse — instead, map each service to its saybench equivalent and bench exactly what your pipeline runs:

| Pipecat service | saybench |
|---|---|
| `DeepgramSTTService` | `saybench stream -providers deepgram` |
| `OpenAISTTService` | `saybench stt -providers openai` |
| `AssemblyAISTTService` | `saybench stream -providers assemblyai` |
| `OpenAILLMService` | `saybench llm -targets gpt-4o-mini` (or your model) |
| any OpenAI-compatible LLM (OpenRouter, Groq, vLLM…) | `saybench llm -targets openrouter:<m>` / `groq:<m>` / `custom:<m>` |
| `OpenAIRealtimeBetaLLMService` (speech-to-speech) | `saybench s2s -providers openai` |
| `GeminiMultimodalLiveLLMService` (speech-to-speech) | `saybench s2s -providers gemini -turn-ending server_vad` |
| `ElevenLabsTTSService` | `saybench tts -providers elevenlabs` |
| `OpenAITTSService` | `saybench tts -providers openai` |
| `CartesiaTTSService` and others | one-file adapter, PRs welcome — see [CONTRIBUTING.md](../CONTRIBUTING.md) |

Use your own recorded call audio as the corpus (a manifest is one JSONL line per clip — see [Batch STT](stt.md)), wire `compare -max-wer-regression` into CI ([Reports & CI](reports.md)), and every pipeline change gets a latency-and-accuracy diff before it ships.
