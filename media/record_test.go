//go:build darwin || (linux && (amd64 || arm64)) || (windows && (amd64 || arm64))

package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A patternSource gives bright pictures that move, for testing what comes
// back out of an encoder: a still picture could decode right while the
// frames came out in the wrong order.
type patternSource struct {
	width, height int
	frames        int
	at            int
	fps           int
	// onFrame, when set, is called with the number of the frame about to
	// be given out, so that a test can stop the recording part way.
	onFrame func(i int)
	// failAt makes the source fail instead of giving that frame.
	failAt int
	closed bool
}

func newPatternSource(frames int) *patternSource {
	return &patternSource{width: 160, height: 120, frames: frames, fps: 30, failAt: -1}
}

func (s *patternSource) NextFrame(ctx context.Context) (*Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.at >= s.frames {
		return nil, ErrEnd
	}
	if s.at == s.failAt {
		return nil, errors.New("the source broke")
	}
	if s.onFrame != nil {
		s.onFrame(s.at)
	}
	pix := make([]byte, s.width*s.height*4)
	for y := 0; y < s.height; y++ {
		for x := 0; x < s.width; x++ {
			at := (y*s.width + x) * 4
			pix[at] = byte(40 + (s.at*8+x)%200)
			pix[at+1] = byte(40 + (y*2+s.at*4)%200)
			pix[at+2] = 220
			pix[at+3] = 255
		}
	}
	pts := time.Duration(s.at) * time.Second / time.Duration(s.fps)
	s.at++
	return NewFrame(PixelRGBA, s.width, s.height, []Plane{{Pix: pix, Stride: s.width * 4}}, pts, time.Second/time.Duration(s.fps), nil), nil
}

func (s *patternSource) Close() error {
	s.closed = true
	return nil
}

// pictureMean is the average of the picture's colours, so that a test can
// tell a recorded picture from a black one.
func pictureMean(t *testing.T, img []byte) (int, int, int) {
	t.Helper()
	var r, g, b int
	n := 0
	for i := 0; i+3 < len(img); i += 4 {
		r += int(img[i])
		g += int(img[i+1])
		b += int(img[i+2])
		n++
	}
	if n == 0 {
		return 0, 0, 0
	}
	return r / n, g / n, b / n
}

// recordPattern records frames of the test pattern and returns the path.
func recordPattern(t *testing.T, frames int, opts ...RecordOption) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recording.mp4")
	src := newPatternSource(frames)
	defer src.Close()
	if err := Record(context.Background(), path, src, opts...); err != nil {
		t.Fatalf("recording the test pattern: %v", err)
	}
	return path
}

// TestTheRecorderWritesAFileAPlayerCanOpen is the whole of recording in one
// test: pictures in, a file out, and the file plays as the pictures were
// given.
func TestTheRecorderWritesAFileAPlayerCanOpen(t *testing.T) {
	const frames = 30
	path := recordPattern(t, frames)

	player, err := Open(path, WithAudio(false))
	if err != nil {
		t.Fatalf("opening the recording: %v", err)
	}
	defer player.Close()
	tracks := player.Tracks()
	if len(tracks) != 1 {
		t.Fatalf("the recording has %d tracks, want 1", len(tracks))
	}
	if tracks[0].Kind != TrackVideo || tracks[0].Codec != CodecH264 {
		t.Fatalf("the recording's track is %v %v, want a video h264 track", tracks[0].Kind, tracks[0].Codec)
	}
	if tracks[0].Width != 160 || tracks[0].Height != 120 {
		t.Fatalf("the recording is %dx%d, want 160x120", tracks[0].Width, tracks[0].Height)
	}
	got := 0
	for {
		f, err := player.NextFrame(context.Background())
		if errors.Is(err, ErrEnd) {
			break
		}
		if err != nil {
			t.Fatalf("playing the recording back: %v", err)
		}
		if f.Width != 160 || f.Height != 120 {
			t.Fatalf("picture %d is %dx%d, want 160x120", got, f.Width, f.Height)
		}
		if f.PTS < 0 {
			t.Fatalf("picture %d is at %v", got, f.PTS)
		}
		got++
		f.Release()
	}
	if got == 0 {
		t.Fatal("the recording has no pictures")
	}
	// VideoToolbox can drop a picture when the encoder is behind, as it
	// is a real-time one; what it must not do is invent pictures.
	if got > frames {
		t.Fatalf("the recording has %d pictures, and %d were given", got, frames)
	}
	t.Logf("%d of %d pictures came back", got, frames)
}

