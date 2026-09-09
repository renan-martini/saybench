package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
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
