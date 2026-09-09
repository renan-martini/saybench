package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/renan-martini/saybench/internal/wav"
)

// geminiS2S benchmarks Google's Live API (BidiGenerateContent over WS) —
// the speech-to-speech surface behind Gemini Live. Input audio feeds as
// 16kHz PCM realtimeInput chunks; output arrives as 24kHz inlineData parts
// plus an outputTranscription of what the model said.
//
// Turn handling is automatic-VAD only — the Live API has no deterministic
// commit. The spec routing refuses -turn-ending commit; server_vad
// semantics apply (silence tail fed after the speech, V2V anchored at end
// of speech, VAD hangover included in the number — which is the point).
//
// Same isolation rule as the realtime adapter: one FRESH connection per
// clip, because sessions are stateful conversations.
//
// LIVE-UNVERIFIED: protocol-tested against a local mock speaking the
// documented BidiGenerateContent dialect, not yet run against the real
// endpoint. Google also renames models often — if the endpoint rejects
// the default model, its error is surfaced; override with
// SAYBENCH_GEMINI_S2S_MODEL.
type geminiS2S struct {
	name, base, key, model string
	instructions           string
}

const geminiInRate = 16000
const geminiDefaultOutRate = 24000

func newGeminiS2S(name, base, key, model string) *geminiS2S {
	return &geminiS2S{
		name: name, base: base, key: key, model: model,
		instructions: "You are a helpful phone agent. Respond briefly and naturally.",
	}
}

func (g *geminiS2S) Name() string { return g.name }

func (g *geminiS2S) Converse(ctx context.Context, audioPath string) (S2SResult, error) {
	f, err := wav.Parse(audioPath)
	if err != nil {
		return S2SResult{}, err
	}
	if f.Channels != 1 {
		return S2SResult{}, fmt.Errorf("%s: %s: mono audio required", g.name, audioPath)
	}
	pcm := resampleTo(f, geminiInRate)

	hdr := http.Header{}
	// The key travels in a header, never in the URL — dial errors embed
	// the URL, and keys must never leak into logs.
	if g.key != "" {
		hdr.Set("x-goog-api-key", g.key)
	}
	conn, _, err := websocket.Dial(ctx, g.base, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		return S2SResult{}, fmt.Errorf("%s: dial: %w", g.name, err)
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
	model := g.model
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	if err := send(map[string]any{
		"setup": map[string]any{
			"model":                    model,
			"generationConfig":         map[string]any{"responseModalities": []string{"AUDIO"}},
			"systemInstruction":        map[string]any{"parts": []any{map[string]any{"text": g.instructions}}},
			"outputAudioTranscription": map[string]any{},
		},
	}); err != nil {
		return S2SResult{}, fmt.Errorf("%s: setup: %w", g.name, err)
	}
	// The handshake: no audio until the server acknowledges the setup.
	// A refusal closes the socket — the close reason IS the diagnosis
	// (bad key, renamed model), so it flows into the error verbatim.
	_, boot, err := conn.Read(ctx)
	if err != nil {
		return S2SResult{}, fmt.Errorf("%s: setup handshake: %w", g.name, err)
	}
	var ack struct {
		SetupComplete *struct{} `json:"setupComplete"`
	}
	if json.Unmarshal(boot, &ack) != nil || ack.SetupComplete == nil {
		return S2SResult{}, fmt.Errorf("%s: expected setupComplete, got %.200s", g.name, boot)
	}

	done := make(chan outcome, 1)
	turnEndCh := make(chan time.Time, 1)
	go func() {
		var firstAudio time.Time
		var audioBytes int
		var transcript []byte
		outRate := geminiDefaultOutRate
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				done <- outcome{err: fmt.Errorf("%s: read: %w", g.name, err)}
				return
			}
			var ev struct {
				ServerContent struct {
					ModelTurn struct {
						Parts []struct {
							InlineData struct {
								MimeType string `json:"mimeType"`
								Data     string `json:"data"`
							} `json:"inlineData"`
						} `json:"parts"`
					} `json:"modelTurn"`
					OutputTranscription struct {
						Text string `json:"text"`
					} `json:"outputTranscription"`
					TurnComplete bool `json:"turnComplete"`
				} `json:"serverContent"`
			}
			if json.Unmarshal(msg, &ev) != nil {
				continue
			}
			sc := ev.ServerContent
			for _, p := range sc.ModelTurn.Parts {
				if p.InlineData.Data == "" {
					continue
				}
				if firstAudio.IsZero() {
					firstAudio = time.Now()
				}
				outRate = mimeRate(p.InlineData.MimeType, outRate)
				if b, err := base64.StdEncoding.DecodeString(p.InlineData.Data); err == nil {
					audioBytes += len(b)
				}
			}
			transcript = append(transcript, sc.OutputTranscription.Text...)
			if sc.TurnComplete {
				doneAt := time.Now()
				end := <-turnEndCh
				res := S2SResult{Transcript: string(transcript)}
				if !firstAudio.IsZero() {
					res.V2VFirstAudioMS = ceilMS(firstAudio.Sub(end))
				}
				res.ResponseDoneMS = ceilMS(doneAt.Sub(end))
				res.OutputAudioMS = audioBytes / 2 * 1000 / outRate
				done <- outcome{res: res}
				return
			}
		}
	}()

	// failWith prefers the reader's outcome — a socket the server closed
	// mid-feed carries the actual reason there, not in the send error.
	failWith := func(stage string, err error) (S2SResult, error) {
		select {
		case o := <-done:
			if o.err != nil {
				return S2SResult{}, o.err
			}
		case <-time.After(2 * time.Second):
		}
		return S2SResult{}, fmt.Errorf("%s: %s: %w", g.name, stage, err)
	}

	sendChunk := func(chunk []byte) error {
		return send(map[string]any{
			"realtimeInput": map[string]any{
				"mediaChunks": []any{map[string]any{
					"mimeType": fmt.Sprintf("audio/pcm;rate=%d", geminiInRate),
					"data":     base64.StdEncoding.EncodeToString(chunk),
				}},
			},
		})
	}
	rf := &wav.File{SampleRate: geminiInRate, Channels: 1, BitsPerSample: 16, Data: pcm}
	if _, _, err := feed(ctx, rf, sendChunk); err != nil {
		return failWith("send audio", err)
	}
	// The user's turn ends when the speech does; the VAD must notice.
	// Feed a silence tail so it can — its hangover time is measured.
	turnEndCh <- time.Now()
	silence := &wav.File{SampleRate: geminiInRate, Channels: 1, BitsPerSample: 16, Data: make([]byte, geminiInRate*2*3/2)} // 1.5s
	if _, _, err := feed(ctx, silence, sendChunk); err != nil {
		return failWith("send silence tail", err)
	}

	select {
	case o := <-done:
		return o.res, o.err
	case <-ctx.Done():
		return S2SResult{}, ctx.Err()
	}
}

// mimeRate pulls the sample rate out of "audio/pcm;rate=24000".
func mimeRate(mime string, def int) int {
	const marker = "rate="
	i := strings.Index(mime, marker)
	if i < 0 {
		return def
	}
	rest := mime[i+len(marker):]
	if j := strings.IndexAny(rest, ";, "); j >= 0 {
		rest = rest[:j]
	}
	if n, err := strconv.Atoi(rest); err == nil && n > 0 {
		return n
	}
	return def
}