// TestTheRecorderWritesThePicturesItWasGiven checks the pictures are really
// the ones the source had, not black ones or the wrong way up.
func TestTheRecorderWritesThePicturesItWasGiven(t *testing.T) {
	path := recordPattern(t, 10)
	player, err := Open(path, WithAudio(false))
	if err != nil {
		t.Fatalf("opening the recording: %v", err)
	}
	defer player.Close()
	f, err := player.NextFrame(context.Background())
	if err != nil {
		t.Fatalf("reading a picture back: %v", err)
	}
	defer f.Release()
	if f.Format != PixelRGBA {
		t.Fatalf("the picture came back as %s", f.Format)
	}
	img := f.Image()
	if img == nil {
		t.Fatal("the picture has no image")
	}
	r, g, b := pictureMean(t, img.Pix)
	if r < 20 || g < 20 || b < 100 {
		t.Fatalf("the picture came back as (%d, %d, %d), too dark for the one recorded", r, g, b)
	}
	if b < r || b < g {
		t.Fatalf("the picture came back as (%d, %d, %d): the blue of it was on top, so it is upside down or the colours are swapped", r, g, b)
	}
}

// TestTheRecorderSaysWhatItCannotEncode checks a codec this platform has no
// encoder for is named, rather than a file that only looks like a recording.
func TestTheRecorderSaysWhatItCannotEncode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.mp4")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rec, err := NewRecorder(file, WithRecordCodec(CodecVP9))
	if err != nil {
		t.Fatalf("making the recorder: %v", err)
	}
	defer rec.Close()
	src := newPatternSource(1)
	defer src.Close()
	f, err := src.NextFrame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Release()
	if err := rec.WriteFrame(f); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("recording VP9 gave %v, want an unsupported error", err)
	}
}

// TestTheRecorderSaysWhenThePicturesChangeSize checks the first picture
// fixes the size, rather than a recording of two sizes.
func TestTheRecorderSaysWhenThePicturesChangeSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.mp4")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rec, err := NewRecorder(file)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	src := newPatternSource(2)
	defer src.Close()
	first, err := src.NextFrame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	if err := rec.WriteFrame(first); err != nil {
		t.Fatalf("recording the first picture: %v", err)
	}
	other := NewFrame(PixelRGBA, 320, 240, []Plane{{Pix: make([]byte, 320*240*4), Stride: 320 * 4}}, time.Second/30, time.Second/30, nil)
	defer other.Release()
	if err := rec.WriteFrame(other); err == nil {
		t.Fatal("recording a picture of another size was allowed")
	}
}

// TestRecordingASourceStopsWhenAsked checks a recording that is cancelled
// stops at the picture it reached, and still leaves a file that plays.
func TestRecordingASourceStopsWhenAsked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.mp4")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := newPatternSource(1000)
	defer src.Close()
	src.onFrame = func(i int) {
		if i == 10 {
			cancel()
		}
	}
	if err := Record(ctx, path, src); err != nil {
		t.Fatalf("recording until cancelled: %v", err)
	}
	player, err := Open(path, WithAudio(false))
	if err != nil {
		t.Fatalf("opening the recording: %v", err)
	}
	defer player.Close()
	got := 0
	for {
		f, err := player.NextFrame(context.Background())
		if errors.Is(err, ErrEnd) {
			break
		}
		if err != nil {
			t.Fatalf("playing the recording back: %v", err)
		}
		got++
		f.Release()
	}
	if got == 0 {
		t.Fatal("a cancelled recording has no pictures")
	}
}

// TestRecordingSaysWhenTheSourceBreaks checks a failure is reported as it
// is, not as the end of the recording.
func TestRecordingSaysWhenTheSourceBreaks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.mp4")
	src := newPatternSource(1000)
	src.failAt = 5
	defer src.Close()
	err := Record(context.Background(), path, src)
	if err == nil {
		t.Fatal("a recording of a source that broke says it ended fine")
	}
	if errors.Is(err, ErrEnd) {
		t.Fatal("the source's failure was reported as the end of the source")
	}
	// The file was finished on the way out, so it plays up to the last
	// picture written.
	player, oerr := Open(path, WithAudio(false))
	if oerr != nil {
		t.Fatalf("opening the file a broken recording left: %v", oerr)
	}
	player.Close()
}
