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

// AssemblyAIStream benchmarks AssemblyAI Universal-Streaming (v3).
// Key: ASSEMBLYAI_API_KEY. Endpoint override: SAYBENCH_ASSEMBLYAI_STREAM_URL.
type AssemblyAIStream struct {
	key, base string
}

func NewAssemblyAIStream() (*AssemblyAIStream, error) {
	key, err := requireEnv("ASSEMBLYAI_API_KEY")
	if err != nil {
		return nil, err
	}
	base := os.Getenv("SAYBENCH_ASSEMBLYAI_STREAM_URL")
	if base == "" {
		base = "wss://streaming.assemblyai.com/v3/ws"
	}
	return &AssemblyAIStream{key: key, base: base}, nil
}

func (a *AssemblyAIStream) Name() string { return "assemblyai-stream" }

func (a *AssemblyAIStream) StreamTranscribe(ctx context.Context, audioPath string) (StreamResult, error) {
	f, err := wav.Parse(audioPath)
	if err != nil {
		return StreamResult{}, err
	}
	u := a.base + "?" + url.Values{
		"sample_rate":  {strconv.Itoa(f.SampleRate)},
		"format_turns": {"false"},
	}.Encode()
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {a.key}},
	})
	if err != nil {
		return StreamResult{}, fmt.Errorf("assemblyai-stream: dial: %w", err)
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
				Type       string `json:"type"`
				Transcript string `json:"transcript"`
				EndOfTurn  bool   `json:"end_of_turn"`
			}
			if json.Unmarshal(msg, &ev) != nil || ev.Type != "Turn" {
				continue
			}
			if ev.EndOfTurn {
				col.final(ev.Transcript)
			} else {
				col.interim(ev.Transcript)
			}
		}
	}()

	start, end, err := feed(ctx, f, func(chunk []byte) error {
		return conn.Write(ctx, websocket.MessageBinary, chunk)
	})
	col.begin(start)
	col.endFeed(end)
	if err != nil {
		return StreamResult{}, fmt.Errorf("assemblyai-stream: send: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Terminate"}`)); err != nil {
		return StreamResult{}, fmt.Errorf("assemblyai-stream: terminate: %w", err)
	}
	if err := <-readDone; err != nil && websocket.CloseStatus(err) == -1 && ctx.Err() != nil {
		return StreamResult{}, ctx.Err()
	}
	return col.result(), nil
}
