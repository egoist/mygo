//go:build linux && (amd64 || arm64)

package media

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/egoist/mygo/media/internal/linux"
)

// screenTestSource, when it is set, is what a screen is read from instead of an
// X server: a test's way of putting pictures through the capture path on a
// machine that runs none.
var screenTestSource string

// screenHint is what a person can do about the X11 plugin a screen needs. It is
// advice, not a contract.
const screenHint = "install the GStreamer X11 plugin, for example on Debian or Ubuntu: " +
	"apt install gstreamer1.0-x"

// listScreens lists the machine's screens. X11 has one screen the whole
// session draws on; what Wayland calls a screen needs the XDG ScreenCast
// portal, which this version does not speak, and it says so rather than
// handing out black pictures.
func listScreens() ([]ScreenInfo, error) {
	if err := linux.Load(); err != nil {
		return nil, &MissingComponentError{
			Component: "GStreamer",
			Feature:   FeatureScreenCapture,
			Hint: "install it, for example on Debian or Ubuntu: " +
				"apt install libgstreamer1.0-0 gstreamer1.0-plugins-base",
		}
	}
	if os.Getenv("DISPLAY") == "" {
		if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland" {
			return nil, &MissingComponentError{
				Component: "the XDG ScreenCast portal on Wayland",
				Feature:   FeatureScreenCapture,
				Hint:      "this version captures X11 and XWayland screens",
			}
		}
		return nil, &MissingComponentError{
			Component: "an X11 display",
			Feature:   FeatureScreenCapture,
			Hint:      "run where DISPLAY names an X server, or start one, for example Xvfb",
		}
	}
	probe, err := linux.NewPipelineFrom("ximagesrc")
	if err != nil {
		return nil, screenError(err)
	}
	probe.Release()
	return []ScreenInfo{{ID: "x11", Name: "The screen"}}, nil
}

// openScreen starts capturing the screen, whose pictures it hands to q.
func openScreen(info ScreenInfo, q *liveFrames) (screenHandle, error) {
	if err := linux.Load(); err != nil {
		return nil, &MissingComponentError{
			Component: "GStreamer",
			Feature:   FeatureScreenCapture,
			Hint: "install it, for example on Debian or Ubuntu: " +
				"apt install libgstreamer1.0-0 gstreamer1.0-plugins-base",
		}
	}
	source := screenTestSource
	if source == "" {
		// Damage tracking would only make the pipeline quiet while the screen
		// does not change; a reader that asks for a picture gets the screen as
		// it is now.
		source = "ximagesrc use-damage=false"
	}
	return startGSTLive(source+" ! videoconvert ! video/x-raw,format=RGBA "+
		"! appsink name=video sync=false max-buffers=2 drop=true", q, screenError)
}

// screenError turns what GStreamer said about a screen into what media
// promises: a machine missing a plugin is a component that is missing.
func screenError(err error) error {
	if err == nil {
		return nil
	}
	var missing *linux.MissingError
	if errors.As(err, &missing) {
		return &MissingComponentError{Component: missing.Element, Feature: FeatureScreenCapture, Hint: screenHint}
	}
	var failure *linux.PipelineError
	if errors.As(err, &failure) {
		if what := missingElement(failure.Message); what != "" {
			return &MissingComponentError{Component: what, Feature: FeatureScreenCapture, Hint: screenHint}
		}
		return fmt.Errorf("media: capturing the screen: %s", failure.Message)
	}
	if what := missingElement(err.Error()); what != "" {
		return &MissingComponentError{Component: what, Feature: FeatureScreenCapture, Hint: screenHint}
	}
	return err
}

// screenPermission is what Linux says about capturing the screen: nothing, as
// the display server's own access is the permission.
func screenPermission() PermissionStatus { return PermissionNotRequired }

// requestScreenPermission asks nobody: there is no permission to ask for.
func requestScreenPermission(ctx context.Context) (PermissionStatus, error) {
	return PermissionNotRequired, nil
}

// openScreenPermissionSettings has nowhere to send the caller.
func openScreenPermissionSettings() error { return nil }
