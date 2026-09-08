package provider

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/renan-martini/saybench/internal/wer"
)

// Fake is a deterministic offline provider for CI and demos. It "transcribes"
// by taking the clip's reference text and introducing predictable errors —
// dropping every 8th word and substituting every 11th — so reports have
// stable, nonzero WER without any network or API key.
type Fake struct {
	refs map[string]string
}

// NewFake builds a Fake from a map of audio path -> reference transcript.
func NewFake(refs map[string]string) *Fake { return &Fake{refs: refs} }

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Transcribe(_ context.Context, audioPath string) (Result, error) {
	ref, ok := f.refs[audioPath]
	if !ok {
		return Result{}, fmt.Errorf("fake provider has no reference for %s", audioPath)
	}
	words := wer.Normalize(ref)
	var out []string
	for i, w := range words {
		switch {
		case (i+1)%8 == 0: // deletion
		case (i+1)%11 == 0: // substitution
			out = append(out, "um")
		default:
			out = append(out, w)
		}
	}
	// Deterministic pseudo-latency derived from the path, so tables and
	// compares are reproducible run to run.
	h := fnv.New32a()
	h.Write([]byte(audioPath))
	latency := time.Duration(20+h.Sum32()%60) * time.Millisecond
	return Result{Text: strings.Join(out, " "), Latency: latency}, nil
}
