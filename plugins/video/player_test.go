package video

import (
	"errors"
	"github.com/egoist/mygo/ui"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"
)

type fakeBackend struct {
	mu       sync.Mutex
	commands [][]string
	events   chan event
	wake     chan struct{}
	closed   int
	err      error
}

func (f *fakeBackend) command(_ uint64, args []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, append([]string(nil), args...))
	return f.err
}
func (f *fakeBackend) next() (event, bool) {
	select {
	case e := <-f.events:
		return e, true
	default:
		return event{}, false
	}
}
func (f *fakeBackend) render(w, h int) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
		img.Pix[i+3] = 255
	}
	return img, nil
}
func (f *fakeBackend) close()        { f.mu.Lock(); f.closed++; f.mu.Unlock() }
func (f *fakeBackend) emit(ev event) { f.events <- ev; signal(f.wake) }
func fakePlayer() (*Player, *fakeBackend) {
	p := makePlayer(Options{MaxWidth: 640, MaxHeight: 360})
	f := &fakeBackend{events: make(chan event, 64), wake: p.wake}
	p.native = f
	p.start()
	return p, f
}
func waitPhase(t *testing.T, p *Player, phase Phase) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.State().Phase == phase {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("want %s, state %+v", phase, p.State())
}
func TestControlsStateAndErrors(t *testing.T) {
	p, f := fakePlayer()
	defer p.Close()
	if err := p.Load("video.mp4"); err != nil {
		t.Fatal(err)
	}
	if err := p.Load("next.mp4"); !errors.Is(err, ErrLoading) {
		t.Fatal(err)
	}
	f.emit(event{kind: 6, entry: 10})
	f.emit(event{kind: 8})
	waitPhase(t, p, Paused)
	f.emit(event{kind: 22, name: "seekable", flag: true})
	f.emit(event{kind: 22, name: "duration", number: 20})
	waitPhase(t, p, Paused)
	p.apply(event{kind: 22, name: "seekable", flag: true})
	p.apply(event{kind: 22, name: "duration", number: 20})
	if err := p.Play(); err != nil {
		t.Fatal(err)
	}
	f.emit(event{kind: 22, name: "pause", flag: false})
	waitPhase(t, p, Playing)
	if err := p.Seek(40 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := p.SetVolume(55); err != nil {
		t.Fatal(err)
	}
	if err := p.SetMuted(true); err != nil {
		t.Fatal(err)
	}
	if err := p.SetRate(1.5); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	commands := append([][]string(nil), f.commands...)
	f.mu.Unlock()
	want := []string{"seek", "20.000000", "absolute+exact"}
	if !reflect.DeepEqual(commands[3], want) {
		t.Fatal(commands)
	}
	if err := p.SetRate(math.NaN()); err == nil {
		t.Fatal("NaN rate")
	}
	if err := p.SetVolume(101); err == nil {
		t.Fatal("excess volume")
	}
	f.emit(event{kind: 7, entry: 10, err: errors.New("bad codec")})
	waitPhase(t, p, Failed)
	if p.State().Err == nil {
		t.Fatal("missing source failure")
	}
}
func TestLoadFailureAndReplacedFileEvents(t *testing.T) {
	p, f := fakePlayer()
	defer p.Close()
	_ = p.Load("bad")
	f.emit(event{kind: 6, entry: 3})
	f.emit(event{kind: 7, entry: 3, err: errors.New("load failed")})
	waitPhase(t, p, Failed)
	_ = p.Load("good")
	p.apply(event{kind: 7, entry: 3, reason: 2})
	if p.State().Phase != Loading {
		t.Fatal("old file replaced loading state")
	}
	f.emit(event{kind: 6, entry: 4})
	f.emit(event{kind: 8})
	waitPhase(t, p, Paused)
	p.apply(event{kind: 7, entry: 3, err: errors.New("old error")})
	if p.State().Phase != Paused {
		t.Fatal("stale end file applied")
	}
	p.apply(event{kind: 22, name: "eof-reached", flag: true})
	if p.State().Phase != Ended {
		t.Fatal(p.State())
	}
}
func TestCommandFailureAndCloseLifetime(t *testing.T) {
	p, f := fakePlayer()
	f.err = errors.New("command rejected")
	if err := p.Load("bad"); err == nil || p.State().Phase != Idle {
		t.Fatal(err, p.State())
	}
	f.err = nil
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			for j := 0; j < 100; j++ {
				_ = p.Pause()
				_ = p.State()
			}
		})
	}
	p.Close()
	p.Close()
	wg.Wait()
	select {
	case <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("cleanup stuck")
	}
	if f.closed != 1 {
		t.Fatal(f.closed)
	}
	if err := p.Play(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if p.Capabilities().Playback {
		t.Fatal("closed player reported playback")
	}
}
func TestViewRenderingAndControls(t *testing.T) {
	p, f := fakePlayer()
	defer p.Close()
	p.apply(event{kind: 6, entry: 1})
	p.apply(event{kind: 8})
	tt := ui.NewTester(func(c *ui.Context) { View(c, p).Fill() }, 500, 350)
	signal(p.renderWake)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		ready := p.bitmap != nil
		p.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(time.Millisecond)
	}
	tt.Frame()
	if got := tt.Image().RGBAAt(100, 100); got != (color.RGBA{255, 0, 0, 255}) {
		t.Fatal(got)
	}
	if err := tt.Click("Play"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	last := f.commands[len(f.commands)-1]
	f.mu.Unlock()
	if !reflect.DeepEqual(last, []string{"set", "pause", "no"}) {
		t.Fatal(last)
	}
}
func TestABIAndMissingLibrary(t *testing.T) {
	if unsafe.Sizeof(nativeEvent{}) != 24 || unsafe.Sizeof(property{}) != 24 || unsafe.Sizeof(renderParam{}) != 16 {
		t.Fatal("native ABI sizes")
	}
	if _, err := New(Options{Library: "/missing/mygo-libmpv"}); err == nil {
		t.Fatal("missing library loaded")
	}
	if _, err := New(Options{MaxWidth: -1}); err == nil {
		t.Fatal("invalid render bounds")
	}
}
func TestNativeMPV(t *testing.T) {
	path := os.Getenv("MYGO_MPV_LIBRARY")
	if path == "" {
		t.Skip("set MYGO_MPV_LIBRARY for libmpv integration")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required to generate test video")
	}
	file := filepath.Join(t.TempDir(), "test.mp4")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=160x90:d=2", "-c:v", "mpeg4", "-y", file).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	p, err := New(Options{Library: path, Silent: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Close()
		select {
		case <-p.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("native cleanup stuck")
		}
	}()
	if err = p.Load(file); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, p, Paused)
	if err = p.Play(); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, p, Playing)
	deadline := time.Now().Add(2 * time.Second)
	drawn := false
	for time.Now().Before(deadline) {
		p.mu.Lock()
		drawn = p.bitmap != nil
		p.mu.Unlock()
		if drawn && p.State().Position > 0 {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	if !drawn || p.State().Position == 0 {
		t.Fatal("video did not produce frames/time", p.State())
	}
	if err = p.Pause(); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, p, Paused)
	if !p.State().Seekable {
		t.Fatal("local video not seekable")
	}
	if err = p.Seek(time.Second); err != nil {
		t.Fatal(err)
	}
	if err = p.Stop(); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, p, Idle)
	if err = p.Load(filepath.Join(t.TempDir(), "missing.mp4")); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, p, Failed)
	if p.State().Err == nil {
		t.Fatal("missing-file error not reported")
	}
}

