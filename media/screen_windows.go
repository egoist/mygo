//go:build windows && (amd64 || arm64)

package media

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo/media/internal/windows"
)

// screenInterval is how often a screen is taken as a picture. Windows' GDI
// hands out the screen when asked rather than calling back when it changes, so
// a reader gets the screen at this rate.
const screenInterval = time.Second / 30

// screenTestCapture, when it is set, is what a screen's pictures come from
// instead of GDI: a test's way of putting pictures through the capture path on
// a session that has no desktop to copy.
var screenTestCapture func(w, h int, dst []byte) error

// screenTestSize is the size a screen test capture is.
var screenTestSize = [2]int{640, 480}

// listScreens lists the displays Windows draws on.
func listScreens() ([]ScreenInfo, error) {
	monitors, err := windows.Monitors()
	if err != nil {
		return nil, err
	}
	screens := make([]ScreenInfo, len(monitors))
	for i, monitor := range monitors {
		screens[i] = ScreenInfo{
			ID:     monitor.Name,
			Name:   fmt.Sprintf("Display %d", i+1),
			Width:  monitor.Width(),
			Height: monitor.Height(),
		}
	}
	return screens, nil
}

// gdiScreen takes a monitor's picture again and again, into the queue the
// caller reads.
type gdiScreen struct {
	monitor windows.Monitor
	q       *liveFrames
	pool    pixPool
	done    chan struct{}
	stop    chan struct{}

	mu     sync.Mutex
	closed bool
}

// openScreen starts capturing a display.
func openScreen(info ScreenInfo, q *liveFrames) (screenHandle, error) {
	var monitor windows.Monitor
	if screenTestCapture == nil {
		monitors, err := windows.Monitors()
		if err != nil {
			return nil, err
		}
		found := false
		for _, m := range monitors {
			if m.Name == info.ID {
				monitor, found = m, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("media: no screen with id %q", info.ID)
		}
	} else {
		monitor = windows.Monitor{Name: info.ID, Right: screenTestSize[0], Bottom: screenTestSize[1]}
	}
	s := &gdiScreen{monitor: monitor, q: q, done: make(chan struct{}), stop: make(chan struct{})}
	go s.run()
	return s, nil
}

// run takes the screen's picture until the screen is closed or breaks.
func (s *gdiScreen) run() {
	defer close(s.done)
	start := time.Now()
	for {
		if s.stopped() {
			return
		}
		w, h := s.monitor.Width(), s.monitor.Height()
		buf := s.pool.get(w * h * 4)
		err := error(nil)
		if screenTestCapture != nil {
			err = screenTestCapture(w, h, buf)
		} else {
			err = windows.CaptureMonitor(s.monitor, buf)
		}
		if err != nil {
			s.pool.put(buf)
			if errors.Is(err, windows.ErrNoDesktop) {
				s.q.fail(&MissingComponentError{Component: "an interactive desktop", Feature: FeatureScreenCapture, Hint: "screen capture needs a session logged in to a desktop, not a service or a bare remote shell"})
				return
			}
			s.q.fail(fmt.Errorf("media: capturing the screen: %w", err))
			return
		}
		// GDI's pixels are blue, green, red and alpha; the frame hands out red,
		// green, blue and alpha.
		for i := 0; i+3 < len(buf); i += 4 {
			buf[i], buf[i+2] = buf[i+2], buf[i]
		}
		f := NewFrame(PixelRGBA, w, h, []Plane{{Pix: buf, Stride: w * 4}}, time.Since(start), screenInterval, func() {
			s.pool.put(buf)
		})
		s.q.push(f)
		timer := time.NewTimer(screenInterval)
		select {
		case <-timer.C:
		case <-s.stop:
			timer.Stop()
			return
		}
	}
}

// stopped reports whether the screen is being closed.
func (s *gdiScreen) stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// close stops capturing and waits for the picture in flight.
func (s *gdiScreen) close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	close(s.stop)
	<-s.done
	return nil
}

// screenPermission is what Windows says about capturing the screen: nothing,
// as any app may take a picture of the desktop.
func screenPermission() PermissionStatus { return PermissionNotRequired }

// requestScreenPermission asks nobody: there is no permission to ask for.
func requestScreenPermission(ctx context.Context) (PermissionStatus, error) {
	return PermissionNotRequired, nil
}

// openScreenPermissionSettings has nowhere to send the caller.
func openScreenPermissionSettings() error { return nil }
