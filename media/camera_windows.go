//go:build windows && (amd64 || arm64)

package media

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/egoist/mygo/media/internal/windows"
)

// listCameras lists the machine's cameras, as Media Foundation's device
// enumerator knows them.
func listCameras() ([]CameraInfo, error) {
	windows.Enter()
	if err := windows.Load(); err != nil {
		return nil, unsupported("Media Foundation: %v", err)
	}
	devices, err := windows.CaptureDevices()
	if err != nil {
		return nil, cameraError(err)
	}
	defer releaseDevices(devices)
	cams := make([]CameraInfo, 0, len(devices))
	for _, device := range devices {
		var info CameraInfo
		if name, err := device.Name(); err == nil {
			info.Name = name
		}
		if id, err := device.ID(); err == nil {
			info.ID = id
		}
		cams = append(cams, info)
	}
	return cams, nil
}

// openCamera starts a camera, whose pictures it hands to q.
func openCamera(info CameraInfo, q *liveFrames) (cameraHandle, error) {
	windows.Enter()
	if err := windows.Load(); err != nil {
		return nil, unsupported("Media Foundation: %v", err)
	}
	device, err := findDevice(info.ID)
	if err != nil {
		return nil, err
	}
	source, err := device.Activate()
	if err != nil {
		device.Release()
		if windows.IsAccessDenied(err) {
			return nil, fmt.Errorf("media: opening the camera %q: %w", info.Name, ErrPermissionDenied)
		}
		return nil, cameraError(err)
	}
	reader, err := windows.OpenReaderFromSource(source, true)
	if err != nil {
		windows.ReleaseObject(source)
		device.Release()
		return nil, cameraError(err)
	}
	c := &mfCamera{
		q:      q,
		device: device,
		source: source,
		reader: reader,
		done:   make(chan struct{}),
	}
	if err := c.negotiate(); err != nil {
		c.release()
		return nil, err
	}
	go c.run()
	return c, nil
}

// negotiate asks the reader for pictures as the colour the UI draws, and keeps
// what the device agreed to.
func (c *mfCamera) negotiate() error {
	mt, err := windows.NewMediaType()
	if err != nil {
		return cameraError(err)
	}
	defer mt.Release()
	if err := mt.SetGUID(&windows.KeyMajorType, &windows.MajorVideo); err != nil {
		return cameraError(err)
	}
	if err := mt.SetGUID(&windows.KeySubtype, &windows.FormatRGB32); err != nil {
		return cameraError(err)
	}
	if err := c.reader.SetCurrentMediaType(windows.StreamFirstVideo, mt); err != nil {
		return cameraError(err)
	}
	current, err := c.reader.CurrentMediaType(windows.StreamFirstVideo)
	if err != nil {
		return cameraError(err)
	}
	defer current.Release()
	w, h, ok := current.FrameSize()
	if !ok || w <= 0 || h <= 0 {
		return unsupported("a camera picture of %dx%d", w, h)
	}
	c.w, c.h = w, h
	// A picture Media Foundation stores from its bottom row up says so
	// with a negative stride, which is what a buffer with no rows of its
	// own is read with.
	if stride, ok := current.DefaultStride(); ok {
		c.stride = int(stride)
	}
	return nil
}

// findDevice takes the camera with id out of the machine's list, giving the
// others back.
func findDevice(id string) (windows.CaptureDevice, error) {
	devices, err := windows.CaptureDevices()
	if err != nil {
		return windows.CaptureDevice{}, cameraError(err)
	}
	defer releaseDevices(devices)
	for i, device := range devices {
		name, err := device.ID()
		if err != nil || name != id {
			continue
		}
		found := devices[i]
		devices[i] = windows.CaptureDevice{}
		return found, nil
	}
	return windows.CaptureDevice{}, fmt.Errorf("media: no camera with id %q", id)
}

// releaseDevices gives back every device in a list.
func releaseDevices(devices []windows.CaptureDevice) {
	for _, device := range devices {
		device.Release()
	}
}

