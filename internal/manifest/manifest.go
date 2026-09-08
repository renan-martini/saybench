// Package manifest loads the JSONL file that describes a benchmark corpus:
// one line per clip, each naming an audio file, its reference transcript,
// and a failure-mode category.
package manifest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Item is one benchmark clip.
type Item struct {
	// Audio is resolved to an absolute path at load time.
	Audio string `json:"audio"`
	// Reference is the ground-truth transcript.
	Reference string `json:"reference"`
	// Category groups clips by what they stress: names, numbers, acronyms…
	Category string `json:"category"`
}

// Load reads a JSONL manifest. Relative audio paths resolve against the
// manifest's own directory, so a corpus is a self-contained folder.
func Load(path string) ([]Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	base := filepath.Dir(path)
	var items []Item
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "//") {
			continue
		}
		var it Item
		if err := json.Unmarshal([]byte(raw), &it); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if it.Audio == "" || it.Reference == "" {
			return nil, fmt.Errorf("%s:%d: audio and reference are required", path, line)
		}
		if it.Category == "" {
			it.Category = "uncategorized"
		}
		if !filepath.IsAbs(it.Audio) {
			it.Audio = filepath.Join(base, it.Audio)
		}
		if _, err := os.Stat(it.Audio); err != nil {
			return nil, fmt.Errorf("%s:%d: audio file: %w", path, line, err)
		}
		items = append(items, it)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%s: manifest is empty", path)
	}
	return items, nil
}
