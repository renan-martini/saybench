package provider

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testWAV writes a 0.2s 16kHz mono PCM16 wav (4 paced chunks).
func testWAV(t *testing.T) string {
	t.Helper()
	samples := make([]int16, 3200)
	var data bytes.Buffer
	for _, s := range samples {
		binary.Write(&data, binary.LittleEndian, s)
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+data.Len()))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint32(16000))
	binary.Write(&b, binary.LittleEndian, uint32(32000))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(data.Len()))
	b.Write(data.Bytes())
	p := filepath.Join(t.TempDir(), "t.wav")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func wsURL(s *httptest.Server) string { return "ws" + strings.TrimPrefix(s.URL, "http") }

func TestDeepgramStreamAgainstLocalServer(t *testing.T) {
	gotAuth := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		sentInterim := false
		for {
			typ, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary && !sentInterim {
				sentInterim = true
				time.Sleep(5 * time.Millisecond) // realistic think-time so TTFP is measurably > 0
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"Results","is_final":false,"channel":{"alternatives":[{"transcript":"hello"}]}}`))
			}
			if typ == websocket.MessageText && strings.Contains(string(msg), "CloseStream") {
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"Results","is_final":true,"speech_final":true,"channel":{"alternatives":[{"transcript":"hello world"}]}}`))
				c.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	}))
	defer srv.Close()

	t.Setenv("DEEPGRAM_API_KEY", "test-key")
	t.Setenv("SAYBENCH_DEEPGRAM_STREAM_URL", wsURL(srv))
	p, err := NewDeepgramStream()
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.StreamTranscribe(context.Background(), testWAV(t))
	if err != nil {
		t.Fatal(err)
	}
	if auth := <-gotAuth; auth != "Token test-key" {
		t.Fatalf("auth header = %q", auth)
	}
	if res.Text != "hello world" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.TTFPartialMS <= 0 || res.Interims != 1 || res.FinalLagMS < 0 {
		t.Fatalf("timings wrong: %+v", res)
	}
}

func TestOpenAIRealtimeAgainstLocalServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"transcription_session.created"}`))
		var appends int
		sessionOK := false
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			var ev struct {
				Type    string `json:"type"`
				Session struct {
					Type  string `json:"type"`
					Audio struct {
						Input struct {
							Format struct {
								Type string `json:"type"`
								Rate int    `json:"rate"`
							} `json:"format"`
						} `json:"input"`
					} `json:"audio"`
				} `json:"session"`
			}
			json.Unmarshal(msg, &ev)
			switch ev.Type {
			case "session.update":
				// GA dialect: reject anything else, like the real API does.
				if ev.Session.Type != "transcription" || ev.Session.Audio.Input.Format.Type != "audio/pcm" || ev.Session.Audio.Input.Format.Rate != 24000 {
					c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":{"message":"invalid session shape"}}`))
					c.Close(websocket.StatusPolicyViolation, "bad session")
					return
				}
				sessionOK = true
			case "input_audio_buffer.append":
				if !sessionOK {
					c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":{"message":"audio before session.update"}}`))
					c.Close(websocket.StatusPolicyViolation, "no session")
					return
				}
				appends++
				if appends == 1 {
					time.Sleep(5 * time.Millisecond) // realistic think-time so TTFP is measurably > 0
					c.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.delta","delta":"hel"}`))
				}
			case "input_audio_buffer.commit":
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"conversation.item.input_audio_transcription.completed","transcript":"hello world"}`))
				// Like the real API: the server does NOT close after
				// completed — the client must close once it has its result.
			}
		}
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("SAYBENCH_OPENAI_REALTIME_URL", wsURL(srv))
	p, err := NewOpenAIRealtime()
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.StreamTranscribe(context.Background(), testWAV(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello world" || res.Interims != 1 || res.TTFPartialMS <= 0 {
		t.Fatalf("result wrong: %+v", res)
	}
}

func TestAssemblyAIStreamAgainstLocalServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"Begin"}`))
		seenAudio := false
		for {
			typ, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary && !seenAudio {
				seenAudio = true
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"Turn","transcript":"hello","end_of_turn":false}`))
			}
			if typ == websocket.MessageText && strings.Contains(string(msg), "Terminate") {
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"Turn","transcript":"hello world","end_of_turn":true}`))
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"Termination"}`))
				c.Close(websocket.StatusNormalClosure, "")
				return
			}
		}
	}))
	defer srv.Close()

	t.Setenv("ASSEMBLYAI_API_KEY", "test-key")
	t.Setenv("SAYBENCH_ASSEMBLYAI_STREAM_URL", wsURL(srv))
	p, err := NewAssemblyAIStream()
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.StreamTranscribe(context.Background(), testWAV(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello world" || res.Interims != 1 {
		t.Fatalf("result wrong: %+v", res)
	}
}

func TestOpenAIRealtimeSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		// Reject immediately with an error event, like the real API does on a
		// bad session — the adapter must surface the message, not swallow it
		// behind "use of closed network connection".
		c.Read(ctx)
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":{"message":"invalid session shape"}}`))
		c.Close(websocket.StatusPolicyViolation, "bad session")
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("SAYBENCH_OPENAI_REALTIME_URL", wsURL(srv))
	p, _ := NewOpenAIRealtime()
	_, err := p.StreamTranscribe(context.Background(), testWAV(t))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid session shape") {
		t.Fatalf("server error event not surfaced: %v", err)
	}
}

func TestStreamVendorsRequireKeys(t *testing.T) {
	for _, unset := range []string{"DEEPGRAM_API_KEY", "OPENAI_API_KEY", "ASSEMBLYAI_API_KEY"} {
		t.Setenv(unset, "")
	}
	if _, err := NewDeepgramStream(); err == nil {
		t.Fatal("deepgram: expected missing-key error")
	}
	if _, err := NewOpenAIRealtime(); err == nil {
		t.Fatal("openai: expected missing-key error")
	}
	if _, err := NewAssemblyAIStream(); err == nil {
		t.Fatal("assemblyai: expected missing-key error")
	}
}
