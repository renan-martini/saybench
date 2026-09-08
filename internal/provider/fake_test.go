package provider

import (
	"context"
	"testing"
)

func TestFakeIsDeterministic(t *testing.T) {
	refs := map[string]string{"/a.wav": "one two three four five six seven eight nine ten eleven twelve"}
	f := NewFake(refs)
	r1, err := f.Transcribe(context.Background(), "/a.wav")
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := f.Transcribe(context.Background(), "/a.wav")
	if r1.Text != r2.Text || r1.Latency != r2.Latency {
		t.Fatalf("fake not deterministic: %+v vs %+v", r1, r2)
	}
	// Word 8 dropped, word 11 substituted.
	if r1.Text == "one two three four five six seven eight nine ten eleven twelve" {
		t.Fatal("fake produced a perfect transcript; it must inject errors")
	}
}

func TestFakeUnknownPath(t *testing.T) {
	f := NewFake(nil)
	if _, err := f.Transcribe(context.Background(), "/nope.wav"); err == nil {
		t.Fatal("expected error for unknown path")
	}
}

func TestFromSpecs(t *testing.T) {
	ps, err := FromSpecs("fake", nil)
	if err != nil || len(ps) != 1 || ps[0].Name() != "fake" {
		t.Fatalf("FromSpecs(fake) = %v, %v", ps, err)
	}
	if _, err := FromSpecs("bogus", nil); err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if _, err := FromSpecs("", nil); err == nil {
		t.Fatal("expected error for empty spec")
	}
}
