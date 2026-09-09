// Package pricing computes benchmark run costs from a USER-SUPPLIED rate
// table. Deliberately no built-in prices: vendor pricing drifts monthly and
// a benchmark that ships stale rates lies with authority. The repo carries
// pricing.example.json with illustrative values and links to verify.
package pricing

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/renan-martini/saybench/internal/report"
)

// Rates prices one provider. Set the fields that apply; unset means "don't
// charge on that axis".
type Rates struct {
	PerAudioMin       float64 `json:"per_audio_min,omitempty"`
	Per1MInputTokens  float64 `json:"per_1m_input_tokens,omitempty"`
	Per1MOutputTokens float64 `json:"per_1m_output_tokens,omitempty"`
	Per1MChars        float64 `json:"per_1m_chars,omitempty"`
}

// Table maps exact provider names (as they appear in reports) to rates.
type Table struct {
	Providers map[string]Rates `json:"providers"`
}

// Load reads a pricing table.
func Load(path string) (*Table, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Table
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(t.Providers) == 0 {
		return nil, fmt.Errorf("%s: pricing table has no providers", path)
	}
	return &t, nil
}

// CostFor prices one item. text is the synthesized input for tts items
// (chars axis); pass "" elsewhere. Unknown providers cost 0 — absent
// pricing must never become an invented number.
func (t *Table) CostFor(it report.ItemResult, text string) float64 {
	r, ok := t.Providers[it.Provider]
	if !ok {
		return 0
	}
	var c float64
	if r.PerAudioMin > 0 && it.AudioDurationMS > 0 {
		c += float64(it.AudioDurationMS) / 60000 * r.PerAudioMin
	}
	if r.Per1MInputTokens > 0 && it.InputTokens > 0 {
		c += float64(it.InputTokens) / 1e6 * r.Per1MInputTokens
	}
	if r.Per1MOutputTokens > 0 && it.OutputTokens > 0 {
		c += float64(it.OutputTokens) / 1e6 * r.Per1MOutputTokens
	}
	if r.Per1MChars > 0 && text != "" {
		c += float64(len(text)) / 1e6 * r.Per1MChars
	}
	return c
}
