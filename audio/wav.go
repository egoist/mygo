package audio

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

func init() {
	RegisterFormat("wav", "RIFF????WAVE", func(r io.ReadSeeker) (Decoder, error) { return newWAV(r) })
}

// A wavDecoder decodes WAV files of integer PCM of 8 to 32 bits, or of
// 32 or 64 bit floats.
type wavDecoder struct {
	r        io.ReadSeeker
	br       io.Reader
	rate     int
	channels int
	bits     int
	float    bool
	start    int64 // the data's offset
	length   int64 // frames, or -1
	pos      int64 // frame
	buf      []byte
}

var errWAV = errors.New("audio: invalid WAV")

func newWAV(r io.ReadSeeker) (*wavDecoder, error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return nil, errWAV
	}
	d := &wavDecoder{r: r}
	off := start + 12
	haveFmt := false
	for {
		var h [8]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return nil, errWAV
		}
		size := int64(binary.LittleEndian.Uint32(h[4:]))
		off += 8
		switch string(h[:4]) {
		case "fmt ":
			if size < 16 || size > 1<<16 {
				return nil, errWAV
			}
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil, errWAV
			}
			tag := binary.LittleEndian.Uint16(b)
			d.channels = int(binary.LittleEndian.Uint16(b[2:]))
			d.rate = int(binary.LittleEndian.Uint32(b[4:]))
			d.bits = int(binary.LittleEndian.Uint16(b[14:]))
			if tag == 0xfffe && size >= 40 {
				// WAVE_FORMAT_EXTENSIBLE: the tag starts the sub-format.
				tag = binary.LittleEndian.Uint16(b[24:])
			}
			switch {
			case tag == 1 && d.bits >= 8 && d.bits <= 32 && d.bits%8 == 0:
			case tag == 3 && (d.bits == 32 || d.bits == 64):
				d.float = true
			default:
				return nil, errors.New("audio: WAV of an encoding other than PCM or float")
			}
			if d.channels < 1 || d.rate < 1 {
				return nil, errWAV
			}
			haveFmt = true
		case "data":
			if !haveFmt {
				return nil, errWAV
			}
			d.start = off
			d.length = -1
			if size != 0 && size != 0xffffffff {
				// Not one written as it streamed.
				d.length = size / int64(d.frameBytes())
			}
			d.br = readerOf(r)
			return d, nil
		}
		// Chunks are padded to even sizes.
		size += size & 1
		if _, err := r.Seek(off+size, io.SeekStart); err != nil {
			return nil, errWAV
		}
		off += size
	}
}

func (d *wavDecoder) frameBytes() int { return d.channels * d.bits / 8 }
func (d *wavDecoder) SampleRate() int { return d.rate }
func (d *wavDecoder) Channels() int   { return d.channels }
func (d *wavDecoder) Len() int64      { return d.length }

func (d *wavDecoder) Read(dst []float32) (int, error) {
	fb := d.frameBytes()
	n := len(dst) / d.channels
	if d.length >= 0 {
		n = int(min(int64(n), d.length-d.pos))
	}
	if n <= 0 {
		return 0, io.EOF
	}
	if cap(d.buf) < n*fb {
		d.buf = make([]byte, n*fb)
	}
	b := d.buf[:n*fb]
	got, err := io.ReadFull(d.br, b)
	n = got / fb
	if n == 0 {
		if err == nil || err == io.ErrUnexpectedEOF {
			err = io.EOF
		}
		return 0, err
	}
	bs := d.bits / 8
	for i := range n * d.channels {
		s := b[i*bs:]
		var v float32
		switch {
		case d.float && bs == 4:
			v = math.Float32frombits(binary.LittleEndian.Uint32(s))
		case d.float:
			v = float32(math.Float64frombits(binary.LittleEndian.Uint64(s)))
		case bs == 1:
			v = (float32(s[0]) - 128) / 128
		case bs == 2:
			v = float32(int16(binary.LittleEndian.Uint16(s))) / (1 << 15)
		case bs == 3:
			v = float32(int32(uint32(s[0])<<8|uint32(s[1])<<16|uint32(s[2])<<24)>>8) / (1 << 23)
		default:
			v = float32(int32(binary.LittleEndian.Uint32(s))) / (1 << 31)
		}
		dst[i] = v
	}
	d.pos += int64(n)
	return n * d.channels, nil
}

