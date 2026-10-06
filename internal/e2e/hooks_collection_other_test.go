//go:build !darwin && !(linux && (amd64 || arm64)) && !(windows && (amd64 || arm64))

package e2e

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/platform"
	"testing"
)

func externalCollection(*testing.T, string, int, int, int, int) {}

func collectionProbe(*mygo.Window, string, int, int) (platform.AccessCollectionProbe, bool) {
	return platform.AccessCollectionProbe{}, false
}
func collectionPerform(*mygo.Window, string, int, int, string) bool { return false }

func collectionLifetime(*mygo.Window, string, int, int) bool { return false }
