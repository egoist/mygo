package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// clipPackets reads the video of the test clip as the packets an encoder
// would have handed over: its samples with start codes, and the avcC a
// decoder needs.
func clipPackets(t *testing.T) (CodecConfig, []EncodedPacket) {
	t.Helper()
	f, err := mp4.ReadMP4File("testdata/clip.mp4")
	if err != nil {
		t.Fatalf("reading the test clip: %v", err)
	}
	var trak *mp4.TrakBox
	for _, tr := range f.Moov.Traks {
		if tr.Mdia != nil && tr.Mdia.Hdlr != nil && tr.Mdia.Hdlr.HandlerType == "vide" {
			trak = tr
		}
	}
	if trak == nil {
		t.Fatal("the test clip has no video track")
	}
	stsd := trak.Mdia.Minf.Stbl.Stsd
	if stsd.AvcX == nil || stsd.AvcX.AvcC == nil {
		t.Fatal("the test clip's video is not H.264")
	}
	var extra bytes.Buffer
	if err := stsd.AvcX.AvcC.DecConfRec.Encode(&extra); err != nil {
		t.Fatalf("encoding the avcC: %v", err)
	}
	n := int(trak.Mdia.Minf.Stbl.Stsz.GetNrSamples())
	samples, err := trak.GetSampleData(1, uint32(n))
	if err != nil {
		t.Fatalf("reading the test clip's sample table: %v", err)
	}
	all, err := f.ReadSampleData(trak, 1, uint32(n), nil, nil)
	if err != nil {
		t.Fatalf("reading the test clip's sample data: %v", err)
	}
	timescale := trak.Mdia.Mdhd.Timescale
	cfg := CodecConfig{
		Codec:  CodecH264,
		Extra:  extra.Bytes(),
		Width:  int(stsd.AvcX.Width),
		Height: int(stsd.AvcX.Height),
	}
	var packets []EncodedPacket
	var pts time.Duration
	at := 0
	for i, s := range samples {
		size := int(s.Size)
		if at+size > len(all) {
			t.Fatalf("sample %d runs past the clip's data", i)
		}
		dur := time.Duration(int64(s.Dur) * int64(time.Second) / int64(timescale))
		packets = append(packets, EncodedPacket{
			PTS:      pts,
			Duration: dur,
			Keyframe: s.IsSync(),
			Data:     avccToStartCodes(all[at:at+size], 4),
		})
		at += size
		pts += dur
	}
	return cfg, packets
}

// avccToStartCodes turns the length-prefixed NALUs a file or an encoder's
// sample buffer holds into the start-code form packets travel in.
func avccToStartCodes(data []byte, lengthSize int) []byte {
	var out []byte
	at := 0
	for at+lengthSize <= len(data) {
		n := 0
		for i := 0; i < lengthSize; i++ {
			n = n<<8 | int(data[at+i])
		}
		at += lengthSize
		if at+n > len(data) {
			break
		}
		out = append(out, 0, 0, 0, 1)
		out = append(out, data[at:at+n]...)
		at += n
	}
	return out
}

// TestTheMP4MuxerWritesThePicturesItWasGiven muxes the test clip's real
// H.264 samples and reads them back out of the file it wrote.
func TestTheMP4MuxerWritesThePicturesItWasGiven(t *testing.T) {
	cfg, packets := clipPackets(t)
	var out bytes.Buffer
	m, err := NewMuxer(&out, ContainerMP4)
	if err != nil {
		t.Fatalf("opening a muxer: %v", err)
	}
	track, err := m.AddTrack(cfg)
	if err != nil {
		t.Fatalf("adding the video track: %v", err)
	}
	for i := range packets {
		packets[i].Track = track
		if err := m.WritePacket(packets[i]); err != nil {
			t.Fatalf("writing packet %d: %v", i, err)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatalf("finishing the file: %v", err)
	}

	f, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("reading back the file written: %v", err)
	}
	if !f.IsFragmented() {
		t.Fatal("the muxer wrote a file that is not fragmented")
	}
	if len(f.Init.Moov.Traks) != 1 {
		t.Fatalf("the file has %d tracks, want 1", len(f.Init.Moov.Traks))
	}
	trak := f.Init.Moov.Traks[0]
	if got := trak.Mdia.Minf.Stbl.Stsd.AvcX; got == nil {
		t.Fatal("the file's video track is not H.264")
	} else if got.Width != uint16(cfg.Width) || got.Height != uint16(cfg.Height) {
		t.Fatalf("the file's picture is %dx%d, want %dx%d", got.Width, got.Height, cfg.Width, cfg.Height)
	}
	trex := f.Init.Moov.Mvex.Trexs[0]
	var got [][]byte
	for _, seg := range f.Segments {
		for _, frag := range seg.Fragments {
			samples, err := frag.GetFullSamples(trex)
			if err != nil {
				t.Fatalf("reading the samples of a fragment: %v", err)
			}
			for _, s := range samples {
				got = append(got, s.Data)
			}
		}
	}
	if len(got) != len(packets) {
		t.Fatalf("the file has %d pictures, want %d", len(got), len(packets))
	}
	for i := range got {
		want := nalusToAVCC(nalusOf2(t, packets[i].Data), 4)
		if !bytes.Equal(got[i], want) {
			t.Fatalf("picture %d differs from the one written", i)
		}
	}
}

