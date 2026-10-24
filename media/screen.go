package media

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNoScreen says that the system offers no screen to capture.
var ErrNoScreen = errors.New("media: no screen")

// ScreenInfo describes a screen the system offers for capture.
type ScreenInfo struct {
	// ID identifies the screen to the system: a display id on macOS, a
	// display's name elsewhere.
	ID string
	// Name is what the screen calls itself: what a person picks from.
	Name string
	// Width and Height are its size in pixels, or zero when the platform
	// cannot say before capturing.
	Width, Height int
}

// Screens lists the screens the system offers for capture. It captures none
// of them and asks for no permission to capture them.
func Screens() ([]ScreenInfo, error) { return listScreens() }

// A Screen is a screen opened for capture: a live source of pictures, in the
// pixel format the file players hand out, [PixelRGBA].
//
// A screen has no clock, no duration, no position and no tracks to choose: it
// hands out pictures as the screen changes, and a reader that falls behind
// loses the pictures in between rather than slowing the system's capture down.
// Its methods are safe to call from any goroutine.
type Screen struct {
	info ScreenInfo
	q    *liveFrames
	h    screenHandle

	once sync.Once
	err  error
}

// OpenScreen opens a screen: the one whose [ScreenInfo.ID] is id, or the
// system's main screen when id is empty. Opening a screen the system guards
// asks no question: see [RequestScreenPermission]. A screen the person has
// refused to share fails with [ErrPermissionDenied].
func OpenScreen(id string) (*Screen, error) {
	screens, err := listScreens()
	if err != nil {
		return nil, err
	}
	if len(screens) == 0 {
		return nil, ErrNoScreen
	}
	info := screens[0]
	if id != "" {
		found := false
		for _, scr := range screens {
			if scr.ID == id {
				info, found = scr, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("media: no screen with id %q", id)
		}
	}
	q := newLiveFrames(screenFrames)
	h, err := openScreen(info, q)
	if err != nil {
		return nil, err
	}
	return &Screen{info: info, q: q, h: h}, nil
}

// Info describes the screen that was opened.
func (s *Screen) Info() ScreenInfo { return s.info }

// NextFrame waits for the screen's next picture. It returns [ErrEnd] once the
// screen is closed, and ctx's error when ctx is done first.
//
// The caller owns the frame and releases it with [Frame.Release] when done.
func (s *Screen) NextFrame(ctx context.Context) (*Frame, error) { return s.q.next(ctx) }

// Close stops capturing, waits for the pictures in flight, and gives back the
// ones no one took. It is safe to call more than once.
func (s *Screen) Close() error {
	s.once.Do(func() {
		s.err = s.h.close()
		s.q.close()
	})
	return s.err
}

// A screen is a source of pictures, like a file is.
var _ Source = (*Screen)(nil)

// screenFrames is how many pictures a screen holds for a reader that falls
// behind before the oldest goes.
const screenFrames = 4

// screenHandle is the platform's end of an open screen.
type screenHandle interface {
	// close stops capturing and waits for the system to stop delivering.
	close() error
}

// ScreenPermission is what the system says about this app capturing the
// screen, without asking.
func ScreenPermission() PermissionStatus { return screenPermission() }

// RequestScreenPermission asks the person to let the app capture the screen,
// where the system has not asked yet, and reports the answer. macOS asks this
// once per app, and takes a changed answer only after the app is quit: the
// status it returns is what the system says now.
func RequestScreenPermission(ctx context.Context) (PermissionStatus, error) {
	return requestScreenPermission(ctx)
}

// OpenScreenPermissionSettings shows the system settings where the person can
// change the app's answer about capturing the screen.
func OpenScreenPermissionSettings() error { return openScreenPermissionSettings() }
