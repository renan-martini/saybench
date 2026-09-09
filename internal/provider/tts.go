package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// TTSResult is one synthesis outcome. All audio is requested as 24kHz mono
// pcm16 so duration is computable from bytes.
type TTSResult struct {
	// TTFAudioMS: request sent -> first audio byte. What the caller hears
	// as response latency once the LLM has spoken its first token.
	TTFAudioMS int
	// TotalMS: request sent -> synthesis stream finished.
	TotalMS int
	// AudioMS: duration of the synthesized speech.
	AudioMS int
}

// TTSProvider synthesizes one utterance.
type TTSProvider interface {
	Name() string
	Speak(ctx context.Context, text string) (TTSResult, error)
}

const ttsRate = 24000

// TTSFromSpecs builds providers from "fake-tts,openai,elevenlabs,custom".
// custom is an OpenAI-compatible /audio/speech endpoint via
// SAYBENCH_TTS_BASE_URL (+ optional SAYBENCH_TTS_API_KEY, SAYBENCH_TTS_MODEL,
// SAYBENCH_TTS_VOICE).
func TTSFromSpecs(specs string) ([]TTSProvider, error) {
	var out []TTSProvider
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
		case "fake-tts":
			out = append(out, NewFakeTTS())
		case "openai":
			key, err := requireEnv("OPENAI_API_KEY")
			if err != nil {
				return nil, err
			}
			model := envOr("SAYBENCH_OPENAI_TTS_MODEL", "gpt-4o-mini-tts")
			voice := envOr("SAYBENCH_OPENAI_TTS_VOICE", "alloy")
			out = append(out, newOpenAITTS("openai:"+model, "https://api.openai.com/v1", key, model, voice))
		case "elevenlabs":
			key, err := requireEnv("ELEVENLABS_API_KEY")
			if err != nil {
				return nil, err
			}
			voice := envOr("SAYBENCH_ELEVENLABS_VOICE", "21m00Tcm4TlvDq8ikWAM")
			model := envOr("SAYBENCH_ELEVENLABS_MODEL", "eleven_turbo_v2_5")
			out = append(out, newElevenLabsTTS("https://api.elevenlabs.io", key, voice, model))
		case "custom":
			base := strings.TrimRight(os.Getenv("SAYBENCH_TTS_BASE_URL"), "/")
			if base == "" {
				return nil, fmt.Errorf("custom tts target needs SAYBENCH_TTS_BASE_URL (an OpenAI-compatible /audio/speech endpoint)")
			}
			model := os.Getenv("SAYBENCH_TTS_MODEL")
			voice := envOr("SAYBENCH_TTS_VOICE", "alloy")
			out = append(out, newOpenAITTS("custom:"+model, base, os.Getenv("SAYBENCH_TTS_API_KEY"), model, voice))
		default:
			return nil, fmt.Errorf("unknown tts provider %q (known: fake-tts, openai, elevenlabs, custom)", s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no providers selected")
	}
	return out, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// streamTiming reads a streaming audio body, timing first byte and total,
// and counting bytes for duration.
func streamTiming(start time.Time, body io.Reader) (TTSResult, error) {
	var res TTSResult
	buf := make([]byte, 32*1024)
	var total int
	var first time.Time
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if first.IsZero() {
				first = time.Now()
			}
			total += n
			if total > maxAudioBytes {
				return res, fmt.Errorf("synthesis exceeded %d bytes", maxAudioBytes)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, err
		}
	}
	if first.IsZero() {
		return res, fmt.Errorf("stream ended with no audio")
	}
	res.TTFAudioMS = ceilMS(first.Sub(start))
	res.TotalMS = ceilMS(time.Since(start))
	res.AudioMS = total / 2 * 1000 / ttsRate
	return res, nil
}

// openAITTS speaks the OpenAI /audio/speech dialect (and compatible servers).
type openAITTS struct {
	name, base, key, model, voice string
	client                        *http.Client
}

func newOpenAITTS(name, base, key, model, voice string) *openAITTS {
	return &openAITTS{name: name, base: strings.TrimRight(base, "/"), key: key, model: model, voice: voice, client: newHTTPClient()}
}

func (o *openAITTS) Name() string { return o.name }

func (o *openAITTS) Speak(ctx context.Context, text string) (TTSResult, error) {
	body, err := json.Marshal(map[string]any{
		"model":           o.model,
		"voice":           o.voice,
		"input":           text,
		"response_format": "pcm", // 24kHz mono pcm16 — duration computable
	})
	if err != nil {
		return TTSResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return TTSResult{}, err
	}
	if o.key != "" {
		req.Header.Set("Authorization", "Bearer "+o.key)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := o.client.Do(req)
	if err != nil {
		return TTSResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return TTSResult{}, fmt.Errorf("%s: HTTP %d: %s", o.name, resp.StatusCode, snippet(b))
	}
	res, err := streamTiming(start, resp.Body)
	if err != nil {
		return TTSResult{}, fmt.Errorf("%s: %w", o.name, err)
	}
	return res, nil
}

// elevenLabsTTS speaks the ElevenLabs streaming dialect.
type elevenLabsTTS struct {
	base, key, voice, model string
	client                  *http.Client
}

func newElevenLabsTTS(base, key, voice, model string) *elevenLabsTTS {
	return &elevenLabsTTS{base: strings.TrimRight(base, "/"), key: key, voice: voice, model: model, client: newHTTPClient()}
}

func (e *elevenLabsTTS) Name() string { return "elevenlabs:" + e.model }

func (e *elevenLabsTTS) Speak(ctx context.Context, text string) (TTSResult, error) {
	body, err := json.Marshal(map[string]any{"text": text, "model_id": e.model})
	if err != nil {
		return TTSResult{}, err
	}
	u := e.base + "/v1/text-to-speech/" + url.PathEscape(e.voice) + "/stream?" + url.Values{
		"output_format": {"pcm_24000"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return TTSResult{}, err
	}
	req.Header.Set("xi-api-key", e.key)
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		return TTSResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return TTSResult{}, fmt.Errorf("%s: HTTP %d: %s", e.Name(), resp.StatusCode, snippet(b))
	}
	res, err := streamTiming(start, resp.Body)
	if err != nil {
		return TTSResult{}, fmt.Errorf("%s: %w", e.Name(), err)
	}
	return res, nil
}

// FakeTTS is the offline deterministic provider for CI and demos.
type FakeTTS struct{}

func NewFakeTTS() *FakeTTS { return &FakeTTS{} }

func (f *FakeTTS) Name() string { return "fake-tts" }

func (f *FakeTTS) Speak(_ context.Context, text string) (TTSResult, error) {
	h := fnv.New32a()
	h.Write([]byte(text))
	n := h.Sum32()
	words := len(strings.Fields(text))
	return TTSResult{
		TTFAudioMS: int(90 + n%120),
		TotalMS:    int(400 + n%300),
		AudioMS:    300 * max(words, 1),
	}, nil
}
