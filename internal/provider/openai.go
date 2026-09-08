package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// OpenAI calls the /v1/audio/transcriptions endpoint.
// Key: OPENAI_API_KEY. Model: SAYBENCH_OPENAI_MODEL (default
// "gpt-4o-mini-transcribe"; "whisper-1" also works).
type OpenAI struct {
	key    string
	model  string
	client *http.Client
}

func NewOpenAI() (*OpenAI, error) {
	key, err := requireEnv("OPENAI_API_KEY")
	if err != nil {
		return nil, err
	}
	model := os.Getenv("SAYBENCH_OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini-transcribe"
	}
	return &OpenAI{key: key, model: model, client: newHTTPClient()}, nil
}

func (o *OpenAI) Name() string { return "openai:" + o.model }

func (o *OpenAI) Transcribe(ctx context.Context, audioPath string) (Result, error) {
	audio, err := readAudio(audioPath)
	if err != nil {
		return Result{}, err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return Result{}, err
	}
	if _, err := fw.Write(audio); err != nil {
		return Result{}, err
	}
	if err := mw.WriteField("model", o.model); err != nil {
		return Result{}, err
	}
	if err := mw.Close(); err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/audio/transcriptions", &buf)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+o.key)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	start := time.Now()
	resp, err := o.client.Do(req)
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
		return Result{}, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Result{}, fmt.Errorf("openai: decode: %w", err)
	}
	return Result{Text: parsed.Text, Latency: latency}, nil
}
