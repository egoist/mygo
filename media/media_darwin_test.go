//go:build darwin

package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/internal/wait"
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

// holdFirstRead makes the source's first read wait until release is called,
// and lets the reads after it go to the file. It is how a test keeps a decode
// in flight while it closes the source, or while it cancels the context that
// waits for the decode.
//
// entered is closed when the read has begun and is held, and finished when it
// has returned.
func holdFirstRead(t *testing.T, s *avSource) (entered, finished <-chan struct{}, release func()) {
	t.Helper()
	real := s.readSample
	if real == nil {
		t.Fatal("the source has no read to hold")
	}
	begin := make(chan struct{})
	unhold := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	s.readSample = func(out uintptr) uintptr {
		held := false
		once.Do(func() {
			held = true
			close(begin)
			<-unhold
		})
		sb := real(out)
		if held {
			close(done)
		}
		return sb
	}
	var onceRelease sync.Once
	release = func() { onceRelease.Do(func() { close(unhold) }) }
	return begin, done, release
}

// TestWithAudioOffLeavesTheSoundUndecoded checks the read gate of the source:
// a file opened with WithAudio(false) reads, and so decodes, no sound.
func TestWithAudioOffLeavesTheSoundUndecoded(t *testing.T) {
	s, err := openSource(clipPath, options{noSound: true})
	if err != nil {
		t.Fatalf("openSource: %v", err)
	}
	defer s.Close()
	ss, ok := s.(soundSource)
	if !ok {
		t.Fatalf("%T does not tell its sound", s)
	}
	if d := ss.Sound(); d != nil {
		t.Fatal("WithAudio(false) still decoded the sound")
	}
}

// TestNextFrameStopsWaitingWhenContextIsDone checks that a decode in flight
// does not keep NextFrame from noticing a canceled context, and that the
// sample it read is not lost: the next call takes it.
func TestNextFrameStopsWaitingWhenContextIsDone(t *testing.T) {
	s := openTest(t, clipPath).(*avSource)
	entered, finished, release := holdFirstRead(t, s)

	ctx, cancel := context.WithCancel(context.Background())
	waited := make(chan error, 1)
	go func() {
		_, err := s.NextFrame(ctx)
		waited <- err
	}()
	wait.For(t, "a read to begin", entered)
	cancel()
	if err := wait.For(t, "NextFrame to give up on a canceled context", waited); !errors.Is(err, context.Canceled) {
		t.Fatalf("NextFrame returned %v, want context.Canceled", err)
	}

	// The decode still runs on: the caller that gave up on it must not
	// disturb it, and its sample is the next picture.
	release()
	wait.For(t, "the held read to finish", finished)
	f, err := s.NextFrame(context.Background())
	if err != nil {
		t.Fatalf("the picture after a canceled read: %v", err)
	}
	defer f.Release()
	if f.Width != 160 || f.Height != 120 {
		t.Fatalf("the picture after a canceled read is %dx%d, want 160x120", f.Width, f.Height)
	}
}

