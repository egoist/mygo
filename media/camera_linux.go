//go:build linux && (amd64 || arm64)

package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/media/internal/linux"
)

// cameraPullWait is how long a pull from a camera waits before it looks at
// whether the camera is still wanted. A live source is not always ready with a
// picture, and a pull that waits is a pull that does not spin.
const cameraPullWait = 100 * time.Millisecond

// cameraTestSource, when it is set, is what a camera is read from instead of a
// device: a test's way of putting pictures through a capture that no machine
// here has hardware for.
var cameraTestSource string

// listCameras lists the machine's cameras, as its GStreamer's device monitor
// knows them.
func listCameras() ([]CameraInfo, error) {
	if err := linux.Load(); err != nil {
		return nil, &MissingComponentError{
			Component: "GStreamer",
			Feature:   FeatureCamera,
			Hint: "install it, for example on Debian or Ubuntu: " +
				"apt install libgstreamer1.0-0 gstreamer1.0-plugins-base",
		}
	}
	devices, err := linux.VideoDevices()
	if err != nil {
		return nil, err
	}
	cams := make([]CameraInfo, 0, len(devices))
	for _, device := range devices {
		cams = append(cams, CameraInfo{ID: device.ID, Name: device.Name})
	}
	return cams, nil
}

// openCamera starts a camera, whose pictures it hands to q.
func openCamera(info CameraInfo, q *liveFrames) (cameraHandle, error) {
	if err := linux.Load(); err != nil {
		return nil, &MissingComponentError{
			Component: "GStreamer",
			Feature:   FeatureCamera,
			Hint: "install it, for example on Debian or Ubuntu: " +
				"apt install libgstreamer1.0-0 gstreamer1.0-plugins-base",
		}
	}
	description, err := cameraPipeline(info)
	if err != nil {
		return nil, err
	}
	pipe, err := linux.NewPipelineFrom(description)
	if err != nil {
		return nil, cameraError(err)
	}
	sink := pipe.ByName("video")
	if !sink.Valid() {
		pipe.Release()
		return nil, errors.New("media: the camera's pipeline has no sink to read pictures from")
	}
	c := &gstCamera{
		q:    q,
		pipe: pipe,
		sink: linux.AppSink{Element: sink},
		bus:  pipe.Bus(),
		done: make(chan struct{}),
	}
	if pipe.SetState(linux.StatePlaying) == linux.StateChangeFailure {
		err := c.busError()
		c.release()
		if err == nil {
			err = fmt.Errorf("media: the camera %q would not start", info.Name)
		}
		return nil, err
	}
	go c.run()
	return c, nil
}

// cameraPipeline is the pipeline a camera is read through: the device, a
// conversion to the packed RGBA the UI draws, and the sink this version pulls
// pictures from. A picture that arrives late is dropped at the sink rather than
// waiting for a reader: a live picture that waited is one nobody wants to see.
func cameraPipeline(info CameraInfo) (string, error) {
	source := cameraTestSource
	if source == "" {
		if !strings.HasPrefix(info.ID, "/") || strings.ContainsAny(info.ID, "\" \t\n!") {
			return "", fmt.Errorf("media: %q is not a camera device this version opens", info.ID)
		}
		source = `v4l2src device="` + info.ID + `"`
	}
	return source + " ! videoconvert ! video/x-raw,format=RGBA " +
		"! appsink name=video sync=false max-buffers=2 drop=true", nil
}

// cameraError turns what GStreamer said about a camera into what media
// promises: a machine missing a plugin is a component that is missing.
func cameraError(err error) error {
	if err == nil {
		return nil
	}
	var missing *linux.MissingError
	if errors.As(err, &missing) {
		return &MissingComponentError{Component: missing.Element, Feature: FeatureCamera, Hint: pluginHint}
	}
	var failure *linux.PipelineError
	if errors.As(err, &failure) {
		if what := missingElement(failure.Message); what != "" {
			return &MissingComponentError{Component: what, Feature: FeatureCamera, Hint: pluginHint}
		}
		return fmt.Errorf("media: reading the camera: %s", failure.Message)
	}
	// A pipeline that would not build says which of its elements the
	// machine does not have, in GStreamer's own words.
	if what := missingElement(err.Error()); what != "" {
		return &MissingComponentError{Component: what, Feature: FeatureCamera, Hint: pluginHint}
	}
	return err
}

// A gstCamera is a camera read through a GStreamer pipeline: the pipeline
// pushes pictures into an appsink, and a goroutine of its own hands them to the
// queue the caller reads.
type gstCamera struct {
	q    *liveFrames
	pipe linux.Element
	sink linux.AppSink
	bus  *linux.Bus
	done chan struct{}
	pool pixPool

	mu     sync.Mutex
	closed bool
}

// run reads the camera's pictures until it stops or breaks.
func (c *gstCamera) run() {
	defer close(c.done)
	for {
		if c.stopped() {
			return
		}
		sample := c.sink.PullSample(cameraPullWait)
		if sample == nil {
			if c.sink.IsEOS() {
				c.q.fail(errors.New("media: the camera stopped"))
				return
			}
			if err := c.busError(); err != nil {
				c.q.fail(err)
				return
			}
			continue
		}
		f, err := pictureFrame(sample, &c.pool)
		sample.Release()
		if err != nil {
			c.q.fail(err)
			return
		}
		c.q.push(f)
	}
}

// busError is what the pipeline said went wrong, when it said anything.
func (c *gstCamera) busError() error {
	if c.bus == nil {
		return nil
	}
	message := c.bus.Pop(linux.MessageError, 0)
	if message == nil {
		return nil
	}
	defer message.Release()
	return cameraError(message.PipelineError())
}

// stopped reports whether the camera is being closed, which is what a pull
// that came back with nothing looks at.
func (c *gstCamera) stopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// close stops the pipeline, which is what lets the reading goroutine go, and
// waits for it before letting go of the pipeline it reads from.
func (c *gstCamera) close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.pipe.SetState(linux.StateNull)
	<-c.done
	c.release()
	return nil
}

// release drops what the camera holds of the pipeline.
func (c *gstCamera) release() {
	c.sink.Element.Release()
	if c.bus != nil {
		c.bus.Release()
		c.bus = nil
	}
	c.pipe.Release()
}

// cameraPermission is what Linux says about a camera: nothing, as a camera here
// is a device node whose permissions are the system's.
func cameraPermission() PermissionStatus { return PermissionNotRequired }

// requestCameraPermission asks nobody: there is no permission to ask for.
func requestCameraPermission(ctx context.Context) (PermissionStatus, error) {
	return PermissionNotRequired, nil
}

// openCameraPermissionSettings has nowhere to send the caller: a camera here is
// a device node, not an answer the system keeps for an app.
func openCameraPermissionSettings() error { return nil }
