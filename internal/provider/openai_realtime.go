package provider

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"

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
		HTTPHeader: http.Header{"Authorization": {"Bearer " + o.key}},
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
	// GA transcription-session shape (the beta "transcription_session.update"
	// with input_audio_format:"pcm16" is rejected by the live API).
	// turn_detection:null means no server VAD — we commit explicitly after
	// the feed ends, which makes finalization deterministic for measurement.
	if err := send(map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{
				"input": map[string]any{
					"format":         map[string]any{"type": "audio/pcm", "rate": openaiRealtimeRate},
					"transcription":  map[string]any{"model": o.model},
					"turn_detection": nil,
				},
			},
		},
	}); err != nil {
		return StreamResult{}, fmt.Errorf("openai-realtime: session update: %w", err)
	}

	col := &collector{}
	readDone := make(chan error, 1)
	completed := make(chan struct{}, 1)
	var serverErr atomic.Value // last {"type":"error"} event message
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
				Error      struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(msg, &ev) != nil {
				continue
			}
			switch ev.Type {
			case "conversation.item.input_audio_transcription.delta":
				col.interim(ev.Delta)
			case "conversation.item.input_audio_transcription.completed":
				col.final(ev.Transcript)
				select {
				case completed <- struct{}{}:
				default:
				}
			case "error":
				serverErr.Store(ev.Error.Message)
			}
		}
	}()
	// failWith folds the server's own error event (the actual reason) into
	// any transport-level failure, instead of reporting "connection closed".
	failWith := func(stage string, err error) error {
		if m, ok := serverErr.Load().(string); ok && m != "" {
			return fmt.Errorf("openai-realtime: %s: server error: %s", stage, m)
		}
		return fmt.Errorf("openai-realtime: %s: %w", stage, err)
	}

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
		<-readDone // let the reader capture any error event first
		return StreamResult{}, failWith("send", err)
	}
	if err := send(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		<-readDone
		return StreamResult{}, failWith("commit", err)
	}
	// The live API keeps the socket open after the transcript completes —
	// the client owns the goodbye. Wait for the completed event (or an
	// early server close / deadline), then close and drain the reader.
	select {
	case <-completed:
		conn.Close(websocket.StatusNormalClosure, "")
		<-readDone
	case err := <-readDone:
		if m, ok := serverErr.Load().(string); ok && m != "" {
			return StreamResult{}, fmt.Errorf("openai-realtime: server error: %s", m)
		}
		if err != nil && websocket.CloseStatus(err) == -1 {
			if ctx.Err() != nil {
				return StreamResult{}, ctx.Err()
			}
			return StreamResult{}, fmt.Errorf("openai-realtime: connection: %w", err)
		}
	case <-ctx.Done():
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
