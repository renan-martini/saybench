package manifest

import (
	"testing"
)

func TestLoadPrompts(t *testing.T) {
	dir := t.TempDir()
	mf := write(t, dir, "golden.jsonl",
		`{"name":"greet","system":"You are a phone agent.","user":"Hi, who am I speaking with?","category":"greeting","max_tokens":60}
// comment

{"user":"What are your opening hours?","history":[{"role":"assistant","content":"Hello!"},{"role":"user","content":"Hi"}]}
`)
	ps, err := LoadPrompts(mf)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("got %d prompts, want 2", len(ps))
	}
	if ps[0].Name != "greet" || ps[0].MaxTokens != 60 || ps[0].Category != "greeting" {
		t.Fatalf("first prompt wrong: %+v", ps[0])
	}
	if ps[1].Name != "prompt-2" {
		t.Fatalf("missing name not defaulted: %q", ps[1].Name)
	}
	if ps[1].Category != "uncategorized" || ps[1].MaxTokens != 120 {
		t.Fatalf("defaults wrong: %+v", ps[1])
	}
	if len(ps[1].History) != 2 || ps[1].History[0].Role != "assistant" {
		t.Fatalf("history wrong: %+v", ps[1].History)
	}
}

func TestLoadPromptsRejectsBad(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"nouser.jsonl":  `{"system":"x"}`,
		"badrole.jsonl": `{"user":"x","history":[{"role":"alien","content":"y"}]}`,
		"empty.jsonl":   "\n",
	} {
		mf := write(t, dir, name, content)
		if _, err := LoadPrompts(mf); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}
