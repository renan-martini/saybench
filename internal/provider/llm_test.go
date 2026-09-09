package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func chat(user string) ChatPrompt {
	return ChatPrompt{System: "You are a phone agent.", User: user, MaxTokens: 60}
}

func TestFakeLLMDeterministic(t *testing.T) {
	f := NewFakeLLM()
	r1, err := f.Complete(context.Background(), chat("hello there"))
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := f.Complete(context.Background(), chat("hello there"))
	if !reflect.DeepEqual(r1, r2) {
		t.Fatalf("fake-llm not deterministic: %+v vs %+v", r1, r2)
	}
	if r1.TTFTMS <= 0 || r1.CompletionMS <= r1.TTFTMS || r1.OutputTokens <= 0 || r1.Text == "" {
		t.Fatalf("shape wrong: %+v", r1)
	}
}

func TestFromLLMSpecs(t *testing.T) {
	ts, err := FromLLMSpecs("fake-llm")
	if err != nil || len(ts) != 1 || ts[0].Name() != "fake-llm" {
		t.Fatalf("FromLLMSpecs = %v, %v", ts, err)
	}
	if _, err := FromLLMSpecs("fake-llm,fake-llm"); err == nil {
		t.Fatal("expected duplicate error")
	}
	if _, err := FromLLMSpecs("unknownprov:model"); err == nil {
		t.Fatal("expected unknown provider error")
	}
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := FromLLMSpecs("gpt-4o-mini"); err == nil {
		t.Fatal("expected missing-key error for openai target")
	}
	t.Setenv("SAYBENCH_LLM_BASE_URL", "")
	t.Setenv("SAYBENCH_LLM_API_KEY", "k")
	if _, err := FromLLMSpecs("custom:m"); err == nil {
		t.Fatal("expected missing base url error for custom target")
	}
}

func TestOpenAICompatibleSSE(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		// role-only first chunk must NOT count as first token
		fmt.Fprint(w, `data: {"choices":[{"delta":{"role":"assistant"}}]}`+"\n\n")
		fl.Flush()
		time.Sleep(8 * time.Millisecond)
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Our hours"}}]}`+"\n\n")
		fl.Flush()
		time.Sleep(4 * time.Millisecond)
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":" are 9 to 5."}}]}`+"\n\n")
		fl.Flush()
		fmt.Fprint(w, `data: {"choices":[],"usage":{"completion_tokens":7}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	tgt := newOpenAICompatible("test", srv.URL, "sk-test", "some-model")
	res, err := tgt.Complete(context.Background(), chat("hours?"))
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if !strings.HasSuffix(gotPath, "/chat/completions") {
		t.Fatalf("path = %q", gotPath)
	}
	if res.Text != "Our hours are 9 to 5." {
		t.Fatalf("text = %q", res.Text)
	}
	if res.TTFTMS <= 0 || res.CompletionMS < res.TTFTMS {
		t.Fatalf("timings wrong: %+v", res)
	}
	if res.OutputTokens != 7 {
		t.Fatalf("tokens = %d, want 7 (from usage)", res.OutputTokens)
	}
}

func TestOpenAICompatibleSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"model not found"}}`, http.StatusNotFound)
	}))
	defer srv.Close()
	tgt := newOpenAICompatible("test", srv.URL, "sk-test", "nope")
	if _, err := tgt.Complete(context.Background(), chat("x")); err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("error body not surfaced: %v", err)
	}
}

func TestOpenAIResponsesWS(t *testing.T) {
	var conns int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&conns, 1)
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			var ev struct {
				Type            string `json:"type"`
				Model           string `json:"model"`
				MaxOutputTokens int    `json:"max_output_tokens"`
			}
			json.Unmarshal(msg, &ev)
			if ev.Type != "response.create" {
				continue
			}
			if ev.Model != "some-model" || ev.MaxOutputTokens <= 0 {
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.failed","response":{"error":{"message":"bad request shape"}}}`))
				continue
			}
			c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.in_progress"}`))
			time.Sleep(5 * time.Millisecond)
			c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.output_text.delta","delta":"Our hours"}`))
			c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.output_text.delta","delta":" are 9 to 5."}`))
			c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.completed","response":{"usage":{"output_tokens":7}}}`))
		}
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("SAYBENCH_OPENAI_WS_URL", wsURL(srv))
	ts, err := FromLLMSpecs("openai-ws:some-model")
	if err != nil {
		t.Fatal(err)
	}
	tgt := ts[0]
	if tgt.Name() != "openai-ws:some-model" {
		t.Fatalf("name = %q", tgt.Name())
	}
	r1, err := tgt.Complete(context.Background(), chat("hours?"))
	if err != nil {
		t.Fatal(err)
	}
	if r1.Text != "Our hours are 9 to 5." || r1.TTFTMS <= 0 || r1.CompletionMS < r1.TTFTMS || r1.OutputTokens != 7 {
		t.Fatalf("first result wrong: %+v", r1)
	}
	// Second prompt must ride the SAME connection — reuse is the semantics.
	if _, err := tgt.Complete(context.Background(), chat("more hours?")); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&conns); n != 1 {
		t.Fatalf("connections = %d, want 1 (persistent connection is the point of WS mode)", n)
	}
}

func TestOpenAIResponsesWSSurfacesFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		c.Read(ctx)
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.failed","response":{"error":{"message":"model not available"}}}`))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("SAYBENCH_OPENAI_WS_URL", wsURL(srv))
	ts, _ := FromLLMSpecs("openai-ws:m")
	if _, err := ts[0].Complete(context.Background(), chat("x")); err == nil || !strings.Contains(err.Error(), "model not available") {
		t.Fatalf("failure not surfaced: %v", err)
	}
}
