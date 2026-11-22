package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"
)

// TestEncodeWAV checks every byte of a file, header and all.
func TestEncodeWAV(t *testing.T) {
	s := NewSound(8000, 2, []float32{0, 0.5, -0.5, -1})
	want := []byte{
		'R', 'I', 'F', 'F', 44, 0, 0, 0, // the file's size minus these 8 bytes
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ', 16, 0, 0, 0, // the fmt chunk
		1, 0, // PCM
		2, 0, // channels
		0x40, 0x1f, 0, 0, // 8000 Hz
		0x00, 0x7d, 0, 0, // 32000 bytes a second
		4, 0, // bytes a frame
		16, 0, // bits a sample
		'd', 'a', 't', 'a', 8, 0, 0, 0, // the data chunk
		0x00, 0x00, // 0
		0x00, 0x40, // 0.5
		0x00, 0xc0, // -0.5
		0x00, 0x80, // -1
	}
	if got := s.EncodeWAV(); !bytes.Equal(got, want) {
		t.Errorf("EncodeWAV =\n% x\nwant\n% x", got, want)
	}
	var b bytes.Buffer
	if err := s.WriteWAV(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("WriteWAV =\n% x\nwant\n% x", b.Bytes(), want)
	}
}

// TestWAVRoundTrip encodes sounds and decodes them again: the samples come
// back within half a step of 16 bits, and nothing else changes.
func TestWAVRoundTrip(t *testing.T) {
	sine := func(n int) []float32 {
		s := make([]float32, n)
		for i := range s {
			s[i] = float32(math.Sin(float64(i) / 7))
		}
		return s
	}
	for _, tc := range []struct {
		name     string
		rate, ch int
		samples  []float32
	}{
		{"mono", 8000, 1, sine(1000)},
		{"stereo", 44100, 2, sine(1000)},
		{"empty", 22050, 1, nil},
		{"past a chunk", 48000, 2, sine(20002)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := NewSound(tc.rate, tc.ch, tc.samples)
			got, err := DecodeSound(want.EncodeWAV())
			if err != nil {
				t.Fatal(err)
			}
			if got.rate != tc.rate || got.channels != tc.ch {
				t.Errorf("decoded %d Hz, %d channels, want %d Hz, %d", got.rate, got.channels, tc.rate, tc.ch)
			}
			if len(got.samples) != len(tc.samples) {
				t.Fatalf("decoded %d samples, want %d", len(got.samples), len(tc.samples))
			}
			for i, v := range tc.samples {
				if d := math.Abs(float64(got.samples[i] - v)); d > 1.0/32768 {
					t.Errorf("sample %d = %v, want %v (off by %v)", i, got.samples[i], v, d)
				}
			}
			if got.Duration() != want.Duration() {
				t.Errorf("Duration = %v, want %v", got.Duration(), want.Duration())
			}
		})
	}
}

// Samples beyond full scale are clipped, not wrapped, which would click, and
// a sample that is not a number is silence.
func TestWAVClips(t *testing.T) {
	s := NewSound(8000, 1, []float32{2, -2, 1, -1, float32(math.NaN()), float32(math.Inf(1))})
	got, err := DecodeSound(s.EncodeWAV())
	if err != nil {
		t.Fatal(err)
	}
	full := float32(1<<15-1) / (1 << 15)
	want := []float32{full, -1, full, -1, 0, full}
	for i := range want {
		if got.samples[i] != want[i] {
			t.Errorf("sample %d = %v, want %v", i, got.samples[i], want[i])
		}
	}
}

// A sound of a frame and a half encodes its whole frames, and says so.
func TestWAVOfAWholeFrame(t *testing.T) {
	s := &Sound{samples: []float32{0.5, 0.5, 0.5}, channels: 2, rate: 8000}
	b := s.EncodeWAV()
	if len(b) != wavHeaderSize+4 {
		t.Fatalf("encoded %d bytes, want %d", len(b), wavHeaderSize+4)
	}
	if n := binary.LittleEndian.Uint32(b[40:]); n != 4 {
		t.Errorf("the data chunk says %d bytes, want 4", n)
	}
	if n := binary.LittleEndian.Uint32(b[4:]); n != uint32(len(b)-8) {
		t.Errorf("the file says %d bytes, want %d", n+8, len(b))
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

func TestWriteWAVErrors(t *testing.T) {
	s := NewSound(8000, 1, []float32{0.5})
	want := errors.New("no room")
	if err := s.WriteWAV(failingWriter{want}); !errors.Is(err, want) {
		t.Errorf("WriteWAV = %v, want %v", err, want)
	}
	if err := s.WriteWAV(zeroWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("WriteWAV = %v, want %v", err, io.ErrShortWrite)
	}
}

// A WAV file's sizes are uint32: a sound too long for them is an error, not
// a file of a wrong size.
func TestWAVTooLong(t *testing.T) {
	for _, tc := range []struct {
		frames, channels int
		want             uint32
		ok               bool
	}{
		{0, 1, 0, true},
		{2, 2, 8, true},
		{1073741814, 2, 4294967256, true}, // the last that fits
		{1073741815, 2, 0, false},         // 4 bytes more than 4 GB
		{1 << 30, 2, 0, false},            // more than a slice holds
	} {
		got, ok := wavData(tc.frames, tc.channels)
		if got != tc.want || ok != tc.ok {
			t.Errorf("wavData(%d, %d) = %d, %v, want %d, %v", tc.frames, tc.channels, got, ok, tc.want, tc.ok)
		}
	}
}
