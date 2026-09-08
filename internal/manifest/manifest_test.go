package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.wav", "fake-audio")
	write(t, dir, "b.wav", "fake-audio")
	mf := write(t, dir, "manifest.jsonl",
		`{"audio":"a.wav","reference":"hello world","category":"names"}
// a comment line

{"audio":"b.wav","reference":"second clip"}
`)
	items, err := Load(mf)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if !filepath.IsAbs(items[0].Audio) {
		t.Fatalf("audio not resolved to absolute: %s", items[0].Audio)
	}
	if items[1].Category != "uncategorized" {
		t.Fatalf("missing category not defaulted, got %q", items[1].Category)
	}
}

func TestLoadRejectsMissingAudio(t *testing.T) {
	dir := t.TempDir()
	mf := write(t, dir, "manifest.jsonl", `{"audio":"nope.wav","reference":"x"}`)
	if _, err := Load(mf); err == nil {
		t.Fatal("expected error for missing audio file")
	}
}

func TestLoadRejectsEmptyManifest(t *testing.T) {
	dir := t.TempDir()
	mf := write(t, dir, "manifest.jsonl", "\n// nothing\n")
	if _, err := Load(mf); err == nil {
		t.Fatal("expected error for empty manifest")
	}
}

func TestLoadRejectsConflictingDuplicateAudio(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.wav", "x")
	mf := write(t, dir, "manifest.jsonl",
		`{"audio":"a.wav","reference":"first"}
{"audio":"a.wav","reference":"second"}
`)
	if _, err := Load(mf); err == nil {
		t.Fatal("expected error for conflicting duplicate audio")
	}
}
