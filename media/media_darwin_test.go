//go:build darwin

package media

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
)

// The fixtures are a two second 160×120 H.264 video with mono AAC sound, and
// a second of a 220 Hz tone in a mono 44.1 kHz WAV, both made with ffmpeg.
const (
	clipPath = "testdata/clip.mp4"
	wavPath  = "testdata/tone.wav"
)

func openTest(t *testing.T, path string) Source {
	t.Helper()
	s, err := openSource(path, options{})
	if err != nil {
		t.Fatalf("openSource(%q): %v", path, err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCapabilitiesReportTheSystemsOwnCodecs(t *testing.T) {
	c := Capabilities()
	for _, f := range []Feature{FeaturePlayback} {
		if !c.Supports(f) {
			t.Errorf("Capabilities() = %s, without %s", c, f)
		}
	}
	for _, codec := range []Codec{CodecH264, CodecHEVC, CodecAAC} {
		if !c.CanDecode(codec) {
			t.Errorf("Capabilities() = %s, which cannot decode %s", c, codec)
		}
	}
	if !c.CanRead(ContainerMP4) {
		t.Errorf("Capabilities() = %s, which cannot read %s", c, ContainerMP4)
	}
}

func TestTracksAreTheFilesOwn(t *testing.T) {
	s := openTest(t, clipPath)
	ts, ok := s.(TrackSource)
	if !ok {
		t.Fatalf("%T does not tell its tracks", s)
	}
	tracks := ts.Tracks()
	if len(tracks) != 2 {
		t.Fatalf("the file has %d tracks, want 2: %+v", len(tracks), tracks)
	}
	var video, sound *Track
	for i := range tracks {
		switch tracks[i].Kind {
		case TrackVideo:
			video = &tracks[i]
		case TrackAudio:
			sound = &tracks[i]
		}
	}
	if video == nil {
		t.Fatalf("the file has no picture: %+v", tracks)
	}
	if video.Codec != CodecH264 {
		t.Errorf("the picture is %s, want H.264", video.Codec)
	}
	if video.Width != 160 || video.Height != 120 {
		t.Errorf("the picture is %dx%d, want 160x120", video.Width, video.Height)
	}
	if sound == nil {
		t.Fatalf("the file has no sound: %+v", tracks)
	}
	if sound.Codec != CodecAAC {
		t.Errorf("the sound is %s, want AAC", sound.Codec)
	}
	if sound.SampleRate != 44100 || sound.Channels != 1 {
		t.Errorf("the sound is %d Hz in %d channels, want 44100 in 1", sound.SampleRate, sound.Channels)
	}
	d, ok := s.(DurationSource)
	if !ok {
		t.Fatalf("%T does not tell its length", s)
	}
	if got := d.Duration(); got < 1900*time.Millisecond || got > 2100*time.Millisecond {
		t.Errorf("the file is %v long, want about 2s", got)
	}
}

func TestNextFrameHandsOutTheFilesPictures(t *testing.T) {
	s := openTest(t, clipPath)
	ctx := context.Background()
	var last, step time.Duration
	for i := range 5 {
		f, err := s.NextFrame(ctx)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if f.Format != PixelRGBA {
			t.Fatalf("frame %d is %s, want RGBA", i, f.Format)
		}
		if f.Width != 160 || f.Height != 120 {
			t.Fatalf("frame %d is %dx%d, want 160x120", i, f.Width, f.Height)
		}
		if len(f.Planes) != 1 {
			t.Fatalf("frame %d has %d planes, want 1", i, len(f.Planes))
		}
		if p := f.Planes[0]; p.Stride != 4*f.Width || len(p.Pix) != p.Stride*f.Height {
			t.Fatalf("frame %d holds %d bytes in rows of %d", i, len(p.Pix), p.Stride)
		}
		if i > 0 && f.PTS <= last {
			t.Fatalf("frame %d is at %v, after frame %d at %v", i, f.PTS, i-1, last)
		}
		if f.PTS < 0 || f.PTS > 2*time.Second {
			t.Fatalf("frame %d is at %v, outside the file", i, f.PTS)
		}
		if f.Duration < 0 {
			t.Errorf("frame %d lasts %v", i, f.Duration)
		}
		if i == 1 {
			step = f.PTS - last
		}
		f.Release()
		last = f.PTS
	}
	// The file is 15 frames a second, so its pictures are 66ms apart.
	// (AVFoundation gives a picture no duration of its own: a picture
	// lasts until the next one, which is the PTS that follows.)
	if step < 50*time.Millisecond || step > 85*time.Millisecond {
		t.Errorf("the pictures are %v apart, want about %v", step, time.Second/15)
	}
}

func TestFramesShowThePicture(t *testing.T) {
	s := openTest(t, clipPath)
	f, err := s.NextFrame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	img := f.Image()
	if img == nil {
		t.Fatal("the frame has no image")
	}
	if img.Rect.Dx() != 160 || img.Rect.Dy() != 120 {
		t.Fatalf("the image is %v", img.Rect)
	}
	// A test picture is not one color, and its pixels are opaque.
	first := [4]byte{img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3]}
	var differ bool
	for i := 4; i < len(img.Pix); i += 4 {
		if img.Pix[i] != first[0] || img.Pix[i+1] != first[1] || img.Pix[i+2] != first[2] {
			differ = true
			break
		}
	}
	if !differ {
		t.Error("every pixel of the picture is the same color")
	}
	if first[3] != 0xff {
		t.Errorf("the first pixel's alpha is %#x, want opaque", first[3])
	}
}

func TestTheEndOfTheFileIsTheEndOfTheSource(t *testing.T) {
	s := openTest(t, clipPath)
	ctx := context.Background()
	// The file is two seconds at 15 frames a second: no more than a few
	// hundred pictures come out of it.
	for i := 0; i < 1000; i++ {
		f, err := s.NextFrame(ctx)
		if errors.Is(err, ErrEnd) {
			if i < 15 {
				t.Fatalf("the file ended after %d pictures", i)
			}
			return
		}
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		f.Release()
	}
	t.Fatal("the file never ended")
}

func TestSeekReadsFromThere(t *testing.T) {
	s := openTest(t, clipPath)
	if err := s.(Seekable).Seek(1200 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	f, err := s.NextFrame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	// Where the reader starts is the picture at the sought time, or the
	// one before it that it had to decode from.
	if f.PTS < 1100*time.Millisecond || f.PTS > 1400*time.Millisecond {
		t.Errorf("after seeking to 1.2s the first picture is at %v", f.PTS)
	}
}

func TestSoundDecodesToWhatTheAudioPackageMixes(t *testing.T) {
	s := openTest(t, clipPath)
	ss, ok := s.(soundSource)
	if !ok {
		t.Fatalf("%T has no sound", s)
	}
	d := ss.Sound()
	if d == nil {
		t.Fatal("the file's sound is nil")
	}
	if d.SampleRate() != soundRate || d.Channels() != soundChannels {
		t.Fatalf("the sound is %d Hz in %d channels, want %d in %d",
			d.SampleRate(), d.Channels(), soundRate, soundChannels)
	}
	frames, peak := readSound(t, d, 0)
	if want := 2 * soundRate; frames < want-soundRate/10 || frames > want+soundRate/10 {
		t.Errorf("the sound is %d frames long, want about %d", frames, want)
	}
	if peak < 0.1 {
		t.Errorf("the loudest sample is %v: the sound is silent", peak)
	}
	if l := d.Len(); l < int64(1900*soundRate/1000) || l > int64(2100*soundRate/1000) {
		t.Errorf("Len() is %d, want about %d", l, 2*soundRate)
	}
}

// TestSoundSeeksToAFrame checks that the sound can be started anywhere, which
// is a reader made again at that time.
func TestSoundSeeksToAFrame(t *testing.T) {
	s := openTest(t, clipPath)
	d := s.(soundSource).Sound()
	if d == nil {
		t.Fatal("the file's sound is nil")
	}
	if err := d.SetPosition(soundRate); err != nil { // one second in
		t.Fatal(err)
	}
	frames, peak := readSound(t, d, 0)
	if want := soundRate; frames < want-want/10 || frames > want+want/10 {
		t.Errorf("the sound from 1s is %d frames long, want about %d", frames, want)
	}
	if peak < 0.1 {
		t.Errorf("the loudest sample after seeking is %v: the sound is silent", peak)
	}
}

// readSound reads a decoder to its end, and returns how many frames it gave
// and how loud its loudest sample was.
func readSound(t *testing.T, d audio.Decoder, limit int) (frames int, peak float32) {
	t.Helper()
	buf := make([]float32, 2048*soundChannels)
	for i := 0; ; i++ {
		n, err := d.Read(buf)
		if n > 0 {
			frames += n
			for _, v := range buf[:n*soundChannels] {
				peak = max(peak, float32(math.Abs(float64(v))))
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, ErrEnd) {
				return frames, peak
			}
			t.Fatalf("reading the sound: %v", err)
		}
		if n == 0 {
			t.Fatal("reading the sound made no progress")
		}
		if limit > 0 && frames > limit {
			return frames, peak
		}
		if i > 10000 {
			t.Fatal("the sound never ended")
		}
	}
}

func TestAnAudioOnlyFileHasNoPicture(t *testing.T) {
	s := openTest(t, wavPath)
	tracks := s.(TrackSource).Tracks()
	if len(tracks) != 1 || tracks[0].Kind != TrackAudio {
		t.Fatalf("the file's tracks are %+v, want one sound track", tracks)
	}
	if _, err := s.NextFrame(context.Background()); !errors.Is(err, ErrEnd) {
		t.Errorf("a file with no picture gave %v, want the end of the source", err)
	}
	d := s.(soundSource).Sound()
	if d == nil {
		t.Fatal("the file's sound is nil")
	}
	frames, peak := readSound(t, d, 0)
	if want := soundRate; frames < want-want/10 || frames > want+want/10 {
		t.Errorf("the sound is %d frames long, want about %d", frames, want)
	}
	if peak < 0.1 {
		t.Errorf("the loudest sample is %v: the sound is silent", peak)
	}
}

// TestChoosingTracksTurnsThemOnAndOff checks the readers are made again for
// the tracks asked for: the fixture has one of each, so choosing none of one
// kind is how a file with several would choose another.
func TestChoosingTracksTurnsThemOnAndOff(t *testing.T) {
	s := openTest(t, clipPath)
	ts := s.(TrackSource)
	if len(ts.Tracks()) != 2 {
		t.Fatalf("the fixture has %+v, want a picture and a sound", ts.Tracks())
	}
	if err := ts.SelectTracks(-1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.NextFrame(context.Background()); !errors.Is(err, ErrEnd) {
		t.Errorf("with no picture chosen, reading gave %v, want the end of the source", err)
	}
	if s.(soundSource).Sound() == nil {
		t.Error("choosing no picture gave up the sound too")
	}
	if err := ts.SelectTracks(0, -1); err != nil {
		t.Fatal(err)
	}
	if s.(soundSource).Sound() != nil {
		t.Error("choosing no sound left a sound")
	}
	f, err := s.NextFrame(context.Background())
	if err != nil {
		t.Fatalf("with a picture chosen again: %v", err)
	}
	f.Release()
	if err := ts.SelectTracks(2, 0); err == nil {
		t.Error("choosing a track the file does not have worked")
	}
}

func TestAFileThatIsNotThereFails(t *testing.T) {
	s, err := openSource("testdata/nothing-here.mp4", options{})
	if err == nil {
		s.Close()
		t.Fatal("opening a file that is not there worked")
	}
	if errors.Is(err, ErrEnd) {
		t.Errorf("a file that is not there is not the end of a source: %v", err)
	}
	t.Logf("error: %v", err)
}

func TestPictureBuffersComeBackInTheOrderTheyWereFreed(t *testing.T) {
	var p pixPool
	a := p.get(4)[:1]
	b := p.get(4)[:1]
	p.put(a)
	p.put(b)
	if got := p.get(4)[:1]; &got[0] != &a[0] {
		t.Error("the pool gave back the last buffer freed, not the first")
	}
}