// nalusOf2 is the test's way to get a packet's NALUs, the same split the
// muxer does.
func nalusOf2(t *testing.T, data []byte) [][]byte {
	t.Helper()
	nalus, err := nalusOf(data)
	if err != nil {
		t.Fatalf("splitting a packet: %v", err)
	}
	return nalus
}

// nalusToAVCC is what the muxer writes: the length-prefixed form.
func nalusToAVCC(nalus [][]byte, lengthSize int) []byte {
	var out []byte
	for _, n := range nalus {
		for i := lengthSize - 1; i >= 0; i-- {
			out = append(out, byte(uint32(len(n))>>(8*i)))
		}
		out = append(out, n...)
	}
	return out
}

// TestTheMuxerWritesAFileAPlayerCanOpen completes the file, writes it where
// the system's own decoders can read it, and plays the first picture.
func TestTheMuxerWritesAFileAPlayerCanOpen(t *testing.T) {
	cfg, packets := clipPackets(t)
	var out bytes.Buffer
	m, err := NewMuxer(&out, ContainerMP4)
	if err != nil {
		t.Fatalf("opening a muxer: %v", err)
	}
	track, err := m.AddTrack(cfg)
	if err != nil {
		t.Fatalf("adding the video track: %v", err)
	}
	for i := range packets {
		packets[i].Track = track
		if err := m.WritePacket(packets[i]); err != nil {
			t.Fatalf("writing packet %d: %v", i, err)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatalf("finishing the file: %v", err)
	}
	path := filepath.Join(t.TempDir(), "muxed.mp4")
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := Open(path)
	if err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skipf("this machine cannot play what it muxed: %v", err)
		}
		t.Fatalf("opening the file muxed: %v", err)
	}
	defer src.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f, err := src.NextFrame(ctx)
	if err != nil {
		t.Fatalf("the first picture of the file muxed: %v", err)
	}
	defer f.Release()
	if f.Format != PixelRGBA {
		t.Fatalf("the picture muxed came back as %v, not RGBA", f.Format)
	}
	if f.Width != cfg.Width || f.Height != cfg.Height {
		t.Fatalf("the picture muxed came back as %dx%d, want %dx%d", f.Width, f.Height, cfg.Width, cfg.Height)
	}
}

// TestAMuxerThatCannotWriteSaysWhatIsMissing checks that a container this
// version does not write is an unsupported error naming it, not a broken
// file.
func TestAMuxerThatCannotWriteSaysWhatIsMissing(t *testing.T) {
	_, err := NewMuxer(&bytes.Buffer{}, ContainerWebM)
	if err == nil {
		t.Fatal("a WebM muxer was opened for writing")
	}
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("writing WebM failed with %v, not an unsupported error", err)
	}
}

// TestTheMuxerSaysWhatItCannotWrite checks the ways a file cannot be
// started: an unsupported codec, a track added late, and a first picture
// that is not a keyframe.
func TestTheMuxerSaysWhatItCannotWrite(t *testing.T) {
	cfg, packets := clipPackets(t)
	t.Run("codec", func(t *testing.T) {
		m, err := NewMuxer(&bytes.Buffer{}, ContainerMP4)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.AddTrack(CodecConfig{Codec: CodecVP9}); err == nil || !errors.Is(err, errors.ErrUnsupported) {
			t.Fatalf("adding a VP9 track gave %v, not an unsupported error", err)
		}
	})
	t.Run("late track", func(t *testing.T) {
		var out bytes.Buffer
		m, err := NewMuxer(&out, ContainerMP4)
		if err != nil {
			t.Fatal(err)
		}
		track, err := m.AddTrack(cfg)
		if err != nil {
			t.Fatal(err)
		}
		p := packets[0]
		p.Track = track
		if err := m.WritePacket(p); err != nil {
			t.Fatal(err)
		}
		if _, err := m.AddTrack(cfg); err == nil {
			t.Fatal("a track was added after the first packet")
		}
	})
	t.Run("first picture", func(t *testing.T) {
		m, err := NewMuxer(&bytes.Buffer{}, ContainerMP4)
		if err != nil {
			t.Fatal(err)
		}
		track, err := m.AddTrack(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var p EncodedPacket
		for _, v := range packets {
			if !v.Keyframe {
				p = v
				break
			}
		}
		p.Track = track
		if err := m.WritePacket(p); err == nil {
			t.Fatal("a file was started with a picture that is not a keyframe")
		}
	})
}
