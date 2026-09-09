package provider

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/renan-martini/saybench/internal/wer"
)

// FakeStream is the streaming twin of Fake: deterministic, offline, no keys.
// It produces the same mangled transcript plus synthetic-but-stable stream
// timings so CI can exercise the whole streaming path without a network.
type FakeStream struct {
	refs map[string]string
}

func NewFakeStream(refs map[string]string) *FakeStream { return &FakeStream{refs: refs} }

func (f *FakeStream) Name() string { return "fake-stream" }

func (f *FakeStream) StreamTranscribe(_ context.Context, audioPath string) (StreamResult, error) {
	ref, ok := f.refs[audioPath]
	if !ok {
		return StreamResult{}, fmt.Errorf("fake-stream has no reference for %s", audioPath)
	}
	words := wer.Normalize(ref)
	var out []string
	for i, w := range words {
		switch {
		case (i+1)%8 == 0:
		case (i+1)%11 == 0:
			out = append(out, "um")
		default:
			out = append(out, w)
		}
	}
	final := strings.Join(out, " ")
	// Two cumulative interim snapshots (first third, first two-thirds of the
	// final) — deterministic, and deliberately missing the tail words so the
	// survival score is meaningfully below 100%.
	interims := []string{
		strings.Join(out[:len(out)/3], " "),
		strings.Join(out[:len(out)*2/3], " "),
	}
	h := fnv.New32a()
	h.Write([]byte(audioPath))
	n := h.Sum32()
	return StreamResult{
		Text:         final,
		TTFPartialMS: int(150 + n%200),
		FinalLagMS:   int(80 + n%150),
		Interims:     len(interims),
		InterimTexts: interims,
	}, nil
}
