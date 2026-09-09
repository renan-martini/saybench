package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/coder/websocket"

	"github.com/renan-martini/saybench/internal/wav"
)

// DeepgramStream benchmarks Deepgram's live WebSocket API.
// Key: DEEPGRAM_API_KEY. Model: SAYBENCH_DEEPGRAM_MODEL (default nova-3).
// Endpoint override: SAYBENCH_DEEPGRAM_STREAM_URL (self-hosted Deepgram).
type DeepgramStream struct {
	key, model, base string
}

func NewDeepgramStream() (*DeepgramStream, error) {
	key, err := requireEnv("DEEPGRAM_API_KEY")
	if err != nil {
		return nil, err
	}
	model := os.Getenv("SAYBENCH_DEEPGRAM_MODEL")
	if model == "" {
		model = "nova-3"
	}
	base := os.Getenv("SAYBENCH_DEEPGRAM_STREAM_URL")
	if base == "" {
		base = "wss://api.deepgram.com/v1/listen"
	}
	return &DeepgramStream{key: key, model: model, base: base}, nil
}

func (d *DeepgramStream) Name() string { return "deepgram-stream:" + d.model }

func (d *DeepgramStream) StreamTranscribe(ctx context.Context, audioPath string) (StreamResult, error) {
	f, err := wav.Parse(audioPath)
	if err != nil {
		return StreamResult{}, err
	}
	u := d.base + "?" + url.Values{
		"model":           {d.model},
		"encoding":        {"linear16"},
		"sample_rate":     {strconv.Itoa(f.SampleRate)},
		"channels":        {strconv.Itoa(f.Channels)},
		"interim_results": {"true"},
		"punctuate":       {"false"},
		"smart_format":    {"false"},
	}.Encode()
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Token " + d.key}},
	})
	if err != nil {
		return StreamResult{}, fmt.Errorf("deepgram-stream: dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxResponseBytes)

	col := &collector{}
	readDone := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				readDone <- err
				return
			}
			var ev struct {
				Type    string `json:"type"`
				IsFinal bool   `json:"is_final"`
				Channel struct {
					Alternatives []struct {
						Transcript string `json:"transcript"`
					} `json:"alternatives"`
				} `json:"channel"`
			}
			if json.Unmarshal(msg, &ev) != nil || ev.Type != "Results" || len(ev.Channel.Alternatives) == 0 {
				continue
			}
			text := ev.Channel.Alternatives[0].Transcript
			if ev.IsFinal {
				col.final(text)
			} else {
				col.interim(text)
			}
		}
	}()

	start, end, err := feed(ctx, f, func(chunk []byte) error {
		return conn.Write(ctx, websocket.MessageBinary, chunk)
	})
	col.begin(start)
	col.endFeed(end)
	if err != nil {
		return StreamResult{}, fmt.Errorf("deepgram-stream: send: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"CloseStream"}`)); err != nil {
		return StreamResult{}, fmt.Errorf("deepgram-stream: close: %w", err)
	}
	// Read until the server finishes flushing finals and closes.
	if err := <-readDone; err != nil && websocket.CloseStatus(err) == -1 && ctx.Err() != nil {
		return StreamResult{}, ctx.Err()
	}
	return col.result(), nil
}