// wavHeaderSize is the size of the header of an encoded WAV: the RIFF, fmt
// and data chunks, no others.
const wavHeaderSize = 44

// WriteWAV writes s to w as a WAV file of 16-bit PCM, which every system
// plays, in the sound's channels and sample rate. Samples beyond -1 and 1,
// which a sound mixed past full scale has, are clipped rather than wrapped:
// a wrap turns the top of a loud sound into a click. A sample that is not a
// number is silence:
//
//	f, err := os.Create("effect.wav")
//	if err != nil {
//		return err
//	}
//	defer f.Close()
//	if err := s.WriteWAV(f); err != nil {
//		return err
//	}
//
// A sound that DecodeSound read writes to a file whose samples come back
// within a step of 16 bits, 1/32768, of what was decoded, which full scale
// positive spends on 32767 rather than 32768. Samples past a whole frame,
// which NewSound and decoding never make, are left out, as they are left out
// of Duration. A sound of more than six hours at 44.1 kHz runs past what the
// RIFF header's sizes can say, and is an error.
func (s *Sound) WriteWAV(w io.Writer) error {
	frames := len(s.samples) / s.channels
	data, ok := wavData(frames, s.channels)
	if !ok {
		return errors.New("audio: a WAV file holds less than 4 GB of samples")
	}
	if err := writeAll(w, s.wavHeader(data)); err != nil {
		return err
	}
	samples := s.samples[:frames*s.channels]
	const chunk = 4096 // samples, 8 KB
	buf := make([]byte, 0, chunk*2)
	for i := 0; i < len(samples); i += chunk {
		buf = buf[:0]
		for _, v := range samples[i:min(i+chunk, len(samples))] {
			buf = binary.LittleEndian.AppendUint16(buf, pcm16(v))
		}
		if err := writeAll(w, buf); err != nil {
			return err
		}
	}
	return nil
}

// wavData returns the size of frames of channels 16-bit samples, and
// whether a WAV file holds them: the RIFF header's sizes are uint32.
func wavData(frames, channels int) (uint32, bool) {
	n := uint64(frames) * uint64(channels) * 2
	if n > math.MaxUint32-(wavHeaderSize-8) {
		return 0, false
	}
	return uint32(n), true
}

// wavHeader returns the header of a WAV file of data bytes of 16-bit PCM.
func (s *Sound) wavHeader(data uint32) []byte {
	h := make([]byte, wavHeaderSize)
	le := binary.LittleEndian
	copy(h, "RIFF")
	le.PutUint32(h[4:], wavHeaderSize-8+data)
	copy(h[8:], "WAVE")
	copy(h[12:], "fmt ")
	le.PutUint32(h[16:], 16)                          // the fmt chunk's size
	le.PutUint16(h[20:], 1)                           // PCM
	le.PutUint16(h[22:], uint16(s.channels))          // channels
	le.PutUint32(h[24:], uint32(s.rate))              // samples a second
	le.PutUint32(h[28:], uint32(s.rate*s.channels*2)) // bytes a second
	le.PutUint16(h[32:], uint16(s.channels*2))        // bytes a frame
	le.PutUint16(h[34:], 16)                          // bits a sample
	copy(h[36:], "data")
	le.PutUint32(h[40:], data) // the data chunk's size
	return h
}

// pcm16 returns v as a 16-bit sample: -1 is -32768, and 1 is 32767, a step
// short of 32768, which the decoder reads as -1.
func pcm16(v float32) uint16 {
	if v != v { // not a number
		return 0
	}
	n := math.Round(float64(min(max(v, -1), 1)) * (1 << 15))
	return uint16(int16(min(n, 1<<15-1)))
}

// writeAll writes b to w.
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		b = b[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (d *wavDecoder) SetPosition(frame int64) error {
	if frame < 0 || d.length >= 0 && frame > d.length {
		return errors.New("audio: seek out of range")
	}
	if _, err := d.r.Seek(d.start+frame*int64(d.frameBytes()), io.SeekStart); err != nil {
		return err
	}
	d.br = readerOf(d.r)
	d.pos = frame
	return nil
}
