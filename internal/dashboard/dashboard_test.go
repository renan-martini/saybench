package dashboard

import (
	"strings"
	"testing"

	"github.com/renan-martini/saybench/internal/report"
)

func TestRenderProducesSelfContainedPage(t *testing.T) {
	r := report.Build("test", "m.jsonl", []report.ItemResult{
		{Provider: "fake", Category: "names", WER: 0.2, Sub: 1, RefWords: 5, LatencyMS: 30,
			KeytermsTotal: 2, KeytermsHit: 1, MissedKeyterms: []string{"Beatriz"}},
	})
	var b strings.Builder
	if err := Render(&b, []report.Report{r, r}, "0.2.0"); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"<!doctype html>", "saybench dashboard", `"provider":"fake"`, "</html>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q", want)
		}
	}
	if strings.Contains(out, "__SAYBENCH_DATA__") {
		t.Fatal("data placeholder not replaced")
	}
	// The payload must be script-safe: encoding/json escapes angle brackets,
	// so a literal </script> can never appear inside the data blob.
	payload := out[strings.Index(out, `type="application/json">`)+len(`type="application/json">`):]
	payload = payload[:strings.Index(payload, "</script>")]
	if strings.Contains(payload, "</") {
		t.Fatal("unescaped closing tag inside JSON payload")
	}
}

func TestRenderRejectsEmpty(t *testing.T) {
	if err := Render(&strings.Builder{}, nil, "x"); err == nil {
		t.Fatal("expected error for zero reports")
	}
}
