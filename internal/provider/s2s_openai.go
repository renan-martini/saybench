package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"

	"github.com/renan-martini/saybench/internal/wav"
)

// realtimeS2S benchmarks a speech-to-speech model over the OpenAI-Realtime
// dialect (openai itself, or any compatible endpoint via the custom spec).
//
// One FRESH connection per clip, deliberately — the opposite of the LLM-WS
// rule, for a reason: realtime sessions are stateful conversations, and
// separate clips must be separate conversations or context contamination
// corrupts the measurement.
type realtimeS2S struct {
	name, base, key, model string
	instructions           string
}

const s2sRate = 24000

func newRealtimeS2S(name, base, key, model string) *realtimeS2S {
	return &realtimeS2S{
		name: name, base: base, key: key, model: model,
		instructions: "You are a helpful phone agent. Respond briefly and naturally.",
	}
}

func (r *realtimeS2S) Name() string { return r.name }

func (r *realtimeS2S) Converse(ctx context.Context, audioPath string) (S2SResult, error) {
	f, err := wav.Parse(audioPath)
	if err != nil {
		return S2SResult{}, err
	}
	if f.Channels != 1 {
		return S2SResult{}, fmt.Errorf("%s: %s: mono audio required", r.name, audioPath)
	}
	pcm := resampleTo(f, s2sRate)

	u := r.base
	if r.model != "" {
		sep := "?"
		if hasQuery(u) {
			sep = "&"
		}
		u += sep + "model=" + url.QueryEscape(r.model)
	}
	hdr := http.Header{}
	if r.key != "" {
		hdr.Set("Authorization", "Bearer "+r.key)
	}
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		return S2SResult{}, fmt.Errorf("%s: dial: %w", r.name, err)
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
	// Deterministic turn ending: no server VAD; we commit and ask.
	if err := send(map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type":         "realtime",
			"instructions": r.instructions,
			"audio": map[string]any{
				"input": map[string]any{
					"format":         map[string]any{"type": "audio/pcm", "rate": s2sRate},
					"turn_detection": nil,
				},
				"output": map[string]any{
					"format": map[string]any{"type": "audio/pcm", "rate": s2sRate},
				},
			},
		},
	}); err != nil {
		return S2SResult{}, fmt.Errorf("%s: session update: %w", r.name, err)
	}

	type outcome struct {
		res S2SResult
		err error
	}
	done := make(chan outcome, 1)
	var turnEnd time.Time
	turnEndCh := make(chan time.Time, 1)
	go func() {
		var firstAudio, doneAt time.Time
		var audioBytes int
		var transcript []byte
		var serverErr string
		end := time.Time{}
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				if serverErr != "" {
					done <- outcome{err: fmt.Errorf("%s: server error: %s", r.name, serverErr)}
				} else {
					done <- outcome{err: fmt.Errorf("%s: read: %w", r.name, err)}
				}
				return
			}
			var ev struct {
				Type  string `json:"type"`
				Delta string `json:"delta"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(msg, &ev) != nil {
				continue
			}
			switch ev.Type {
			// GA and beta event names both count — the Realtime rename
			// already bit this codebase once.
			case "response.output_audio.delta", "response.audio.delta":
				if firstAudio.IsZero() {
					firstAudio = time.Now()
				}
				if b, err := base64.StdEncoding.DecodeString(ev.Delta); err == nil {
					audioBytes += len(b)
				}
			case "response.output_audio_transcript.delta", "response.audio_transcript.delta":
				transcript = append(transcript, ev.Delta...)
			case "error":
				serverErr = ev.Error.Message
			case "response.done":
				doneAt = time.Now()
				if end.IsZero() {
					end = <-turnEndCh
				}
				res := S2SResult{Transcript: string(transcript)}
				if !firstAudio.IsZero() {
					res.V2VFirstAudioMS = ceilMS(firstAudio.Sub(end))
				}
				res.ResponseDoneMS = ceilMS(doneAt.Sub(end))
				res.OutputAudioMS = audioBytes / 2 * 1000 / s2sRate
				if res.V2VFirstAudioMS == 0 && serverErr != "" {
					done <- outcome{err: fmt.Errorf("%s: server error: %s", r.name, serverErr)}
					return
				}
				done <- outcome{res: res}
				return
			}
		}
	}()

	// failWith prefers the reader's outcome (which folds in any server
	// error event — the actual reason) over a bare transport error.
	failWith := func(stage string, err error) (S2SResult, error) {
		select {
		case o := <-done:
			if o.err != nil {
				return S2SResult{}, o.err
			}
		case <-time.After(2 * time.Second):
		}
		return S2SResult{}, fmt.Errorf("%s: %s: %w", r.name, stage, err)
	}

	rf := &wav.File{SampleRate: s2sRate, Channels: 1, BitsPerSample: 16, Data: pcm}
	if _, _, err := feed(ctx, rf, func(chunk []byte) error {
		return send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(chunk)})
	}); err != nil {
		return failWith("send audio", err)
	}
	if err := send(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		return failWith("commit", err)
	}
	if err := send(map[string]any{"type": "response.create"}); err != nil {
		return failWith("response.create", err)
	}
	// The user's turn is over the instant the model is allowed to speak.
	turnEnd = time.Now()
	turnEndCh <- turnEnd

	select {
	case o := <-done:
		return o.res, o.err
	case <-ctx.Done():
		return S2SResult{}, ctx.Err()
	}
}

func hasQuery(u string) bool {
	for i := range u {
		if u[i] == '?' {
			return true
		}
	}
	return false
}
