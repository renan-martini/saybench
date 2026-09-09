// Package wav parses PCM WAV files just far enough to stream them: format,
// duration, raw sample data, real-time chunking, and a linear resampler for
// vendors that demand a different rate. Deliberately not a general audio
// library.
package wav

import (
	"encoding/binary"
	"fmt"
	"os"
	"time"
)

// File is a parsed PCM WAV file.
type File struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
	// Data is the raw little-endian PCM payload.
	Data []byte
}

// Duration returns the audio length implied by the data size and format.
func (f *File) Duration() time.Duration {
	bytesPerSec := f.SampleRate * f.Channels * f.BitsPerSample / 8
	if bytesPerSec == 0 {
		return 0
	}
	return time.Duration(len(f.Data)) * time.Second / time.Duration(bytesPerSec)
}

// Chunks splits the payload into pieces of the given duration (the last one
// may be shorter). Chunk boundaries land on whole frames.
func (f *File) Chunks(d time.Duration) [][]byte {
	frame := f.Channels * f.BitsPerSample / 8
	bytesPerChunk := int(int64(f.SampleRate)*int64(d)/int64(time.Second)) * frame
	if bytesPerChunk <= 0 {
		return [][]byte{f.Data}
	}
	var out [][]byte
	for off := 0; off < len(f.Data); off += bytesPerChunk {
		end := min(off+bytesPerChunk, len(f.Data))
		out = append(out, f.Data[off:end])
	}
	return out
}

// Parse reads a PCM WAV file. Non-PCM encodings and malformed headers are
// errors — this package streams exactly what it can verify.
func Parse(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, fmt.Errorf("%s: not a RIFF/WAVE file", path)
	}
	f := &File{}
	haveFmt := false
	// Walk chunks: 4-byte id, 4-byte size, payload (word-aligned).
	for off := 12; off+8 <= len(raw); {
		id := string(raw[off : off+4])
		size := int(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		body := off + 8
		if body+size > len(raw) {
			return nil, fmt.Errorf("%s: truncated %q chunk", path, id)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, fmt.Errorf("%s: fmt chunk too small", path)
			}
			format := binary.LittleEndian.Uint16(raw[body : body+2])
			if format != 1 { // PCM
				return nil, fmt.Errorf("%s: audio format %d is not PCM (only PCM WAV is supported)", path, format)
			}
			f.Channels = int(binary.LittleEndian.Uint16(raw[body+2 : body+4]))
			f.SampleRate = int(binary.LittleEndian.Uint32(raw[body+4 : body+8]))
			f.BitsPerSample = int(binary.LittleEndian.Uint16(raw[body+14 : body+16]))
			haveFmt = true
		case "data":
			f.Data = raw[body : body+size]
		}
		off = body + size
		if size%2 == 1 {
			off++ // chunks are word-aligned
		}
	}
	if !haveFmt || f.Data == nil {
		return nil, fmt.Errorf("%s: missing fmt or data chunk", path)
	}
	if f.BitsPerSample != 16 {
		return nil, fmt.Errorf("%s: %d-bit PCM is not supported (16-bit only)", path, f.BitsPerSample)
	}
	return f, nil
}

// ResampleLinear converts 16-bit mono samples between rates with linear
// interpolation. Good enough to satisfy a vendor's input format; resampling
// quality is deliberately not part of what saybench measures.
func ResampleLinear(in []int16, fromRate, toRate int) []int16 {
	if fromRate == toRate || len(in) == 0 {
		return in
	}
	n := int(int64(len(in)) * int64(toRate) / int64(fromRate))
	out := make([]int16, n)
	for i := range out {
		pos := float64(i) * float64(fromRate) / float64(toRate)
		j := int(pos)
		if j >= len(in)-1 {
			out[i] = in[len(in)-1]
			continue
		}
		frac := pos - float64(j)
		out[i] = int16(float64(in[j])*(1-frac) + float64(in[j+1])*frac)
	}
	return out
}
