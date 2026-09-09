package provider

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/coder/websocket"

	"github.com/renan-martini/saybench/internal/wav"
)

// OpenAIRealtime benchmarks OpenAI's Realtime API in transcription mode.
// Key: OPENAI_API_KEY. Model: SAYBENCH_OPENAI_REALTIME_MODEL (default
// gpt-4o-mini-transcribe). Endpoint override: SAYBENCH_OPENAI_REALTIME_URL.
//
// The Realtime API expects 24 kHz mono pcm16; other input rates are
// linearly resampled first (documented in the spec — resampling quality is
// not part of what saybench measures).
type OpenAIRealtime struct {
	key, model, base string
}

const openaiRealtimeRate = 24000

func NewOpenAIRealtime() (*OpenAIRealtime, error) {
	key, err := requireEnv("OPENAI_API_KEY")
	if err != nil {
		return nil, err
	}
	model := os.Getenv("SAYBENCH_OPENAI_REALTIME_MODEL")
	if model == "" {
		model = "gpt-4o-mini-transcribe"
	}
	base := os.Getenv("SAYBENCH_OPENAI_REALTIME_URL")
	if base == "" {
		base = "wss://api.openai.com/v1/realtime?intent=transcription"
	}
	return &OpenAIRealtime{key: key, model: model, base: base}, nil
}

func (o *OpenAIRealtime) Name() string { return "openai-realtime:" + o.model }

func (o *OpenAIRealtime) StreamTranscribe(ctx context.Context, audioPath string) (StreamResult, error) {
	f, err := wav.Parse(audioPath)
	if err != nil {
		return StreamResult{}, err
	}
	if f.Channels != 1 {
		return StreamResult{}, fmt.Errorf("openai-realtime: %s: mono audio required", audioPath)
	}
	pcm := resampleTo(f, openaiRealtimeRate)

	conn, _, err := websocket.Dial(ctx, o.base, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + o.key},
			"OpenAI-Beta":   {"realtime=v1"},
		},
	})
	if err != nil {
		return StreamResult{}, fmt.Errorf("openai-realtime: dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxResponseBytes)

	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, b)
	}
	if err := send(map[string]any{
		"type": "transcription_session.update",
		"session": map[string]any{
			"input_audio_format":        "pcm16",
			"input_audio_transcription": map[string]any{"model": o.model},
			"turn_detection":            map[string]any{"type": "server_vad"},
		},
	}); err != nil {
		return StreamResult{}, fmt.Errorf("openai-realtime: session update: %w", err)
	}

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
				Delta      string `json:"delta"`
				Transcript string `json:"transcript"`
			}
			if json.Unmarshal(msg, &ev) != nil {
				continue
			}
			switch ev.Type {
			case "conversation.item.input_audio_transcription.delta":
				col.interim(ev.Delta)
			case "conversation.item.input_audio_transcription.completed":
				col.final(ev.Transcript)
			}
		}
	}()

	// Feed as a synthetic wav.File at the realtime rate so pacing math holds.
	rf := &wav.File{SampleRate: openaiRealtimeRate, Channels: 1, BitsPerSample: 16, Data: pcm}
	start, end, err := feed(ctx, rf, func(chunk []byte) error {
		return send(map[string]any{
			"type":  "input_audio_buffer.append",
			"audio": base64.StdEncoding.EncodeToString(chunk),
		})
	})
	col.begin(start)
	col.endFeed(end)
	if err != nil {
		return StreamResult{}, fmt.Errorf("openai-realtime: send: %w", err)
	}
	if err := send(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		return StreamResult{}, fmt.Errorf("openai-realtime: commit: %w", err)
	}
	if err := <-readDone; err != nil && websocket.CloseStatus(err) == -1 && ctx.Err() != nil {
		return StreamResult{}, ctx.Err()
	}
	return col.result(), nil
}

// resampleTo returns the file's PCM data at the target rate.
func resampleTo(f *wav.File, rate int) []byte {
	if f.SampleRate == rate {
		return f.Data
	}
	in := make([]int16, len(f.Data)/2)
	for i := range in {
		in[i] = int16(binary.LittleEndian.Uint16(f.Data[i*2:]))
	}
	out := wav.ResampleLinear(in, f.SampleRate, rate)
	b := make([]byte, len(out)*2)
	for i, s := range out {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(s))
	}
	return b
}
