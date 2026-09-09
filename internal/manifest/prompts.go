package manifest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Msg is one prior turn of dialog history in a prompt scenario.
type Msg struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

// Prompt is one LLM benchmark scenario: a voice-agent-shaped chat turn.
type Prompt struct {
	// Name identifies the scenario in reports; defaults to prompt-<line>.
	Name string `json:"name,omitempty"`
	// System is the system prompt (optional).
	System string `json:"system,omitempty"`
	// History is prior dialog, oldest first (optional).
	History []Msg `json:"history,omitempty"`
	// User is the user turn being answered. Required.
	User string `json:"user"`
	// Category groups scenarios (greeting, faq, extraction…).
	Category string `json:"category,omitempty"`
	// MaxTokens bounds the response; defaults to 120 — voice agents speak
	// in short turns, and latency benchmarks should not pay for essays.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// LoadPrompts reads a JSONL prompt manifest.
func LoadPrompts(path string) ([]Prompt, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Prompt
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "//") {
			continue
		}
		var p Prompt
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if strings.TrimSpace(p.User) == "" {
			return nil, fmt.Errorf("%s:%d: user is required", path, line)
		}
		for _, m := range p.History {
			switch m.Role {
			case "system", "user", "assistant":
			default:
				return nil, fmt.Errorf("%s:%d: history role %q (want system, user, or assistant)", path, line, m.Role)
			}
		}
		if p.Name == "" {
			// Ordinal, not file line: comments and blank lines must not
			// shift scenario identity between runs.
			p.Name = fmt.Sprintf("prompt-%d", len(out)+1)
		}
		if p.Category == "" {
			p.Category = "uncategorized"
		}
		if p.MaxTokens <= 0 {
			p.MaxTokens = 120
		}
		out = append(out, p)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: prompt manifest is empty", path)
	}
	return out, nil
}