type blockedRenderer struct {
	*fakeBackend
	entered, release chan struct{}
}

func (b *blockedRenderer) render(w, h int) (*image.RGBA, error) {
	close(b.entered)
	<-b.release
	return b.fakeBackend.render(w, h)
}
func TestCloseWhileRendering(t *testing.T) {
	p := makePlayer(Options{MaxWidth: 640, MaxHeight: 360})
	f := &fakeBackend{events: make(chan event, 1), wake: p.wake}
	b := &blockedRenderer{f, make(chan struct{}), make(chan struct{})}
	p.native = b
	p.start()
	signal(p.renderWake)
	<-b.entered
	before := time.Now()
	p.Close()
	if time.Since(before) > 50*time.Millisecond {
		t.Fatal("Close waited for a rendering thread")
	}
	select {
	case <-p.Done():
		t.Fatal("destroyed engine during rendering")
	default:
	}
	close(b.release)
	select {
	case <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("render worker leaked")
	}
	if f.closed != 1 {
		t.Fatal(f.closed)
	}
}
func TestAsynchronousControlErrorsAndQueueOverflow(t *testing.T) {
	p, _ := fakePlayer()
	defer p.Close()
	_ = p.Pause()
	p.apply(event{kind: 5, reply: 1, err: errors.New("async failure")})
	if p.State().Err == nil || p.State().Phase != Idle {
		t.Fatal(p.State())
	}
	_ = p.Load("source")
	p.apply(event{kind: 24})
	if p.State().Phase != Failed || p.loading || len(p.pending) != 0 {
		t.Fatal("overflow left pending work", p.State())
	}
	if err := p.Load("recovery"); err != nil {
		t.Fatal(err)
	}
}

func TestUnavailableSourceProperties(t *testing.T) {
	p, _ := fakePlayer()
	defer p.Close()
	p.apply(event{kind: 22, name: "duration", number: 42})
	p.apply(event{kind: 22, name: "duration", unavailable: true})
	p.apply(event{kind: 22, name: "speed", unavailable: true})
	if p.State().Duration != 0 || p.State().Rate != 1 {
		t.Fatal(p.State())
	}
}
