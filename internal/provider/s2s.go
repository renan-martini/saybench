package provider

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"strings"

	"github.com/renan-martini/saybench/internal/wer"
)

// S2SResult is one speech-to-speech turn's outcome.
type S2SResult struct {
	// Transcript is the model's own transcript of its spoken reply —
	// phase-2 (echo-elicitation scoring) raw material, unscored today.
	Transcript string
	// V2VFirstAudioMS: end of the user's turn -> first output audio byte.
	// The turn-taking number.
	V2VFirstAudioMS int
	// ResponseDoneMS: end of the user's turn -> response fully finished.
	ResponseDoneMS int
	// OutputAudioMS: how long the agent speaks, from the audio bytes.
	OutputAudioMS int
}

// S2SProvider runs one conversational turn against a speech-to-speech
// model. Two methods — a new vendor is a one-file adapter.
type S2SProvider interface {
	Name() string
	Converse(ctx context.Context, audioPath string) (S2SResult, error)
}

// S2SFromSpecs builds providers from a comma-separated list: "fake-s2s,
// openai, custom". `custom` speaks the same OpenAI-Realtime dialect against
// SAYBENCH_S2S_URL (+ SAYBENCH_S2S_API_KEY, SAYBENCH_S2S_MODEL) — emerging
// S2S vendors clone that dialect the way everyone cloned chat completions;
// anything that doesn't is a small adapter behind S2SProvider.
// echoInstructions is the fixed phase-2 task. Fixed deliberately:
// comparability across runs and vendors depends on the task being identical.
const echoInstructions = "Repeat back exactly, word for word, what the caller just said. Say nothing else — no acknowledgment, no commentary."

// S2SOpts tunes s2s runs.
type S2SOpts struct {
	// Echo swaps to the fixed repeat-back task (phase 2).
	Echo bool
	// TurnEnding: "commit" (default — deterministic) or "server_vad" (the
	// production posture: the model detects end-of-speech itself; measured
	// V2V then INCLUDES VAD hangover time, which is the point).
	TurnEnding string
}

func S2SFromSpecs(specs string, refs map[string]string, echo bool) ([]S2SProvider, error) {
	return S2SFromSpecsOpts(specs, refs, S2SOpts{Echo: echo})
}

func S2SFromSpecsOpts(specs string, refs map[string]string, o S2SOpts) ([]S2SProvider, error) {
	echo := o.Echo
	var out []S2SProvider
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
		case "fake-s2s":
			f := NewFakeS2S(refs)
			f.Echo = echo
			out = append(out, f)
		case "openai":
			key, err := requireEnv("OPENAI_API_KEY")
			if err != nil {
				return nil, err
			}
			model := os.Getenv("SAYBENCH_OPENAI_S2S_MODEL")
			if model == "" {
				model = "gpt-realtime"
			}
			base := os.Getenv("SAYBENCH_OPENAI_S2S_URL")
			if base == "" {
				base = "wss://api.openai.com/v1/realtime"
			}
			p := newRealtimeS2S("openai:"+model, base, key, model)
			if echo {
				p.instructions = echoInstructions
			}
			p.serverVAD = o.TurnEnding == "server_vad"
			out = append(out, p)
		case "custom":
			base := os.Getenv("SAYBENCH_S2S_URL")
			if base == "" {
				return nil, fmt.Errorf("custom s2s target needs SAYBENCH_S2S_URL (an OpenAI-Realtime-dialect endpoint)")
			}
			model := os.Getenv("SAYBENCH_S2S_MODEL")
			p := newRealtimeS2S("custom:"+model, base, os.Getenv("SAYBENCH_S2S_API_KEY"), model)
			if echo {
				p.instructions = echoInstructions
			}
			p.serverVAD = o.TurnEnding == "server_vad"
			out = append(out, p)
		default:
			return nil, fmt.Errorf("unknown s2s provider %q (known: fake-s2s, openai, custom)", s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no providers selected")
	}
	return out, nil
}

// FakeS2S is the offline deterministic provider for CI and demos.
type FakeS2S struct {
	refs map[string]string
	// Echo makes the fake repeat the reference (with its deterministic
	// mangle) instead of replying conversationally.
	Echo bool
}

func NewFakeS2S(refs map[string]string) *FakeS2S { return &FakeS2S{refs: refs} }

func (f *FakeS2S) Name() string { return "fake-s2s" }

func (f *FakeS2S) Converse(_ context.Context, audioPath string) (S2SResult, error) {
	h := fnv.New32a()
	h.Write([]byte(audioPath))
	n := h.Sum32()
	ref := f.refs[audioPath]
	if ref == "" {
		ref = "that"
	}
	transcript := fmt.Sprintf("Sure — let me help with %s.", firstWords(ref, 4))
	if f.Echo {
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
		transcript = strings.Join(out, " ")
	}
	return S2SResult{
		Transcript:      transcript,
		V2VFirstAudioMS: int(300 + n%400),
		ResponseDoneMS:  int(1500 + n%1000),
		OutputAudioMS:   int(900 + n%800),
	}, nil
}

func firstWords(s string, n int) string {
	w := strings.Fields(s)
	if len(w) > n {
		w = w[:n]
	}
	return strings.Join(w, " ")
}
