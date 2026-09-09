package provider

import (
	"context"
	"testing"
)

func TestFakeStreamIsDeterministicAndStreamShaped(t *testing.T) {
	refs := map[string]string{"/a.wav": "one two three four five six seven eight nine ten eleven twelve"}
	f := NewFakeStream(refs)
	r1, err := f.StreamTranscribe(context.Background(), "/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := f.StreamTranscribe(context.Background(), "/a.wav")
	if r1 != r2 {
		t.Fatalf("fake-stream not deterministic: %+v vs %+v", r1, r2)
	}
	if r1.TTFPartialMS <= 0 || r1.FinalLagMS <= 0 || r1.Interims <= 0 {
		t.Fatalf("stream metrics must be positive: %+v", r1)
	}
	if r1.Text == "" {
		t.Fatal("empty transcript")
	}
}

func TestStreamFromSpecs(t *testing.T) {
	ps, err := StreamFromSpecs("fake-stream", nil)
	if err != nil || len(ps) != 1 || ps[0].Name() != "fake-stream" {
		t.Fatalf("StreamFromSpecs = %v, %v", ps, err)
	}
	if _, err := StreamFromSpecs("bogus", nil); err == nil {
		t.Fatal("expected error for unknown streaming provider")
	}
	if _, err := StreamFromSpecs("fake-stream,fake-stream", nil); err == nil {
		t.Fatal("expected error for duplicate spec")
	}
}
