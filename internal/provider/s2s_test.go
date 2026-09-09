package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestFakeS2SDeterministic(t *testing.T) {
	refs := map[string]string{"/a.wav": "what are your opening hours today"}
	f := NewFakeS2S(refs)
	r1, err := f.Converse(context.Background(), "/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := f.Converse(context.Background(), "/a.wav")
	if !reflect.DeepEqual(r1, r2) {
		t.Fatalf("fake-s2s not deterministic: %+v vs %+v", r1, r2)
	}
	if r1.V2VFirstAudioMS <= 0 || r1.ResponseDoneMS <= r1.V2VFirstAudioMS || r1.OutputAudioMS <= 0 || r1.Transcript == "" {
		t.Fatalf("shape wrong: %+v", r1)
	}
}

func TestS2SFromSpecs(t *testing.T) {
	ps, err := S2SFromSpecs("fake-s2s", map[string]string{})
	if err != nil || len(ps) != 1 || ps[0].Name() != "fake-s2s" {
		t.Fatalf("S2SFromSpecs = %v, %v", ps, err)
	}
	if _, err := S2SFromSpecs("bogus", nil); err == nil {
		t.Fatal("expected unknown provider error")
	}
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := S2SFromSpecs("openai", nil); err == nil {
		t.Fatal("expected missing key error")
	}
	t.Setenv("SAYBENCH_S2S_URL", "")
	if _, err := S2SFromSpecs("custom", nil); err == nil {
		t.Fatal("expected missing SAYBENCH_S2S_URL error")
	}
}

// mockRealtimeS2S speaks the GA realtime speech-to-speech dialect.
func mockRealtimeS2S(t *testing.T, requireModelParam string) (*httptest.Server, *int32) {
	t.Helper()
	sessionSeen := new(int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requireModelParam != "" && r.URL.Query().Get("model") != requireModelParam {
			http.Error(w, "missing model", http.StatusBadRequest)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"session.created"}`))
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
							TurnDetection any `json:"turn_detection"`
						} `json:"input"`
					} `json:"audio"`
				} `json:"session"`
			}
			json.Unmarshal(msg, &ev)
			switch ev.Type {
			case "session.update":
				if ev.Session.Type != "realtime" || ev.Session.Audio.Input.TurnDetection != nil {
					c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":{"message":"bad session shape"}}`))
					c.Close(websocket.StatusPolicyViolation, "bad session")
					return
				}
				*sessionSeen++
				sessionOK = true
			case "response.create":
				if !sessionOK {
					c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":{"message":"no session"}}`))
					return
				}
				time.Sleep(6 * time.Millisecond) // model think time
				// 0.5s of 24kHz mono pcm16 across two deltas
				half := base64.StdEncoding.EncodeToString(make([]byte, 12000))
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.output_audio.delta","delta":"`+half+`"}`))
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.output_audio_transcript.delta","delta":"We open "}`))
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.audio.delta","delta":"`+half+`"}`)) // beta name — must also count
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.audio_transcript.delta","delta":"at nine."}`))
				c.Write(ctx, websocket.MessageText, []byte(`{"type":"response.done"}`))
			}
		}
	}))
	return srv, sessionSeen
}

func TestOpenAIS2SAgainstLocalServer(t *testing.T) {
	srv, sessionSeen := mockRealtimeS2S(t, "gpt-realtime")
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("SAYBENCH_OPENAI_S2S_URL", wsURL(srv))
	ps, err := S2SFromSpecs("openai", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ps[0].Converse(context.Background(), testWAV(t))
	if err != nil {
		t.Fatal(err)
	}
	if *sessionSeen != 1 {
		t.Fatal("session.update never validated")
	}
	if res.Transcript != "We open at nine." {
		t.Fatalf("transcript = %q", res.Transcript)
	}
	if res.V2VFirstAudioMS <= 0 || res.ResponseDoneMS < res.V2VFirstAudioMS {
		t.Fatalf("timings wrong: %+v", res)
	}
	// 24000 bytes total = 12000 samples @24kHz = 500ms
	if res.OutputAudioMS < 490 || res.OutputAudioMS > 510 {
		t.Fatalf("output audio = %dms, want ~500", res.OutputAudioMS)
	}
}

func TestOpenAIS2SSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		c.Read(ctx)
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":{"message":"insufficient quota"}}`))
		c.Close(websocket.StatusPolicyViolation, "")
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("SAYBENCH_OPENAI_S2S_URL", wsURL(srv))
	ps, _ := S2SFromSpecs("openai", nil)
	if _, err := ps[0].Converse(context.Background(), testWAV(t)); err == nil || !strings.Contains(err.Error(), "insufficient quota") {
		t.Fatalf("server error not surfaced: %v", err)
	}
}
