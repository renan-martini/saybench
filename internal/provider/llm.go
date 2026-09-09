package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ChatPrompt is one conversation-loop scenario, provider-agnostic.
type ChatPrompt struct {
	System    string
	History   [][2]string // (role, content), oldest first
	User      string
	MaxTokens int
}

// LLMResult is one streamed completion's outcome.
type LLMResult struct {
	Text string
	// TTFTMS: request sent -> first content token. The number that gates
	// when TTS can start speaking.
	TTFTMS int
	// CompletionMS: request sent -> stream finished.
	CompletionMS int
	// InputTokens / OutputTokens from the usage frame; 0 if omitted.
	InputTokens  int
	OutputTokens int
}

// LLMTarget benchmarks one (endpoint, model) pair.
type LLMTarget interface {
	Name() string
	Complete(ctx context.Context, p ChatPrompt) (LLMResult, error)
}

// FromLLMSpecs parses "-targets": comma-separated [provider:]model entries.
// Providers: openai (default), openrouter, groq, custom, plus the offline
// fake-llm. One generic OpenAI-compatible SSE client serves them all.
func FromLLMSpecs(specs string) ([]LLMTarget, error) {
	bases := map[string]struct{ base, keyEnv string }{
		"openai":     {"https://api.openai.com/v1", "OPENAI_API_KEY"},
		"openrouter": {"https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"},
		"groq":       {"https://api.groq.com/openai/v1", "GROQ_API_KEY"},
	}
	var out []LLMTarget
	seen := map[string]bool{}
	for _, s := range strings.Split(specs, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if seen[s] {
			return nil, fmt.Errorf("target %q listed twice", s)
		}
		seen[s] = true
		if s == "fake-llm" {
			out = append(out, NewFakeLLM())
			continue
		}
		if strings.HasPrefix(s, "openai-ws:") {
			t, err := newOpenAIResponsesWS(strings.TrimPrefix(s, "openai-ws:"))
			if err != nil {
				return nil, err
			}
			out = append(out, t)
			continue
		}
		prov, model := "openai", s
		if i := strings.Index(s, ":"); i > 0 {
			if _, known := bases[s[:i]]; known || s[:i] == "custom" {
				prov, model = s[:i], s[i+1:]
			}
		}
		if model == "" {
			return nil, fmt.Errorf("target %q has no model", s)
		}
		var base, key string
		if prov == "custom" {
			base = strings.TrimRight(os.Getenv("SAYBENCH_LLM_BASE_URL"), "/")
			if base == "" {
				return nil, fmt.Errorf("custom target needs SAYBENCH_LLM_BASE_URL")
			}
			key = os.Getenv("SAYBENCH_LLM_API_KEY") // optional for local servers
		} else {
			b := bases[prov]
			k, err := requireEnv(b.keyEnv)
			if err != nil {
				return nil, err
			}
			base, key = b.base, k
		}
		out = append(out, newOpenAICompatible(prov+":"+model, base, key, model))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no targets selected")
	}
	return out, nil
}

// openAICompatible speaks the universal chat-completions SSE dialect.
type openAICompatible struct {
	name, base, key, model string
	client                 *http.Client
}

func newOpenAICompatible(name, base, key, model string) *openAICompatible {
	return &openAICompatible{name: name, base: strings.TrimRight(base, "/"), key: key, model: model, client: newHTTPClient()}
}

func (o *openAICompatible) Name() string { return o.name }

func (o *openAICompatible) Complete(ctx context.Context, p ChatPrompt) (LLMResult, error) {
	msgs := []map[string]string{}
	if p.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": p.System})
	}
	for _, h := range p.History {
		msgs = append(msgs, map[string]string{"role": h[0], "content": h[1]})
	}
	msgs = append(msgs, map[string]string{"role": "user", "content": p.User})

	body, err := json.Marshal(map[string]any{
		"model":          o.model,
		"messages":       msgs,
		"max_tokens":     p.MaxTokens,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	})
	if err != nil {
		return LLMResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return LLMResult{}, err
	}
	if o.key != "" {
		req.Header.Set("Authorization", "Bearer "+o.key)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	start := time.Now()
	resp, err := o.client.Do(req)
	if err != nil {
		return LLMResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return LLMResult{}, fmt.Errorf("%s: HTTP %d: %s", o.name, resp.StatusCode, snippet(b))
	}

	var res LLMResult
	var text strings.Builder
	var firstToken time.Time
	sc := bufio.NewScanner(io.LimitReader(resp.Body, maxResponseBytes))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var ev struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &ev) != nil {
			continue
		}
		if len(ev.Choices) > 0 && ev.Choices[0].Delta.Content != "" {
			if firstToken.IsZero() {
				firstToken = time.Now()
			}
			text.WriteString(ev.Choices[0].Delta.Content)
		}
		if ev.Usage != nil {
			res.InputTokens = ev.Usage.PromptTokens
			res.OutputTokens = ev.Usage.CompletionTokens
		}
	}
	if err := sc.Err(); err != nil {
		return LLMResult{}, fmt.Errorf("%s: stream: %w", o.name, err)
	}
	res.Text = text.String()
	res.CompletionMS = ceilMS(time.Since(start))
	if !firstToken.IsZero() {
		res.TTFTMS = ceilMS(firstToken.Sub(start))
	}
	if res.Text == "" {
		return LLMResult{}, fmt.Errorf("%s: stream ended with no content", o.name)
	}
	return res, nil
}

// FakeLLM is the offline deterministic target for CI and demos.
type FakeLLM struct{}

func NewFakeLLM() *FakeLLM { return &FakeLLM{} }

func (f *FakeLLM) Name() string { return "fake-llm" }

func (f *FakeLLM) Complete(_ context.Context, p ChatPrompt) (LLMResult, error) {
	h := fnv.New32a()
	h.Write([]byte(p.User))
	n := h.Sum32()
	words := len(strings.Fields(p.User))
	if strings.Contains(p.User, "Answer with the number only") {
		// Judge-rubric mode: answer deterministically so CI can exercise
		// the judging path offline.
		return LLMResult{
			Text:         fmt.Sprintf("%d", 60+n%41),
			TTFTMS:       int(50 + n%50),
			CompletionMS: int(120 + n%80),
			InputTokens:  words,
			OutputTokens: 1,
		}, nil
	}
	return LLMResult{
		Text:         fmt.Sprintf("Understood: %s", p.User),
		TTFTMS:       int(120 + n%180),
		CompletionMS: int(400 + n%300),
		InputTokens:  10 + words,
		OutputTokens: 2 + words,
	}, nil
}
