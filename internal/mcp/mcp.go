// Package mcp implements saybench's Model Context Protocol server: stdio,
// newline-delimited JSON-RPC 2.0, zero dependencies. It exists because
// benchmarks should be usable by coding agents, not just humans — an LLM
// editing a voice pipeline can run a bench, read which names it garbled,
// and gate its own change before shipping.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/renan-martini/saybench/internal/manifest"
	"github.com/renan-martini/saybench/internal/provider"
	"github.com/renan-martini/saybench/internal/report"
	"github.com/renan-martini/saybench/internal/runner"
	"github.com/renan-martini/saybench/internal/wer"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve runs the MCP loop until EOF on in.
func Serve(in io.Reader, out io.Writer, version string) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			enc.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32700, Message: "parse error: " + err.Error()}})
			continue
		}
		if req.ID == nil {
			continue // notification — never answered
		}
		resp := response{JSONRPC: "2.0", ID: req.ID}
		switch req.Method {
		case "initialize":
			resp.Result = map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "saybench", "version": version},
			}
		case "tools/list":
			resp.Result = map[string]any{"tools": toolDefs()}
		case "tools/call":
			resp.Result = callTool(req.Params, version)
		case "ping":
			resp.Result = map[string]any{}
		default:
			resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func toolDefs() []map[string]any {
	obj := func(props map[string]any, required ...string) map[string]any {
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	return []map[string]any{
		{
			"name":        "run_stt",
			"description": "Benchmark speech-to-text providers on a JSONL audio corpus: WER by failure-mode category, keyterm recall, latency. Providers: fake (offline), deepgram, openai (keys from env).",
			"inputSchema": obj(map[string]any{
				"providers": str("comma-separated providers, e.g. \"fake\" or \"deepgram,openai\""),
				"manifest":  str("path to the JSONL corpus manifest (audio+reference+category per line)"),
				"normalize": str("\"digits\" to canonicalize digit strings vs spelled digits; empty for literal scoring"),
			}, "providers", "manifest"),
		},
		{
			"name":        "run_llm",
			"description": "Benchmark LLM conversation-loop latency (streamed TTFT, completion, tok/s) over a JSONL prompt set. Targets: fake-llm (offline), gpt-4o-mini, openai-ws:<m>, groq:<m>, openrouter:<m>, custom:<m>.",
			"inputSchema": obj(map[string]any{
				"targets": str("comma-separated [provider:]model targets"),
				"prompts": str("path to the JSONL prompt manifest"),
			}, "targets", "prompts"),
		},
		{
			"name":        "compare_reports",
			"description": "Compare two saved saybench reports (same mode) and report per-provider deltas plus the worst WER regression. Refuses cross-mode comparison; warns on mixed conditions.",
			"inputSchema": obj(map[string]any{
				"old_path": str("baseline report JSON path"),
				"new_path": str("candidate report JSON path"),
			}, "old_path", "new_path"),
		},
		{
			"name":        "read_report",
			"description": "Read a saved saybench report: mode, conditions, and per-provider summaries (full per-item detail stays in the file).",
			"inputSchema": obj(map[string]any{
				"path": str("report JSON path"),
			}, "path"),
		},
	}
}

func callTool(params json.RawMessage, version string) map[string]any {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return errResult("bad tools/call params: " + err.Error())
	}
	text, err := runTool(p.Name, p.Arguments, version)
	if err != nil {
		return errResult(err.Error())
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func errResult(msg string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{{"type": "text", "text": msg}},
	}
}

func runTool(name string, args json.RawMessage, version string) (string, error) {
	switch name {
	case "run_stt":
		var a struct{ Providers, Manifest, Normalize string }
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		items, err := manifest.Load(a.Manifest)
		if err != nil {
			return "", err
		}
		refs := make(map[string]string, len(items))
		for _, it := range items {
			refs[it.Audio] = it.Reference
		}
		ps, err := provider.FromSpecs(a.Providers, refs)
		if err != nil {
			return "", err
		}
		opts := runner.Options{}
		if a.Normalize == "digits" {
			opts.Score = wer.Opts{DigitNormalize: true}
		}
		results := runner.Run(context.Background(), ps, items, opts)
		rep := report.Build(version, a.Manifest, results)
		rep.Normalization = a.Normalize
		return marshal(summaryView(rep))
	case "run_llm":
		var a struct{ Targets, Prompts string }
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		prompts, err := manifest.LoadPrompts(a.Prompts)
		if err != nil {
			return "", err
		}
		ts, err := provider.FromLLMSpecs(a.Targets)
		if err != nil {
			return "", err
		}
		results := runner.RunLLM(context.Background(), ts, prompts, runner.Options{})
		rep := report.BuildMode(version, a.Prompts, report.ModeLLM, results)
		return marshal(summaryView(rep))
	case "compare_reports":
		var a struct {
			OldPath string `json:"old_path"`
			NewPath string `json:"new_path"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		oldRep, err := report.LoadFile(a.OldPath)
		if err != nil {
			return "", err
		}
		newRep, err := report.LoadFile(a.NewPath)
		if err != nil {
			return "", err
		}
		if err := report.CheckComparable(oldRep, newRep); err != nil {
			return "", err
		}
		deltas := report.Compare(oldRep, newRep)
		worst, who := report.WorstRegression(deltas)
		return marshal(map[string]any{
			"deltas":              deltas,
			"worst_wer_change_pp": worst,
			"worst_provider":      who,
			"condition_note":      report.ConditionNote(oldRep, newRep),
		})
	case "read_report":
		var a struct{ Path string }
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		rep, err := report.LoadFile(a.Path)
		if err != nil {
			return "", err
		}
		return marshal(summaryView(rep))
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// summaryView is the agent-sized projection of a report: everything about
// conditions and aggregates, nothing per-item (that stays in the file).
func summaryView(r report.Report) map[string]any {
	mode := r.Mode
	if mode == "" {
		mode = report.ModeBatch
	}
	return map[string]any{
		"mode":            mode,
		"created_at":      r.CreatedAt,
		"manifest":        r.Manifest,
		"warmup":          r.Warmup,
		"s2s_scoring":     r.S2SScoring,
		"s2s_turn_ending": r.S2STurnEnding,
		"normalization":   r.Normalization,
		"summaries":       r.Summaries,
		"categories":      r.Categories,
	}
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
