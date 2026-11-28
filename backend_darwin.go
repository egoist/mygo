//go:build darwin && !ios

package mygo

import (
	"strconv"
	"strings"
	"syscall"

	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/internal/platform"
)

func newBackend() platform.Backend { return darwin.New() }

func enableNotificationSenders() { darwin.EnableNotificationSenders() }

func linkNotificationImages() { darwin.LinkNotificationImages() }

func linkContentRules() { darwin.LinkContentRules() }

func linkNowPlaying() { darwin.LinkNowPlaying() }

func linkMenuIcons() { darwin.LinkMenuIcons() }

func linkContextMenus() { darwin.LinkContextMenus() }

func supportsSessionProxy() bool {
	version, err := syscall.Sysctl("kern.osrelease")
	major, _, _ := strings.Cut(version, ".")
	n, parseErr := strconv.Atoi(major)
	return err == nil && parseErr == nil && n >= 23 // macOS 14
}

var openProxyStore = darwin.NewProxyDataStore
