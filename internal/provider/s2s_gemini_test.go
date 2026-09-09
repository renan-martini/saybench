package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
)

func TestGeminiS2SFromSpecs(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	if _, err := S2SFromSpecsOpts("gemini", nil, S2SOpts{TurnEnding: "server_vad"}); err == nil {
		t.Fatal("expected missing key error")
	}
	t.Setenv("GEMINI_API_KEY", "test-key")
	if _, err := S2SFromSpecsOpts("gemini", nil, S2SOpts{TurnEnding: "commit"}); err == nil || !strings.Contains(err.Error(), "server_vad") {
		t.Fatalf("commit must be refused with guidance, got: %v", err)
	}
	if _, err := S2SFromSpecsOpts("gemini", nil, S2SOpts{TurnEnding: "server_vad", BargeIn: true}); err == nil {
		t.Fatal("expected barge-in refusal")
	}
	ps, err := S2SFromSpecsOpts("gemini", nil, S2SOpts{TurnEnding: "server_vad"})
	if err != nil || len(ps) != 1 || ps[0].Name() != "gemini:gemini-live-2.5-flash" {
		t.Fatalf("S2SFromSpecsOpts = %v, %v", ps, err)
	}
	// GOOGLE_API_KEY alone must also work (the fallback).
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "test-key")
	if _, err := S2SFromSpecsOpts("gemini", nil, S2SOpts{TurnEnding: "server_vad"}); err != nil {
		t.Fatalf("GOOGLE_API_KEY fallback failed: %v", err)
	}
}

// mockGeminiLive speaks the BidiGenerateContent dialect: setup →
// setupComplete, realtimeInput mediaChunks in, serverContent out.
func TestGeminiS2SAgainstLocalServer(t *testing.T) {
	var setupOK, keyOK, mimeOK atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyOK.Store(r.Header.Get("x-goog-api-key") == "test-key")
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		var audioMsgs int
		responded := false
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			var ev struct {
				Setup *struct {
					Model                    string    `json:"model"`
					OutputAudioTranscription *struct{} `json:"outputAudioTranscription"`
				} `json:"setup"`
				RealtimeInput *struct {
					MediaChunks []struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"mediaChunks"`
				} `json:"realtimeInput"`
			}
			if json.Unmarshal(msg, &ev) != nil {
				continue
			}
			switch {
			case ev.Setup != nil:
				if ev.Setup.Model != "models/gemini-live-2.5-flash" || ev.Setup.OutputAudioTranscription == nil {
					c.Close(websocket.StatusPolicyViolation, "bad setup shape")
					return
				}
				setupOK.Store(true)
				c.Write(ctx, websocket.MessageText, []byte(`{"setupComplete":{}}`))
			case ev.RealtimeInput != nil:
				if len(ev.RealtimeInput.MediaChunks) == 1 && ev.RealtimeInput.MediaChunks[0].MimeType == "audio/pcm;rate=16000" {
					mimeOK.Store(true)
				}
				audioMsgs++
				// After enough audio (speech + silence tail), the VAD
				// "detects" end of turn and the model replies: 0.5s of
				// 24kHz pcm16 across two parts, a transcript in two
				// fragments, then turnComplete.
				if audioMsgs == 20 && !responded {
					responded = true
					half := base64.StdEncoding.EncodeToString(make([]byte, 12000))
					c.Write(ctx, websocket.MessageText, []byte(`{"serverContent":{"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm;rate=24000","data":"`+half+`"}}]},"outputTranscription":{"text":"We open "}}}`))
					c.Write(ctx, websocket.MessageText, []byte(`{"serverContent":{"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm;rate=24000","data":"`+half+`"}}]},"outputTranscription":{"text":"at nine."}}}`))
					c.Write(ctx, websocket.MessageText, []byte(`{"serverContent":{"turnComplete":true}}`))
				}
			}
		}
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("SAYBENCH_GEMINI_S2S_URL", wsURL(srv))
	ps, err := S2SFromSpecsOpts("gemini", nil, S2SOpts{TurnEnding: "server_vad"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := ps[0].Converse(context.Background(), testWAV(t))
	if err != nil {
		t.Fatal(err)
	}
	if !keyOK.Load() {
		t.Fatal("key must travel in the x-goog-api-key header")
	}
	if !setupOK.Load() {
		t.Fatal("setup message never validated")
	}
	if !mimeOK.Load() {
		t.Fatal("realtimeInput chunks must declare audio/pcm;rate=16000")
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

func TestGeminiS2SSurfacesSetupRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.Read(r.Context())
		// The Live API refuses a bad setup by closing the socket; the
		// close reason is the only diagnosis the client gets.
		c.Close(websocket.StatusPolicyViolation, "model not found: models/gemini-live-2.5-flash")
	}))
	defer srv.Close()
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("SAYBENCH_GEMINI_S2S_URL", wsURL(srv))
	ps, _ := S2SFromSpecsOpts("gemini", nil, S2SOpts{TurnEnding: "server_vad"})
	if _, err := ps[0].Converse(context.Background(), testWAV(t)); err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("close reason not surfaced: %v", err)
	}
}

func TestMimeRate(t *testing.T) {
	for _, tc := range []struct {
		mime string
		want int
	}{
		{"audio/pcm;rate=24000", 24000},
		{"audio/pcm;rate=16000;foo=bar", 16000},
		{"audio/pcm", 24000},
		{"", 24000},
		{"audio/pcm;rate=bogus", 24000},
	} {
		if got := mimeRate(tc.mime, 24000); got != tc.want {
			t.Errorf("mimeRate(%q) = %d, want %d", tc.mime, got, tc.want)
		}
	}
}
