//go:build darwin

package media

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/media/internal/darwin"
)

// listCameras lists the cameras AVFoundation offers.
func listCameras() ([]CameraInfo, error) {
	devices, err := darwin.VideoDevices()
	if err != nil {
		return nil, err
	}
	cameras := make([]CameraInfo, len(devices))
	for i, device := range devices {
		cameras[i] = CameraInfo{ID: device.ID, Name: device.Name}
	}
	return cameras, nil
}

// avCamera turns the pictures a capture session hands over into frames.
type avCamera struct {
	cap  *darwin.Capture
	q    *liveFrames
	pool pixPool

	// A camera stamps its pictures with the machine's own clock, which began
	// before this camera did: the first picture's stamp moves the rest to the
	// start of the camera, which is where a frame's PTS counts from.
	first     time.Duration
	haveFirst bool
}

// openCamera starts a capture session on a camera.
func openCamera(info CameraInfo, q *liveFrames) (cameraHandle, error) {
	// A camera the person has said no to is not one this app can open,
	// which is a different answer from a camera that is not there.
	switch darwin.CameraAuthorizationStatus() {
	case darwin.CameraDenied, darwin.CameraRestricted:
		return nil, fmt.Errorf("media: opening the camera %q: %w", info.Name, ErrPermissionDenied)
	}
	c := &avCamera{q: q}
	cap, err := darwin.OpenCapture(info.ID, c.deliver)
	if err != nil {
		return nil, err
	}
	c.cap = cap
	return c, nil
}

func (c *avCamera) close() error { return c.cap.Close() }

// deliver takes one of the camera's pictures: it copies the picture into a
// frame this package hands out and returns before the next one. It runs on the
// capture queue, where the sample buffer lives only until it returns.
func (c *avCamera) deliver(sample uintptr) {
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
	buf := c.pool.get(w * h * 4)
	if err := copyCameraPicture(buf, picture, w, h); err != nil {
		c.pool.put(buf)
		return
	}
	pts := darwin.SamplePTS(sample)
	if !c.haveFirst {
		c.first, c.haveFirst = pts, true
	}
	c.q.push(NewFrame(PixelRGBA, w, h, []Plane{{Pix: buf, Stride: w * 4}}, pts-c.first, darwin.SampleDuration(sample), func() {
		c.pool.put(buf)
	}))
}

// cameraPermission is what macOS says about this app using cameras.
func cameraPermission() PermissionStatus {
	switch darwin.CameraAuthorizationStatus() {
	case darwin.CameraAuthorized:
		return PermissionGranted
	case darwin.CameraDenied:
		return PermissionDenied
	case darwin.CameraRestricted:
		return PermissionRestricted
	}
	return PermissionNotDetermined
}

// requestCameraPermission asks the person, and waits for the answer.
func requestCameraPermission(ctx context.Context) (PermissionStatus, error) {
	if err := darwin.LoadCamera(); err != nil {
		return PermissionNotDetermined, err
	}
	if status := cameraPermission(); status != PermissionNotDetermined {
		return status, nil
	}
	answer := make(chan bool, 1)
	darwin.RequestCameraAccess(func(granted bool) {
		select {
		case answer <- granted:
		default:
		}
	})
	select {
	case <-answer:
		return cameraPermission(), nil
	case <-ctx.Done():
		return cameraPermission(), ctx.Err()
	}
}

// openCameraPermissionSettings shows where the person can change the answer.
func openCameraPermissionSettings() error { return darwin.OpenCameraSettings() }