// cameraError turns what Media Foundation said about a camera into what media
// promises.
func cameraError(err error) error {
	if err == nil {
		return nil
	}
	if windows.IsAccessDenied(err) {
		return fmt.Errorf("%w: %v", ErrPermissionDenied, err)
	}
	return readerError(err, FeatureCamera, "")
}

// An mfCamera is a camera read through Media Foundation's source reader: the
// device makes samples as the reader is asked for them, and a goroutine of its
// own hands them to the queue the caller reads.
type mfCamera struct {
	q      *liveFrames
	device windows.CaptureDevice
	source uintptr
	reader windows.SourceReader
	done   chan struct{}
	pool   pixPool

	w, h   int
	stride int

	mu     sync.Mutex
	closed bool
}

// run reads the camera's pictures until it stops or breaks.
func (c *mfCamera) run() {
	defer close(c.done)
	for {
		if c.stopped() {
			return
		}
		// Go moves a goroutine between threads: the thread that asks
		// the device for a picture joins the multithreaded apartment
		// each time a thread it has not been on before does.
		windows.Enter()
		sample, flags, _, err := c.reader.ReadSample(windows.StreamFirstVideo)
		if err != nil {
			if c.stopped() {
				return
			}
			c.q.fail(cameraError(err))
			return
		}
		if flags&windows.FlagEndOfStream != 0 {
			sample.Release()
			c.q.fail(errors.New("media: the camera stopped"))
			return
		}
		if sample.Empty() {
			// A live source says so when it has nothing at this
			// time: the next read waits for the picture after it.
			if flags&windows.FlagError != 0 && !c.stopped() {
				c.q.fail(errors.New("media: reading the picture"))
				return
			}
			continue
		}
		f, err := pictureOf(sample, &c.pool, c.w, c.h, c.stride, FeatureCamera)
		sample.Release()
		if err != nil {
			c.q.fail(err)
			return
		}
		c.q.push(f)
	}
}

// stopped reports whether the camera is being closed.
func (c *mfCamera) stopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// close stops the device, which is what a read that is waiting for a picture
// needs to come back, and waits for the reading goroutine before letting go of
// what it reads from.
func (c *mfCamera) close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	// A read that is waiting for a picture a stopped device does not
	// send would wait for ever: flushing the stream brings it back, and
	// the reading goroutine sees that the camera is closed and stops.
	// Then the device is shut down, and what the goroutine reads from is
	// released only once it has stopped reading.
	c.reader.Flush(windows.StreamFirstVideo)
	c.device.ShutdownObject()
	<-c.done
	c.release()
	return nil
}

// release drops what the camera holds of the device.
func (c *mfCamera) release() {
	c.reader.Release()
	windows.ReleaseObject(c.source)
	c.source = 0
	c.device.Release()
}

// cameraPermission is what Windows says about a camera: it has no answer until
// an app asks the device itself, as a desktop app is either allowed or blocked
// by the person's privacy settings rather than asked.
func cameraPermission() PermissionStatus { return PermissionNotDetermined }

// requestCameraPermission starts the first camera and reports what that said,
// which is the answer Windows has: the person's settings decide it.
func requestCameraPermission(ctx context.Context) (PermissionStatus, error) {
	if err := ctx.Err(); err != nil {
		return PermissionNotDetermined, err
	}
	windows.Enter()
	if err := windows.Load(); err != nil {
		return PermissionNotDetermined, unsupported("Media Foundation: %v", err)
	}
	devices, err := windows.CaptureDevices()
	if err != nil {
		return PermissionNotDetermined, cameraError(err)
	}
	defer releaseDevices(devices)
	if len(devices) == 0 {
		return PermissionNotDetermined, ErrNoCamera
	}
	source, err := devices[0].Activate()
	if err != nil {
		if windows.IsAccessDenied(err) {
			return PermissionDenied, nil
		}
		return PermissionNotDetermined, cameraError(err)
	}
	windows.ReleaseObject(source)
	devices[0].ShutdownObject()
	return PermissionGranted, nil
}

// openCameraPermissionSettings shows the person the page where cameras are
// allowed or blocked for desktop apps.
func openCameraPermissionSettings() error {
	return windows.OpenSettings("ms-settings:privacy-webcam")
}
