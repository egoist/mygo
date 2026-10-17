//go:build !darwin && !(windows && (amd64 || arm64)) && !(linux && (amd64 || arm64))

package media

import "context"

// Screens on platforms whose media stack is not wired up yet answers that the
// platform cannot, rather than pretending there is nothing to capture.
func listScreens() ([]ScreenInfo, error) {
	return nil, unsupported("no screens on this platform yet")
}

func openScreen(info ScreenInfo, q *liveFrames) (screenHandle, error) {
	return nil, unsupported("no screens on this platform yet")
}

func screenPermission() PermissionStatus { return PermissionNotRequired }

func requestScreenPermission(ctx context.Context) (PermissionStatus, error) {
	return PermissionNotRequired, nil
}

func openScreenPermissionSettings() error {
	return unsupported("no screen settings on this platform yet")
}