// TestCloseWaitsForAHeldVideoRead checks Close's promise for a picture read:
// it gives up the source at once, but returns only once every native read has
// returned, so the read never uses a reader Close freed.
func TestCloseWaitsForAHeldVideoRead(t *testing.T) {
	s := openTest(t, clipPath)
	av := s.(*avSource)
	entered, finished, release := holdFirstRead(t, av)

	waited := make(chan error, 1)
	go func() {
		_, err := s.NextFrame(context.Background())
		waited <- err
	}()
	wait.For(t, "a read to begin", entered)

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	// Close gives up the source before it waits: the closed channel says it
	// has started, and the held read says it cannot have returned yet.
	wait.Until(t, "Close to give up the source", func() bool {
		select {
		case <-av.done:
			return true
		default:
			return false
		}
	})
	select {
	case err := <-closed:
		t.Fatalf("Close returned (%v) while a read was held", err)
	default:
	}

	release()
	wait.For(t, "the held read to finish", finished)
	if err := wait.For(t, "Close to return after the read", closed); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := wait.For(t, "NextFrame after Close", waited); !errors.Is(err, ErrEnd) {
		t.Fatalf("NextFrame after Close returned %v, want ErrEnd", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
}

// TestCloseWaitsForAHeldSoundRead checks the same promise for the sound: the
// sound read returns after Close, and only then does Close.
func TestCloseWaitsForAHeldSoundRead(t *testing.T) {
	s := openTest(t, clipPath).(*avSource)
	d := s.Sound().(*avSound)
	entered, finished, release := holdFirstRead(t, s)

	pulled := make(chan error, 1)
	go func() {
		_, err := d.pull()
		pulled <- err
	}()
	wait.For(t, "a sound read to begin", entered)

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	wait.Until(t, "Close to give up the file", func() bool {
		select {
		case <-s.done:
			return true
		default:
			return false
		}
	})
	select {
	case err := <-closed:
		t.Fatalf("Close returned (%v) while a sound read was held", err)
	default:
	}

	release()
	wait.For(t, "the held sound read to finish", finished)
	if err := wait.For(t, "Close to return after the sound read", closed); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := wait.For(t, "the sound read after Close", pulled); !errors.Is(err, ErrEnd) {
		t.Fatalf("the sound read after Close returned %v, want ErrEnd", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
}

// TestNextFramePullsOneSampleAtATime checks that concurrent callers get a
// picture each rather than sharing one read and taking its sample twice.
func TestNextFramePullsOneSampleAtATime(t *testing.T) {
	s := openTest(t, clipPath)
	frames := make(chan *Frame, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			f, err := s.NextFrame(context.Background())
			frames <- f
			errs <- err
		}()
	}
	var got []*Frame
	for i := 0; i < 2; i++ {
		f := wait.For(t, "a picture from a caller", frames)
		if err := wait.For(t, "its error", errs); err != nil {
			t.Fatalf("NextFrame: %v", err)
		}
		if f == nil {
			t.Fatal("a caller got no picture and no error")
		}
		defer f.Release()
		got = append(got, f)
	}
	if got[0].PTS == got[1].PTS {
		t.Fatalf("both callers got the picture at %v", got[0].PTS)
	}
}

// TestSeeksDuringAHeldReadStartNoNewReads checks that a seek or a track change
// while a read is held does not start another one: the held read is waited
// for and its sample given up, so repeated seeks cannot pile up goroutines.
func TestSeeksDuringAHeldReadStartNoNewReads(t *testing.T) {
	s := openTest(t, clipPath).(*avSource)
	real := s.readSample
	var calls atomic.Int32
	entered := make(chan struct{})
	unhold := make(chan struct{})
	var once sync.Once
	s.readSample = func(out uintptr) uintptr {
		calls.Add(1)
		once.Do(func() {
			close(entered)
			<-unhold
		})
		return real(out)
	}

	type result struct {
		f   *Frame
		err error
	}
	results := make(chan result, 1)
	go func() {
		f, err := s.NextFrame(context.Background())
		results <- result{f, err}
	}()
	wait.For(t, "a read to begin", entered)
	for i := 0; i < 8; i++ {
		if err := s.Seek(time.Duration(300+i*100) * time.Millisecond); err != nil {
			t.Fatalf("Seek: %v", err)
		}
		if err := s.SelectTracks(0, 1); err != nil {
			t.Fatalf("SelectTracks: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("%d reads started while one was held, want 1", got)
	}

	close(unhold)
	r := wait.For(t, "the picture after the seeks", results)
	if r.err != nil {
		t.Fatalf("NextFrame after the seeks: %v", r.err)
	}
	defer r.f.Release()
	if got := calls.Load(); got != 2 {
		t.Fatalf("%d reads, want a second one for the generation the seeks made", got)
	}
	if r.f.PTS < time.Second {
		t.Fatalf("the picture is at %v, want the last seek's second", r.f.PTS)
	}
}

// TestAnHTTPFilePlays checks the whole http path of the source: the loader
// serves the container, and the source decodes its pictures and sound and
// seeks it, as it does a file.
func TestAnHTTPFilePlays(t *testing.T) {
	body := clipBytes(t)
	srv := serveClip(t, "clip.mp4", body)
	s, err := openSource(srv.URL+"/clip.mp4", options{})
	if err != nil {
		t.Fatalf("openSource: %v", err)
	}
	defer s.Close()
	av := s.(*avSource)
	tracks := av.Tracks()
	if len(tracks) != 2 || tracks[0].Kind != TrackVideo || tracks[1].Kind != TrackAudio {
		t.Fatalf("tracks = %+v", tracks)
	}
	first, err := s.NextFrame(context.Background())
	if err != nil {
		t.Fatalf("the first picture: %v", err)
	}
	defer first.Release()
	second, err := s.NextFrame(context.Background())
	if err != nil {
		t.Fatalf("the second picture: %v", err)
	}
	defer second.Release()
	if second.PTS <= first.PTS {
		t.Fatalf("the pictures are at %v and %v, not in order", first.PTS, second.PTS)
	}
	d := s.(soundSource).Sound()
	if d == nil {
		t.Fatal("the http file has no sound decoder")
	}
	buf := make([]float32, 2000)
	if n, err := d.Read(buf); err != nil || n == 0 {
		t.Fatalf("the sound: %d frames, %v", n, err)
	}
	if err := s.(Seekable).Seek(time.Second); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	after, err := s.NextFrame(context.Background())
	if err != nil {
		t.Fatalf("the picture after the seek: %v", err)
	}
	defer after.Release()
	if after.PTS < time.Second {
		t.Fatalf("the picture after the seek is at %v", after.PTS)
	}
}

// TestCloseStopsAVideoReadWaitingForTheOrigin checks the cancellation the
// loader exists for: a native read blocked on an origin that has stopped
// sending returns when Close fails its loading requests, and Close waits for
// it before freeing the readers.
func TestCloseStopsAVideoReadWaitingForTheOrigin(t *testing.T) {
	fast, prefix := moovFirst(t, clipBytes(t))
	origin := newStalledOrigin(t, fast, prefix, "video/mp4")
	s, err := openSource(origin.srv.URL+"/clip.mp4", options{})
	if err != nil {
		t.Fatalf("opening the http file: %v", err)
	}
	waited := make(chan error, 1)
	go func() {
		_, err := s.NextFrame(context.Background())
		waited <- err
	}()
	wait.For(t, "the origin to hold what the read waits for", origin.held)

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	wait.For(t, "the loader to fail the loading request", origin.aborted)
	// Close must return without the origin ever sending the rest: the
	// failed loading request is what the native read waiting on it
	// returns from.
	if err := wait.For(t, "Close to return while the origin is still stalled", closed); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := wait.For(t, "NextFrame after Close", waited); !errors.Is(err, ErrEnd) {
		t.Fatalf("NextFrame after Close returned %v, want ErrEnd", err)
	}
	origin.release()
}

// TestCloseStopsASoundReadWaitingForTheOrigin checks the same cancellation
// for the sound's reader.
func TestCloseStopsASoundReadWaitingForTheOrigin(t *testing.T) {
	fast, prefix := moovFirst(t, clipBytes(t))
	origin := newStalledOrigin(t, fast, prefix, "video/mp4")
	s, err := openSource(origin.srv.URL+"/clip.mp4", options{noVideo: true})
	if err != nil {
		t.Fatalf("opening the http file: %v", err)
	}
	d := s.(soundSource).Sound()
	if d == nil {
		t.Fatal("the http file has no sound decoder")
	}
	pulled := make(chan error, 1)
	go func() {
		_, err := d.Read(make([]float32, 2000))
		pulled <- err
	}()
	wait.For(t, "the origin to hold what the sound read waits for", origin.held)

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	wait.For(t, "the loader to fail the loading request", origin.aborted)
	if err := wait.For(t, "Close to return while the origin is still stalled", closed); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := wait.For(t, "the sound read after Close", pulled); err == nil {
		t.Fatal("the sound read after Close decoded on")
	} else if !errors.Is(err, ErrEnd) && !errors.Is(err, io.EOF) {
		t.Logf("the sound read after Close returned %v", err)
	}
	origin.release()
}

// TestHTTPProblemsAreReported checks that an origin this version cannot serve
// fails the open with why, rather than looking like a file without tracks.
func TestHTTPProblemsAreReported(t *testing.T) {
	t.Run("a 404", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(srv.Close)
		s, err := openSource(srv.URL+"/clip.mp4", options{})
		if err == nil {
			s.Close()
			t.Fatal("opening a missing http file worked")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("error = %v, want the 404", err)
		}
	})
	t.Run("an origin that does not say how long the file is", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Write(clipBytes(t))
			w.(http.Flusher).Flush() // chunked, so no Content-Length
		}))
		t.Cleanup(srv.Close)
		s, err := openSource(srv.URL+"/clip.mp4", options{})
		if err == nil {
			s.Close()
			t.Fatal("opening an unknown-length http file worked")
		}
		if !strings.Contains(err.Error(), "does not say how long") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("a scheme it cannot read", func(t *testing.T) {
		if s, err := openSource("rtsp://127.0.0.1:1/stream", options{}); err == nil {
			s.Close()
			t.Fatal("opening an rtsp source worked")
		}
	})
}

// TestSourceURLsBySchemeCase checks that the scheme is what decides how a
// source is read, whatever its case, and that a scheme this version cannot
// read fails as unsupported instead of loading around the loader.
func TestSourceURLsBySchemeCase(t *testing.T) {
	body := clipBytes(t)
	srv := serveClip(t, "clip.mp4", body)
	t.Run("http in any case", func(t *testing.T) {
		upper := strings.Replace(srv.URL, "http://", "HTTP://", 1)
		s, err := openSource(upper+"/clip.mp4", options{})
		if err != nil {
			t.Fatalf("openSource: %v", err)
		}
		defer s.Close()
		f, err := s.NextFrame(context.Background())
		if err != nil {
			t.Fatalf("NextFrame: %v", err)
		}
		f.Release()
	})
	t.Run("a file URL", func(t *testing.T) {
		abs, err := filepath.Abs(clipPath)
		if err != nil {
			t.Fatal(err)
		}
		s, err := openSource("file://"+abs, options{})
		if err != nil {
			t.Fatalf("openSource: %v", err)
		}
		defer s.Close()
		f, err := s.NextFrame(context.Background())
		if err != nil {
			t.Fatalf("NextFrame: %v", err)
		}
		f.Release()
	})
	t.Run("another scheme", func(t *testing.T) {
		s, err := openSource("ftp://127.0.0.1:1/x.mp4", options{})
		if err == nil {
			s.Close()
			t.Fatal("opening an ftp source worked")
		}
		if !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("error = %v, want an unsupported one", err)
		}
	})
}

// clipBytes is the video fixture's bytes.
func clipBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(clipPath)
	if err != nil {
		t.Fatalf("read %s: %v", clipPath, err)
	}
	return b
}

