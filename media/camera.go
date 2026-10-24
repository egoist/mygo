package media

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNoCamera says that the system offers no camera to open.
var ErrNoCamera = errors.New("media: no camera")

// ErrPermissionDenied is what opening a camera fails with when the system does
// not let this app use one: [RequestCameraPermission] asks the person, and
// [OpenCameraPermissionSettings] shows where the answer can be changed.
var ErrPermissionDenied = errors.New("media: the app may not use a camera")

// CameraInfo describes a camera the system offers.
type CameraInfo struct {
	// ID identifies the camera to the system, and stays the same across
	// launches.
	ID string
	// Name is what the camera calls itself: what a person picks from.
	Name string
}

// Cameras lists the cameras the system offers. It opens none of them and asks
// for no permission to use them.
func Cameras() ([]CameraInfo, error) { return listCameras() }

// A Camera is a camera opened for capture: a live source of pictures, in the
// pixel format the file players hand out, [PixelRGBA].
//
// A camera has no clock, no duration, no position and no tracks to choose: it
// hands out pictures as the camera takes them, in the order it takes them, and
// a reader that falls behind loses the pictures in between rather than slowing
// the camera down. Its methods are safe to call from any goroutine.
type Camera struct {
	info CameraInfo
	q    *liveFrames
	h    cameraHandle

	once sync.Once
	err  error
}

// OpenCamera opens a camera: the one whose [CameraInfo.ID] is id, or the
// system's default camera when id is empty. Opening a camera the system guards
// asks no question: see [RequestCameraPermission]. A camera the person has
// refused, or the system will not offer, fails with [ErrPermissionDenied].
func OpenCamera(id string) (*Camera, error) {
	cams, err := listCameras()
	if err != nil {
		return nil, err
	}
	if len(cams) == 0 {
		return nil, ErrNoCamera
	}
	info := cams[0]
	if id != "" {
		found := false
		for _, cam := range cams {
			if cam.ID == id {
				info, found = cam, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("media: no camera with id %q", id)
		}
	}
	q := newLiveFrames(cameraFrames)
	h, err := openCamera(info, q)
	if err != nil {
		return nil, err
	}
	return &Camera{info: info, q: q, h: h}, nil
}

// Info describes the camera that was opened.
func (c *Camera) Info() CameraInfo { return c.info }

// NextFrame waits for the camera's next picture. It returns [ErrEnd] once the
// camera is closed, and ctx's error when ctx is done first.
//
// The caller owns the frame and releases it with [Frame.Release] when done.
func (c *Camera) NextFrame(ctx context.Context) (*Frame, error) { return c.q.next(ctx) }

// Close stops the camera, waits for the pictures in flight, and gives back the
// ones no one took. It is safe to call more than once.
func (c *Camera) Close() error {
	c.once.Do(func() {
		c.err = c.h.close()
		c.q.close()
	})
	return c.err
}

// A camera is a source of pictures, like a file is.
var _ Source = (*Camera)(nil)

// cameraFrames is how many pictures a camera holds for a reader that falls
// behind before the oldest goes. Cameras take thirty to sixty pictures a
// second, and a picture that waited is one nobody wants to see.
const cameraFrames = 4

// cameraHandle is the platform's end of an open camera.
type cameraHandle interface {
	// close stops the camera and waits for its thread to stop delivering.
	close() error
}

// liveFrames hands a camera's pictures from the system's thread to NextFrame,
// dropping the oldest picture when the reader falls behind: a live source has
// no pause for a slow reader, and a fresh picture is worth more than a stale
// one.
type liveFrames struct {
	mu     sync.Mutex
	frames []*Frame
	max    int
	closed bool
	err    error
	wake   chan struct{}
}

func newLiveFrames(max int) *liveFrames {
	return &liveFrames{max: max, wake: make(chan struct{})}
}

// push takes a picture from the system. It never waits: the system calls it
// from the camera's own thread, and holding that up would make the camera drop
// pictures where the app cannot see them.
func (q *liveFrames) push(f *Frame) {
	var oldest *Frame
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		f.Release()
		return
	}
	if len(q.frames) == q.max {
		oldest = q.frames[0]
		copy(q.frames, q.frames[1:])
		q.frames[len(q.frames)-1] = f
	} else {
		q.frames = append(q.frames, f)
	}
	close(q.wake)
	q.wake = make(chan struct{})
	q.mu.Unlock()
	oldest.Release()
}

// next waits for the next picture, or for the camera to close.
func (q *liveFrames) next(ctx context.Context) (*Frame, error) {
	for {
		q.mu.Lock()
		if len(q.frames) > 0 {
			f := q.frames[0]
			copy(q.frames, q.frames[1:])
			q.frames = q.frames[:len(q.frames)-1]
			q.mu.Unlock()
			return f, nil
		}
		if q.closed {
			err := q.err
			q.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return nil, ErrEnd
		}
		wake := q.wake
		q.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// close ends the pictures, giving back the ones no one took.
func (q *liveFrames) close() {
	q.finish(nil)
}

// fail ends the pictures with the error that stopped the camera: a camera
// whose cable is pulled or whose driver goes reports it rather than leaving a
// reader waiting for pictures that will not come.
func (q *liveFrames) fail(err error) {
	if err == nil {
		err = ErrEnd
	}
	q.finish(err)
}

// finish ends the pictures, with err for the error that ended them.
func (q *liveFrames) finish(err error) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.err = err
	frames := q.frames
	q.frames = nil
	close(q.wake)
	q.wake = make(chan struct{})
	q.mu.Unlock()
	for _, f := range frames {
		f.Release()
	}
}

// PermissionStatus is what the system says about letting this app use a
// camera.
type PermissionStatus int

const (
	// PermissionNotDetermined means the system has not asked the person yet,
	// and [RequestCameraPermission] will.
	PermissionNotDetermined PermissionStatus = iota
	// PermissionGranted means the app may use the camera.
	PermissionGranted
	// PermissionDenied means the person said no. Only the person can say yes
	// again, in the system's settings: [OpenCameraPermissionSettings] shows
	// where.
	PermissionDenied
	// PermissionRestricted means the system will not let the app ask, as a
	// managed Mac may not: the person cannot grant it from here.
	PermissionRestricted
	// PermissionNotRequired means the system does not ask per app: Windows
	// and Linux have no camera question, and opening the camera fails instead
	// where the system forbids it.
	PermissionNotRequired
)

// String is the status's name.
func (p PermissionStatus) String() string {
	switch p {
	case PermissionNotDetermined:
		return "not determined"
	case PermissionGranted:
		return "granted"
	case PermissionDenied:
		return "denied"
	case PermissionRestricted:
		return "restricted"
	case PermissionNotRequired:
		return "not required"
	}
	return fmt.Sprintf("PermissionStatus(%d)", int(p))
}

// CameraPermission reports what the system says about the app using cameras,
// without asking the person anything.
func CameraPermission() PermissionStatus { return cameraPermission() }

// RequestCameraPermission asks the person for permission to use cameras,
// where the system has not asked yet, and waits for the answer or for ctx to
// be done. It asks nothing once there is an answer, and returns the answer it
// finds.
func RequestCameraPermission(ctx context.Context) (PermissionStatus, error) {
	return requestCameraPermission(ctx)
}

// OpenCameraPermissionSettings shows the person where to change the app's
// permission to use cameras, where the system has such a place.
func OpenCameraPermissionSettings() error { return openCameraPermissionSettings() }
