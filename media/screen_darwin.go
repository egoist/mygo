//go:build darwin

package media

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/media/internal/darwin"
)

// listScreens lists the displays ScreenCaptureKit offers.
func listScreens() ([]ScreenInfo, error) {
	displays, err := darwin.ScreenDisplays()
	if err != nil {
		if screenPermission() != PermissionGranted {
			// macOS guards the list as well: an app that may not record the
			// screen is told it may not, rather than being told there are no
			// screens.
			return nil, fmt.Errorf("media: macOS shares no screens: %w", ErrPermissionDenied)
		}
		if screenErr := darwin.LoadScreenCapture(); screenErr != nil {
			return nil, &MissingComponentError{Component: "ScreenCaptureKit", Feature: FeatureScreenCapture, Hint: "it needs macOS 12.3 or later"}
		}
		return nil, err
	}
	screens := make([]ScreenInfo, len(displays))
	for i, display := range displays {
		screens[i] = ScreenInfo{
			ID:     strconv.FormatUint(uint64(display.ID), 10),
			Name:   fmt.Sprintf("Display %d", i+1),
			Width:  display.Width,
			Height: display.Height,
		}
	}
	return screens, nil
}

// sckScreen turns the pictures a ScreenCaptureKit stream hands over into
// frames.
type sckScreen struct {
	cap  *darwin.ScreenCapture
	q    *liveFrames
	pool pixPool

	// The system stamps a screen's pictures with the machine's own clock: the
	// first picture's stamp moves the rest to the start of the capture, which
	// is where a frame's PTS counts from.
	first     time.Duration
	haveFirst bool
}

// openScreen starts capturing a display.
func openScreen(info ScreenInfo, q *liveFrames) (screenHandle, error) {
	if screenPermission() == PermissionDenied {
		return nil, fmt.Errorf("media: capturing the screen %q: %w", info.Name, ErrPermissionDenied)
	}
	displayID, err := strconv.ParseUint(info.ID, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("media: no screen with id %q", info.ID)
	}
	s := &sckScreen{q: q}
	cap, err := darwin.OpenScreen(uint32(displayID), s.deliver, func(errRef uintptr) {
		q.fail(fmt.Errorf("media: the screen stopped being shared: %s", darwin.ErrorString(errRef)))
	})
	if err != nil {
		if !darwin.ScreenAccessAllowed() {
			return nil, fmt.Errorf("media: capturing the screen %q: %w", info.Name, ErrPermissionDenied)
		}
		return nil, err
	}
	s.cap = cap
	return s, nil
}

func (s *sckScreen) close() error { return s.cap.Close() }

// deliver takes one picture of the screen: it copies the picture into a frame
// this package hands out and returns before the next one. It runs on the
// capture queue, where the sample buffer lives only until it returns.
func (s *sckScreen) deliver(sample uintptr) {
	picture := darwin.SampleImage(sample)
	if picture == 0 {
		return
	}
	if code := darwin.PixelLock(picture); code != 0 {
		return
	}
	defer darwin.PixelUnlock(picture)
	w, h := darwin.PixelSize(picture)
	if w <= 0 || h <= 0 {
		return
	}
	buf := s.pool.get(w * h * 4)
	if err := copyPicture(buf, darwin.PixelBase(picture), w, h, darwin.PixelStride(picture), darwin.PixelFormat(picture)); err != nil {
		s.pool.put(buf)
		return
	}
	pts := darwin.SamplePTS(sample)
	if !s.haveFirst {
		s.first, s.haveFirst = pts, true
	}
	s.q.push(NewFrame(PixelRGBA, w, h, []Plane{{Pix: buf, Stride: w * 4}}, pts-s.first, darwin.SampleDuration(sample), func() {
		s.pool.put(buf)
	}))
}

// screenPermission is what macOS says about this app recording the screen.
func screenPermission() PermissionStatus {
	if err := darwin.LoadScreenCapture(); err != nil {
		return PermissionNotDetermined
	}
	if darwin.ScreenAccessAllowed() {
		return PermissionGranted
	}
	// macOS does not say whether it was never asked or refused: both are "not
	// allowed" until the person answers in System Settings.
	return PermissionNotDetermined
}

// requestScreenPermission asks the person, and reports the answer.
func requestScreenPermission(ctx context.Context) (PermissionStatus, error) {
	if err := darwin.LoadScreenCapture(); err != nil {
		return PermissionNotDetermined, err
	}
	if status := screenPermission(); status != PermissionNotDetermined {
		return status, nil
	}
	answer := make(chan bool, 1)
	go func() {
		answer <- darwin.RequestScreenAccess()
	}()
	select {
	case <-answer:
		return screenPermission(), nil
	case <-ctx.Done():
		return screenPermission(), ctx.Err()
	}
}

// openScreenPermissionSettings shows where the person can change the answer.
func openScreenPermissionSettings() error { return darwin.OpenScreenSettings() }