// serveClip serves an asset with the usual HTTP range support.
func serveClip(t *testing.T, name string, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A stalledOrigin serves an asset only up to prefix bytes of its body: the
// answer that reaches past it waits, which is what a reader blocked on the
// network waits on, until the test closes finish, and says when a request was
// canceled instead.
type stalledOrigin struct {
	body       []byte
	prefix     int
	contentTyp string
	srv        *httptest.Server

	held    chan struct{} // closed when an answer first waits past the prefix
	aborted chan struct{} // closed when a waiting answer's request was canceled
	finish  chan struct{} // closed by the test to finish the waiting answers

	heldOnce, abortedOnce, finishOnce sync.Once
}

func newStalledOrigin(t *testing.T, body []byte, prefix int, contentType string) *stalledOrigin {
	t.Helper()
	o := &stalledOrigin{
		body:       body,
		prefix:     prefix,
		contentTyp: contentType,
		held:       make(chan struct{}),
		aborted:    make(chan struct{}),
		finish:     make(chan struct{}),
	}
	o.srv = httptest.NewServer(o)
	t.Cleanup(func() {
		o.release()
		o.srv.Close()
	})
	return o
}

// release lets every answer that is waiting on the test finish.
func (o *stalledOrigin) release() {
	o.finishOnce.Do(func() { close(o.finish) })
}

func (o *stalledOrigin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	total := int64(len(o.body))
	start, end := int64(0), total-1
	if v := strings.TrimPrefix(r.Header.Get("Range"), "bytes="); v != r.Header.Get("Range") {
		dash := strings.IndexByte(v, '-')
		if dash < 0 {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		start, _ = strconv.ParseInt(v[:dash], 10, 64)
		if rest := v[dash+1:]; rest != "" {
			end, _ = strconv.ParseInt(rest, 10, 64)
			if end >= total {
				end = total - 1
			}
		}
	}
	if start >= total {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Type", o.contentTyp)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	upTo := int64(o.prefix)
	if upTo > end+1 {
		upTo = end + 1
	}
	if upTo > start {
		w.Write(o.body[start:upTo])
	}
	w.(http.Flusher).Flush()
	if end+1 <= int64(o.prefix) {
		return // this answer asked for no more than the prefix
	}
	o.heldOnce.Do(func() { close(o.held) })
	select {
	case <-r.Context().Done():
		o.abortedOnce.Do(func() { close(o.aborted) })
		<-o.finish
	case <-o.finish:
		if upTo <= end {
			w.Write(o.body[upTo : end+1])
		}
	}
}

// moovFirst rewrites the fixture with its moov box before mdat, so that its
// tracks can be read from the first bytes of an http answer, and returns it
// with how many of its leading bytes an answer needs before the first sample.
func moovFirst(t *testing.T, data []byte) ([]byte, int) {
	t.Helper()
	type box struct {
		typ        string
		start, end int
	}
	var boxes []box
	for i := 0; i+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[i:]))
		if size < 8 || i+size > len(data) {
			t.Fatalf("malformed box at %d of %d", i, len(data))
		}
		boxes = append(boxes, box{string(data[i+4 : i+8]), i, i + size})
		i += size
	}
	var ftyp, moov *box
	var others []box
	for i := range boxes {
		switch boxes[i].typ {
		case "ftyp":
			ftyp = &boxes[i]
		case "moov":
			moov = &boxes[i]
		default:
			others = append(others, boxes[i])
		}
	}
	if ftyp == nil || moov == nil {
		t.Fatal("the fixture has no ftyp or no moov")
	}
	// moov goes right after ftyp, so every box after it moves by its size:
	// patch the chunk offsets that say where the samples are.
	mv := append([]byte(nil), data[moov.start:moov.end]...)
	delta := uint32(len(mv))
	for i := 0; i+12 < len(mv); {
		idx := bytes.Index(mv[i:], []byte("stco"))
		if idx < 0 {
			break
		}
		at := i + idx
		sizeAt := at - 4
		if sizeAt < 0 || at+12 > len(mv) {
			break
		}
		size := int(binary.BigEndian.Uint32(mv[sizeAt:]))
		count := int(binary.BigEndian.Uint32(mv[at+8:]))
		entries := at + 12
		if size != 12+count*4 || entries+count*4 > len(mv) {
			break
		}
		for j := 0; j < count; j++ {
			off := entries + j*4
			v := binary.BigEndian.Uint32(mv[off:])
			binary.BigEndian.PutUint32(mv[off:], v+delta)
		}
		i = entries + count*4
	}
	out := append([]byte(nil), data[ftyp.start:ftyp.end]...)
	out = append(out, mv...)
	for _, b := range others {
		out = append(out, data[b.start:b.end]...)
	}
	// The first sample comes after every box before mdat, and mdat's header.
	pos := (ftyp.end - ftyp.start) + len(mv)
	prefix := len(out)
	for _, b := range others {
		if b.typ == "mdat" {
			prefix = pos + 8
			break
		}
		pos += b.end - b.start
	}
	return out, prefix
}
