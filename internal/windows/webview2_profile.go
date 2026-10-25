//go:build windows && (amd64 || arm64)

package windows

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// A page with a storage ID (PageOptions.StorageID) gets a WebView2 profile
// of its own: a directory of the app's user data folder that holds its
// cookies, local storage, IndexedDB and cache, which the pages of the same
// ID share and no other page does. Pages without one use the environment's
// default profile, as every page did before.
//
// The profile is named when the controller is created, through
// ICoreWebView2ControllerOptions and the environment's
// CreateCoreWebView2ControllerWithOptions (or its composition version for a
// web view in native UI). All profiles live in the one environment, so the
// browser process and the user data folder stay the app's own.

var iidICoreWebView2Environment10 = guid("ee0eb9df-6f12-46ce-b53f-3f47b9c928e0")

// Vtable indices, from WebView2.h (SDK 1.0.2739.15, C interface vtable
// order).
const (
	// ICoreWebView2Environment10, which came with the Runtime 1.0.1210.39.
	env10CreateControllerOptions                = 20
	env10CreateControllerWithOptions            = 21
	env10CreateCompositionControllerWithOptions = 22

	// ICoreWebView2ControllerOptions
	ctlOptionsPutProfileName = 4
)

// storageProfilePrefix starts the name of every profile of a storage ID,
// so that none is the Default profile of pages without one.
const storageProfilePrefix = "mygo-"

// profileName returns the name of the WebView2 profile of storage ID id. It
// is a name WebView2 takes whatever id is: profile names are ASCII case
// insensitive (they are directory names), must not end with a period and
// may not name the Default profile, but IDs differ by case and may end
// with a period. The name is the prefix and the first 160 bits of the
// SHA-256 of id in hex, 45 characters of the 64 WebView2 allows.
func profileName(id string) string {
	sum := sha256.Sum256([]byte(id))
	return storageProfilePrefix + hex.EncodeToString(sum[:20])
}

// profileOptions returns what creating a controller with the profile of
// storage ID id needs: the environment's ICoreWebView2Environment10 and the
// controller options naming the profile, which the caller releases. An
// empty id gets zeros, and creates its controller as before.
func (b *Backend) profileOptions(id string) (env10, options uintptr, err error) {
	if id == "" {
		return 0, 0, nil
	}
	env10 = queryInterface(b.env, &iidICoreWebView2Environment10)
	if env10 == 0 {
		return 0, 0, fmt.Errorf("mygo: a page with a StorageID needs the WebView2 Runtime 1.0.1210.39 or newer: %w", platform.ErrUnsupported)
	}
	if hr := comCall(env10, env10CreateControllerOptions, uintptr(unsafe.Pointer(&options))); failed(hr) || options == 0 {
		release(env10)
		return 0, 0, hresultError("creating the WebView2 controller options", hr)
	}
	if hr := comCall(options, ctlOptionsPutProfileName, uintptr(unsafe.Pointer(u16(profileName(id))))); failed(hr) {
		release(options)
		release(env10)
		return 0, 0, hresultError("naming the WebView2 profile of the StorageID", hr)
	}
	return env10, options, nil
}
