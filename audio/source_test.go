package audio

import (
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/egoist/mygo/internal/wait"
)

func waitStreamFilled(t *testing.T, s *stream) {
	t.Helper()
	wait.Until(t, "the stream to fill its ring", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.ring)-s.n < decodeFrames*s.keep
	})
}

type streamRateDecoder struct {
	genDecoder
	rate int
}

func (d *streamRateDecoder) SampleRate() int { return d.rate }

// Repeated refills must preserve every frame, including across the ring's wrap.
func TestStreamRefills(t *testing.T) {
	for _, tc := range []struct{ ch, rate int }{{1, sampleRate}, {2, sampleRate}, {6, sampleRate}, {1, 1000}} {
		t.Run(fmt.Sprintf("%dch-%dHz", tc.ch, tc.rate), func(t *testing.T) {
			d := &streamRateDecoder{genDecoder: genDecoder{ch: tc.ch, length: 10 * sampleRate}, rate: tc.rate}
			s := newStream(d)
			defer s.close()
			waitStreamFilled(t, s)
			out := make([]float32, 128*s.keep)
			for pos := 0; pos < 3*sampleRate; pos += 128 {
				wait.Until(t, "frames to read", func() bool {
					s.mu.Lock()
					defer s.mu.Unlock()
					return s.n >= len(out)
				})
				n, ended := s.read(out)
				if n != 128 || ended {
					t.Fatalf("read = %d, %v", n, ended)
				}
				for i, got := range out {
					want := float32((pos+i/s.keep)%1000)/1000 + float32(i%s.keep)
					if got != want {
						t.Fatalf("frame %d channel %d = %v, want %v", pos+i/s.keep, i%s.keep, got, want)
					}
				}
			}
		})
	}
}

// Observe notifications with an ordinary Cond waiter, without replacing its
// Locker or counting decoder Reads (which miss wakeups that do no decoding).
func TestStreamReadSignalsAtWaterline(t *testing.T) {
	for _, keep := range []int{1, 2} {
		t.Run(fmt.Sprint(keep), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const frames = 48000
				s := &stream{keep: keep, ring: make([]float32, frames*keep), n: frames * keep, seekTo: -1}
				s.cond.L = &s.mu
				woke := make(chan struct{})
				go func() {
					s.mu.Lock()
					s.cond.Wait()
					s.mu.Unlock()
					close(woke)
				}()
				// Always release the waiter, including on a failed assertion.
				defer func() {
					s.mu.Lock()
					s.cond.Broadcast()
					s.mu.Unlock()
				}()
				synctest.Wait()
				out := make([]float32, 1000*keep)
				for read := 1; read <= 12; read++ {
					s.read(out)
					synctest.Wait()
					select {
					case <-woke:
						if read < 12 {
							t.Fatalf("signaled after %d frames, before a quarter was free", read*1000)
						}
					default:
						if read == 12 {
							t.Fatal("did not signal when a quarter became free")
						}
					}
				}
			})
		})
	}
}

func TestStreamSeekWhileFilled(t *testing.T) {
	d := &genDecoder{ch: 1, length: 10 * sampleRate}
	s := newStream(d)
	defer s.close()
	waitStreamFilled(t, s)
	s.seek(12345)
	waitStreamFilled(t, s)
	out := make([]float32, 1)
	if n, ended := s.read(out); n != 1 || ended || out[0] != 0.345 {
		t.Fatalf("read after seek = %d, %v, %v", n, ended, out)
	}
}

func TestStreamCloseWhileFilled(t *testing.T) {
	d := &genDecoder{ch: 1, length: 10 * sampleRate}
	s := newStream(d)
	t.Cleanup(s.close)
	waitStreamFilled(t, s)
	s.close()
	wait.Until(t, "the decoder to close", d.closed.Load)
}

func TestStreamLoopAfterEOF(t *testing.T) {
	d := &genDecoder{ch: 1, length: 3000}
	s := newStream(d)
	defer s.close()
	wait.Until(t, "the stream's EOF", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.atEOF
	})
	out := make([]float32, 3000)
	s.read(out)
	s.setLoop(true, 0, 0)
	waitStreamFilled(t, s)
	if n, ended := s.read(out); n != len(out) || ended {
		t.Fatalf("read after enabling loop = %d, %v", n, ended)
	}
	for i, got := range out {
		if want := float32(i%1000) / 1000; got != want {
			t.Fatalf("frame %d = %v, want %v", i, got, want)
		}
	}
}

func TestMixDoesNotAllocate(t *testing.T) {
	setup(t)
	p := NewPlayer(&genDecoder{ch: 2, length: 1 << 40})
	defer p.Close()
	waitStreamFilled(t, p.v.src.(*stream))
	p.Play()
	sound := NewSound(22050, 1, ramp(sampleRate/2)).Play()
	out := make([]float32, 512*channels)
	if n := testing.AllocsPerRun(200, func() { mix(out) }); n != 0 {
		t.Errorf("mix allocated %v times a call", n)
	}
	if sound.Playing() {
		t.Error("the sound still plays after its end")
	}
}
