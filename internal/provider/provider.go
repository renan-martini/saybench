// Package provider defines the speech-to-text provider interface and its
// implementations. Providers are deliberately thin: send audio, return text.
//
// Security posture (see SECURITY.md): API keys come only from environment
// variables and are never logged, persisted, or echoed in errors; every HTTP
// request carries a context deadline; response bodies are read with a size
// limit.
package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Result is one transcription outcome.
type Result struct {
	Text string
	// Latency is the full request round-trip as observed by the client.
	// Note: for HTTP batch APIs this includes upload time and is not the
	// same as streaming time-to-first-token. Streaming benchmarks are on
	// the roadmap.
	Latency time.Duration
}

// Provider transcribes a single audio file.
type Provider interface {
	Name() string
	Transcribe(ctx context.Context, audioPath string) (Result, error)
}

// maxResponseBytes bounds how much of any HTTP response we will read.
const maxResponseBytes = 4 << 20 // 4 MiB of JSON is already absurd

// maxAudioBytes bounds how large an input clip may be.
const maxAudioBytes = 100 << 20 // 100 MiB

func newHTTPClient() *http.Client {
	return &http.Client{
		// Per-request deadlines come from the context; this is the
		// backstop so a missing deadline can never hang forever.
		Timeout: 5 * time.Minute,
	}
}

func requireEnv(key string) (string, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return "", fmt.Errorf("%s is not set (saybench reads API keys from the environment only)", key)
	}
	return v, nil
}

func readAudio(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxAudioBytes {
		return nil, fmt.Errorf("%s: %d bytes exceeds the %d byte limit", path, info.Size(), maxAudioBytes)
	}
	return os.ReadFile(path)
}

func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".ogg":
		return "audio/ogg"
	case ".m4a", ".mp4":
		return "audio/mp4"
	default:
		return "application/octet-stream"
	}
}

// FromSpecs builds providers from a comma-separated spec list, e.g.
// "fake,deepgram,openai". refs feeds the fake provider (audio path ->
// reference text) so it can produce deterministic near-miss transcripts.
func FromSpecs(specs string, refs map[string]string) ([]Provider, error) {
	var out []Provider
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
		case "fake":
			out = append(out, NewFake(refs))
		case "deepgram":
			p, err := NewDeepgram()
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		case "openai":
			p, err := NewOpenAI()
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		default:
			return nil, fmt.Errorf("unknown provider %q (known: fake, deepgram, openai)", s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no providers selected")
	}
	return out, nil
}
