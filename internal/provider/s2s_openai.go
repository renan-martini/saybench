package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
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
	// serverVAD lets the model detect end-of-speech itself (production
	// posture). The feed appends a silence tail so the VAD can fire, and
	// V2V anchors at end of SPEECH — VAD hangover is part of the number.
	serverVAD bool
	// bargeIn interrupts the reply with bargeClip and measures stop time.
	bargeIn   bool
	bargeClip string
}

const s2sRate = 24000

// outcome carries the reader goroutine's terminal result.
type outcome struct {
	res S2SResult
	err error
}

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
	var turnDetection any // nil = deterministic commit mode
	if r.serverVAD {
		turnDetection = map[string]any{"type": "server_vad"}
	}
	if err := send(map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type":         "realtime",
			"instructions": r.instructions,
			"audio": map[string]any{
				"input": map[string]any{
					"format":         map[string]any{"type": "audio/pcm", "rate": s2sRate},
					"turn_detection": turnDetection,
				},
				"output": map[string]any{
					"format": map[string]any{"type": "audio/pcm", "rate": s2sRate},
				},
			},
		},
	}); err != nil {
		return S2SResult{}, fmt.Errorf("%s: session update: %w", r.name, err)
	}

	done := make(chan outcome, 1)
	var turnEnd time.Time
	turnEndCh := make(chan time.Time, 1)
	firstAudioCh := make(chan struct{}, 1)
	var lastDelta atomicTime
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
					select {
					case firstAudioCh <- struct{}{}:
					default:
					}
				}
				lastDelta.set(time.Now())
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
	if r.serverVAD {
		// The user's turn ends when the speech does; the VAD must notice.
		// Feed a silence tail so it can — its hangover time is measured.
		turnEnd = time.Now()
		turnEndCh <- turnEnd
		silence := &wav.File{SampleRate: s2sRate, Channels: 1, BitsPerSample: 16, Data: make([]byte, s2sRate*2*3/2)} // 1.5s
		if _, _, err := feed(ctx, silence, func(chunk []byte) error {
			return send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(chunk)})
		}); err != nil {
			return failWith("send silence tail", err)
		}
	} else {
		if err := send(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
			return failWith("commit", err)
		}
		if err := send(map[string]any{"type": "response.create"}); err != nil {
			return failWith("response.create", err)
		}
		// The user's turn is over the instant the model is allowed to speak.
		turnEnd = time.Now()
		turnEndCh <- turnEnd
	}

	if r.bargeIn {
		return r.bargeInWait(ctx, send, done, firstAudioCh, &lastDelta)
	}
	select {
	case o := <-done:
		return o.res, o.err
	case <-ctx.Done():
		return S2SResult{}, ctx.Err()
	}
}

// bargeInWait lets the reply start, interrupts it with the barge clip, and
// measures how long the model kept talking after the interruption began.
func (r *realtimeS2S) bargeInWait(ctx context.Context, send func(any) error, done chan outcome, firstAudio chan struct{}, lastDelta *atomicTime) (S2SResult, error) {
	select {
	case <-firstAudio:
	case o := <-done:
		return o.res, o.err
	case <-ctx.Done():
		return S2SResult{}, ctx.Err()
	}
	select {
	case <-time.After(700 * time.Millisecond):
	case <-ctx.Done():
		return S2SResult{}, ctx.Err()
	}
	clip := r.bargeClip
	if clip == "" {
		clip = "golden/barge/interrupt.wav"
	}
	bf, err := wav.Parse(clip)
	if err != nil {
		return S2SResult{}, fmt.Errorf("%s: barge clip: %w", r.name, err)
	}
	bpcm := resampleTo(bf, s2sRate)
	interruptStart := time.Now()
	brf := &wav.File{SampleRate: s2sRate, Channels: 1, BitsPerSample: 16, Data: bpcm}
	if _, _, err := feed(ctx, brf, func(chunk []byte) error {
		return send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(chunk)})
	}); err != nil {
		return S2SResult{}, fmt.Errorf("%s: send interrupt: %w", r.name, err)
	}
	// The model stopped when its deltas did: wait for done, or for the
	// delta stream to have gone silent long enough to call it stopped.
	deadline := time.After(6 * time.Second)
	var o outcome
waitStop:
	for {
		select {
		case o = <-done:
			break waitStop
		case <-deadline:
			break waitStop
		case <-time.After(250 * time.Millisecond):
			if ld := lastDelta.get(); !ld.IsZero() && time.Since(ld) > 2*time.Second {
				break waitStop
			}
		case <-ctx.Done():
			return S2SResult{}, ctx.Err()
		}
	}
	if o.err != nil {
		return S2SResult{}, o.err
	}
	res := o.res
	if ld := lastDelta.get(); !ld.IsZero() {
		stop := int(ld.Sub(interruptStart).Milliseconds())
		if stop < 0 {
			stop = 0
		}
		res.BargeInStopMS = stop + 1 // never 0 for a measured stop
	}
	return res, nil
}

// atomicTime is a tiny atomic wrapper for the last-delta timestamp.
type atomicTime struct{ v atomic.Value }

func (a *atomicTime) set(t time.Time) { a.v.Store(t) }
func (a *atomicTime) get() time.Time {
	if t, ok := a.v.Load().(time.Time); ok {
		return t
	}
	return time.Time{}
}

func hasQuery(u string) bool {
	for i := range u {
		if u[i] == '?' {
			return true
		}
	}
	return false
}
