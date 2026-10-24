//go:build linux && (amd64 || arm64)

package media

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/media/internal/linux"
)

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
	return startGSTLive(description, q, cameraError)
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
