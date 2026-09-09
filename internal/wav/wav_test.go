package wav

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildWAV writes a minimal RIFF/WAVE PCM file with the given samples.
func buildWAV(t *testing.T, rate int, channels int, samples []int16) string {
	t.Helper()
	var data bytes.Buffer
	for _, s := range samples {
		binary.Write(&data, binary.LittleEndian, s)
	}
	var b bytes.Buffer
	byteRate := rate * channels * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+data.Len()))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(byteRate))
	binary.Write(&b, binary.LittleEndian, uint16(channels*2))
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

func TestParsePCM(t *testing.T) {
	// 16000 samples at 16kHz mono = exactly 1 second.
	p := buildWAV(t, 16000, 1, make([]int16, 16000))
	f, err := Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.SampleRate != 16000 || f.Channels != 1 || f.BitsPerSample != 16 {
		t.Fatalf("format = %+v", f)
	}
	if f.Duration() != time.Second {
		t.Fatalf("duration = %v, want 1s", f.Duration())
	}
	if len(f.Data) != 32000 {
		t.Fatalf("data len = %d, want 32000", len(f.Data))
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.wav")
	os.WriteFile(p, []byte("not a wav at all"), 0o644)
	if _, err := Parse(p); err == nil {
		t.Fatal("expected error for non-WAV file")
	}
}

func TestParseRejectsNonPCM(t *testing.T) {
	p := buildWAV(t, 16000, 1, make([]int16, 100))
	raw, _ := os.ReadFile(p)
	raw[20] = 3 // audio format = IEEE float
	os.WriteFile(p, raw, 0o644)
	if _, err := Parse(p); err == nil {
		t.Fatal("expected error for non-PCM format")
	}
}

func TestChunks(t *testing.T) {
	// 1s of 16kHz mono → 50ms chunks = 20 chunks of 1600 bytes.
	p := buildWAV(t, 16000, 1, make([]int16, 16000))
	f, _ := Parse(p)
	chunks := f.Chunks(50 * time.Millisecond)
	if len(chunks) != 20 {
		t.Fatalf("got %d chunks, want 20", len(chunks))
	}
	for _, c := range chunks {
		if len(c) != 1600 {
			t.Fatalf("chunk size %d, want 1600", len(c))
		}
	}
	// Non-dividing duration keeps the remainder as a final short chunk.
	p2 := buildWAV(t, 16000, 1, make([]int16, 16000+400))
	f2, _ := Parse(p2)
	chunks2 := f2.Chunks(50 * time.Millisecond)
	if len(chunks2) != 21 || len(chunks2[20]) != 800 {
		t.Fatalf("remainder handling wrong: %d chunks, last %d bytes", len(chunks2), len(chunks2[len(chunks2)-1]))
	}
}

func TestResampleLinear(t *testing.T) {
	// A 16kHz ramp resampled to 24kHz keeps endpoints and grows 1.5x.
	in := make([]int16, 16000)
	for i := range in {
		in[i] = int16(i % 32000 / 2)
	}
	out := ResampleLinear(in, 16000, 24000)
	if got, want := len(out), 24000; int(math.Abs(float64(got-want))) > 2 {
		t.Fatalf("resampled length %d, want ~%d", got, want)
	}
	if out[0] != in[0] {
		t.Fatalf("first sample %d != %d", out[0], in[0])
	}
	// Same-rate is a no-op.
	same := ResampleLinear(in, 16000, 16000)
	if len(same) != len(in) || same[7] != in[7] {
		t.Fatal("same-rate resample must be identity")
	}
}
