package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Deepgram calls Deepgram's prerecorded transcription API.
// Key: DEEPGRAM_API_KEY. Model: SAYBENCH_DEEPGRAM_MODEL (default "nova-3").
type Deepgram struct {
	key    string
	model  string
	client *http.Client
}

func NewDeepgram() (*Deepgram, error) {
	key, err := requireEnv("DEEPGRAM_API_KEY")
	if err != nil {
		return nil, err
	}
	model := os.Getenv("SAYBENCH_DEEPGRAM_MODEL")
	if model == "" {
		model = "nova-3"
	}
	return &Deepgram{key: key, model: model, client: newHTTPClient()}, nil
}

func (d *Deepgram) Name() string { return "deepgram:" + d.model }

func (d *Deepgram) Transcribe(ctx context.Context, audioPath string) (Result, error) {
	audio, err := readAudio(audioPath)
	if err != nil {
		return Result{}, err
	}
	u := "https://api.deepgram.com/v1/listen?" + url.Values{
		"model":        {d.model},
		"smart_format": {"false"},
		"punctuate":    {"false"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(audio))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Token "+d.key)
	req.Header.Set("Content-Type", contentTypeFor(audioPath))

	start := time.Now()
	resp, err := d.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	latency := time.Since(start)
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("deepgram: HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	var parsed struct {
		Results struct {
			Channels []struct {
				Alternatives []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Result{}, fmt.Errorf("deepgram: decode: %w", err)
	}
	if len(parsed.Results.Channels) == 0 || len(parsed.Results.Channels[0].Alternatives) == 0 {
		return Result{}, fmt.Errorf("deepgram: response contained no transcript")
	}
	return Result{Text: parsed.Results.Channels[0].Alternatives[0].Transcript, Latency: latency}, nil
}

// snippet bounds error-message payloads so a huge or sensitive body never
// lands in logs wholesale.
func snippet(b []byte) string {
	const n = 200
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}
