// Package dashboard renders benchmark reports into a single self-contained
// HTML file: no server, no CDN, no build step — open it, or commit it as a CI
// artifact. Charts are inline SVG drawn by ~vanilla JS embedded in the
// template; the palette is a colorblind-validated categorical set with
// light and dark modes.
package dashboard

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/renan-martini/saybench/internal/report"
)

//go:embed template.html
var template string

type payload struct {
	Version     string          `json:"version"`
	GeneratedAt time.Time       `json:"generated_at"`
	Runs        []report.Report `json:"runs"`
}

// Render writes the dashboard for the given runs (oldest first).
func Render(w io.Writer, runs []report.Report, version string) error {
	if len(runs) == 0 {
		return fmt.Errorf("no reports to render")
	}
	data, err := json.Marshal(payload{Version: version, GeneratedAt: time.Now().UTC(), Runs: runs})
	if err != nil {
		return err
	}
	// encoding/json escapes < > & inside strings, so the blob is safe to
	// inline in a <script> element.
	out := strings.Replace(template, "__SAYBENCH_DATA__", string(data), 1)
	out = strings.Replace(out, "__SAYBENCH_VERSION__", version, 1)
	_, err = io.WriteString(w, out)
	return err
}
