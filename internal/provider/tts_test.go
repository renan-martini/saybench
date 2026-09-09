package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFakeTTSDeterministic(t *testing.T) {
	f := NewFakeTTS()
	r1, err := f.Speak(context.Background(), "hello there caller")
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := f.Speak(context.Background(), "hello there caller")
	if !reflect.DeepEqual(r1, r2) {
		t.Fatalf("fake-tts not deterministic")
	}
	if r1.TTFAudioMS <= 0 || r1.TotalMS <= r1.TTFAudioMS || r1.AudioMS <= 0 {
		t.Fatalf("shape wrong: %+v", r1)
	}
}

func TestTTSFromSpecs(t *testing.T) {
	ps, err := TTSFromSpecs("fake-tts")
	if err != nil || len(ps) != 1 || ps[0].Name() != "fake-tts" {
		t.Fatalf("TTSFromSpecs = %v, %v", ps, err)
	}
	if _, err := TTSFromSpecs("bogus"); err == nil {
		t.Fatal("expected unknown provider error")
	}
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := TTSFromSpecs("openai"); err == nil {
		t.Fatal("expected missing key error")
	}
	t.Setenv("ELEVENLABS_API_KEY", "")
	if _, err := TTSFromSpecs("elevenlabs"); err == nil {
		t.Fatal("expected missing key error")
	}
	t.Setenv("SAYBENCH_TTS_BASE_URL", "")
	if _, err := TTSFromSpecs("custom"); err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestOpenAITTSStreams(t *testing.T) {
	var gotAuth string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b := make([]byte, 4096)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		w.Header().Set("Content-Type", "audio/pcm")
		fl := w.(http.Flusher)
		time.Sleep(6 * time.Millisecond)
		// 0.25s of 24kHz mono pcm16 in two chunks
		w.Write(make([]byte, 6000))
		fl.Flush()
		time.Sleep(4 * time.Millisecond)
		w.Write(make([]byte, 6000))
	}))
	defer srv.Close()

	tts := newOpenAITTS("test", srv.URL, "sk-test", "gpt-4o-mini-tts", "alloy")
	res, err := tts.Speak(context.Background(), "hello caller")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"response_format":"pcm"`) || !strings.Contains(gotBody, `"input":"hello caller"`) {
		t.Fatalf("request body wrong: %s", gotBody)
	}
	if res.TTFAudioMS <= 0 || res.TotalMS < res.TTFAudioMS {
		t.Fatalf("timings wrong: %+v", res)
	}
	if res.AudioMS < 240 || res.AudioMS > 260 {
		t.Fatalf("audio duration = %dms, want ~250 (12000 bytes @24kHz pcm16)", res.AudioMS)
	}
}

func TestElevenLabsTTSStreams(t *testing.T) {
	var gotKey, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("xi-api-key")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		fl := w.(http.Flusher)
		time.Sleep(5 * time.Millisecond)
		w.Write(make([]byte, 4800)) // 0.1s @24kHz pcm16
		fl.Flush()
	}))
	defer srv.Close()

	tts := newElevenLabsTTS(srv.URL, "el-key", "voice123", "eleven_turbo_v2_5")
	res, err := tts.Speak(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "el-key" {
		t.Fatalf("xi-api-key = %q", gotKey)
	}
	if !strings.Contains(gotPath, "/v1/text-to-speech/voice123/stream") {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "output_format=pcm_24000") {
		t.Fatalf("query = %q", gotQuery)
	}
	if res.TTFAudioMS <= 0 || res.AudioMS < 90 || res.AudioMS > 110 {
		t.Fatalf("result wrong: %+v", res)
	}
}

func TestTTSErrorBodySurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"voice not found"}}`, http.StatusNotFound)
	}))
	defer srv.Close()
	tts := newOpenAITTS("test", srv.URL, "k", "m", "v")
	if _, err := tts.Speak(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "voice not found") {
		t.Fatalf("error not surfaced: %v", err)
	}
	_ = fmt.Sprint() // keep fmt import
}
