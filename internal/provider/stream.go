package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/renan-martini/saybench/internal/wav"
)

// StreamResult is one streaming transcription outcome. All timings are
// measured against real-time audio pacing (see feeder).
type StreamResult struct {
	// Text is the concatenation of final segments.
	Text string
	// TTFPartialMS: first audio byte sent -> first non-empty interim.
	TTFPartialMS int
	// FinalLagMS: end of audio feed -> last final segment received.
	FinalLagMS int
	// Interims counts interim updates (a churn proxy).
	Interims int
	// InterimTexts are the interim snapshots, in arrival order — the raw
	// material for the word-survival stability score.
	InterimTexts []string
}

// StreamingProvider transcribes one audio file over a streaming connection,
// feeding audio at real-time pace.
type StreamingProvider interface {
	Name() string
	StreamTranscribe(ctx context.Context, audioPath string) (StreamResult, error)
}

// chunkInterval is the pacing quantum. 50ms is within every vendor's
// recommended chunk range and small enough that TTFP resolution is real.
const chunkInterval = 50 * time.Millisecond

// feed sends the file's PCM chunks to send() at real-time pace, honoring ctx.
// It returns the wall-clock moments of the first byte sent and the last byte
// sent, which anchor TTFP and finalization-lag measurement.
func feed(ctx context.Context, f *wav.File, send func([]byte) error) (start, end time.Time, err error) {
	chunks := f.Chunks(chunkInterval)
	ticker := time.NewTicker(chunkInterval)
	defer ticker.Stop()
	start = time.Now()
	for i, c := range chunks {
		if err := send(c); err != nil {
			return start, time.Now(), err
		}
		if i == len(chunks)-1 {
			break
		}
		select {
		case <-ctx.Done():
			return start, time.Now(), ctx.Err()
		case <-ticker.C:
		}
	}
	return start, time.Now(), nil
}

// StreamFromSpecs builds streaming providers from a comma-separated list:
// "fake-stream,deepgram,openai-realtime,assemblyai".
func StreamFromSpecs(specs string, refs map[string]string) ([]StreamingProvider, error) {
	var out []StreamingProvider
	seen := map[string]bool{}
	for _, s := range strings.Split(specs, ",") {
		s = strings.TrimSpace(strings.ToLower(s))
		if s != "" && seen[s] {
			return nil, fmt.Errorf("provider %q listed twice", s)
		}
		seen[s] = true
		switch s {
		case "":
			continue
		case "fake-stream":
			out = append(out, NewFakeStream(refs))
		case "deepgram":
			p, err := NewDeepgramStream()
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		case "openai-realtime":
			p, err := NewOpenAIRealtime()
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		case "assemblyai":
			p, err := NewAssemblyAIStream()
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		default:
			return nil, fmt.Errorf("unknown streaming provider %q (known: fake-stream, deepgram, openai-realtime, assemblyai)", s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no providers selected")
	}
	return out, nil
}
