package mygo

import (
	"os"

	"github.com/egoist/mygo/internal/surface"
)

// production is set to "1" by `mygo build` through
// -ldflags "-X github.com/egoist/mygo.production=1".
var production string

// IsDev reports whether the app runs in development: true unless it was
// built for production with `mygo build`, or MYGO_ENV=production is set.
// Development builds enable the web inspector by default.
func IsDev() bool {
	if production == "1" {
		return false
	}
	return os.Getenv("MYGO_ENV") != "production"
}

// devLaunched tells that `mygo dev` launched the app, which it does with
// MYGO_DEV=1: App.Relaunch then has mygo dev start it again, and the app comes
// back as it was (KeepState).
var devLaunched bool

func init() { takeDevMarker() }

// takeDevMarker takes the variable, which isn't meant for child processes. It
// runs again as the app starts: Android's host sets the environment after this
// package's init.
func takeDevMarker() {
	// Unset only what's there: unsetting reaches the C environment, where
	// Android's host may be setting it right now, as the library loads.
	v, ok := os.LookupEnv("MYGO_DEV")
	if !ok {
		return
	}
	os.Unsetenv("MYGO_DEV")
	if v == "1" && production != "1" {
		devLaunched, surface.DevLaunched = true, true
	}
}

// launchedByDev reports whether `mygo dev` launched the app.
func launchedByDev() bool {
	takeDevMarker()
	return devLaunched
}
