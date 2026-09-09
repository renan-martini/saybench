package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// openAIResponsesWS benchmarks OpenAI's Responses API WebSocket mode: the
// same models as SSE chat, over ONE persistent connection per target.
// Prompts are serialized over that connection — sequential turns on a warm
// socket is exactly the voice-loop shape, and connection reuse is the thing
// WS mode exists to provide, so a fresh-socket-per-prompt bench would erase
// what it measures.
//
// Endpoint override: SAYBENCH_OPENAI_WS_URL (default
// wss://api.openai.com/v1/responses). The adapter is protocol-tested against
// a local mock speaking the documented event dialect.
type openAIResponsesWS struct {
	name, base, key, model string

	mu     sync.Mutex // serializes Completes and guards conn
	conn   *websocket.Conn
	nextID int
}

func newOpenAIResponsesWS(model string) (*openAIResponsesWS, error) {
	key, err := requireEnv("OPENAI_API_KEY")
	if err != nil {
		return nil, err
	}
	base := os.Getenv("SAYBENCH_OPENAI_WS_URL")
	if base == "" {
		base = "wss://api.openai.com/v1/responses"
	}
	return &openAIResponsesWS{name: "openai-ws:" + model, base: base, key: key, model: model}, nil
}

func (o *openAIResponsesWS) Name() string { return o.name }

func (o *openAIResponsesWS) dial(ctx context.Context) error {
	if o.conn != nil {
		return nil
	}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dctx, o.base, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + o.key}},
	})
	if err != nil {
		return fmt.Errorf("%s: dial: %w", o.name, err)
	}
	conn.SetReadLimit(maxResponseBytes)
	o.conn = conn
	return nil
}

func (o *openAIResponsesWS) Complete(ctx context.Context, p ChatPrompt) (LLMResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	res, retriable, err := o.completeOnce(ctx, p)
	if err != nil && retriable && ctx.Err() == nil {
		// The server closed the persistent connection cleanly before any
		// output (idle reap between prompts, observed live). A production
		// client redials and resends; so do we — exactly once, and the
		// timer restarts so the measurement covers the attempt that ran.
		res, _, err = o.completeOnce(ctx, p)
	}
	return res, err
}

// completeOnce runs one request. retriable reports a clean connection close
// before any output arrived — the only case where a silent resend is safe.
func (o *openAIResponsesWS) completeOnce(ctx context.Context, p ChatPrompt) (LLMResult, bool, error) {
	if err := o.dial(ctx); err != nil {
		return LLMResult{}, false, err
	}
	o.nextID++
	streamID := "sb-" + strconv.Itoa(o.nextID)

	type item struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	mkItem := func(role, text string) item {
		it := item{Type: "message", Role: role}
		it.Content = append(it.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{"input_text", text})
		return it
	}
	input := []item{}
	if p.System != "" {
		input = append(input, mkItem("system", p.System))
	}
	for _, h := range p.History {
		input = append(input, mkItem(h[0], h[1]))
	}
	input = append(input, mkItem("user", p.User))

	req, err := json.Marshal(map[string]any{
		"type":              "response.create",
		"stream_id":         streamID,
		"model":             o.model,
		"store":             false,
		"max_output_tokens": p.MaxTokens,
		"input":             input,
	})
	if err != nil {
		return LLMResult{}, false, err
	}

	start := time.Now()
	if err := o.conn.Write(ctx, websocket.MessageText, req); err != nil {
		o.reset()
		return LLMResult{}, true, fmt.Errorf("%s: send: %w", o.name, err)
	}

	var res LLMResult
	var text []byte
	var firstToken time.Time
	for {
		_, msg, err := o.conn.Read(ctx)
		if err != nil {
			o.reset()
			retriable := firstToken.IsZero() && websocket.CloseStatus(err) == websocket.StatusNormalClosure
			return LLMResult{}, retriable, fmt.Errorf("%s: read: %w", o.name, err)
		}
		var ev struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal(msg, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				if firstToken.IsZero() {
					firstToken = time.Now()
				}
				text = append(text, ev.Delta...)
			}
		case "response.completed":
			res.Text = string(text)
			res.CompletionMS = ceilMS(time.Since(start))
			if !firstToken.IsZero() {
				res.TTFTMS = ceilMS(firstToken.Sub(start))
			}
			res.OutputTokens = ev.Response.Usage.OutputTokens
			if res.Text == "" {
				return LLMResult{}, false, fmt.Errorf("%s: response completed with no content", o.name)
			}
			return res, false, nil
		case "response.failed", "response.incomplete":
			msg := ev.Response.Error.Message
			if msg == "" {
				msg = ev.Type
			}
			return LLMResult{}, false, fmt.Errorf("%s: %s", o.name, msg)
		}
	}
}

// reset drops a broken connection so the next Complete redials.
func (o *openAIResponsesWS) reset() {
	if o.conn != nil {
		o.conn.CloseNow()
		o.conn = nil
	}
}
