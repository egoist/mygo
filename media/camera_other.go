//go:build !darwin && !(windows && (amd64 || arm64)) && !(linux && (amd64 || arm64))

package media

import "context"

// Platforms where nothing captures pictures yet: asking says what is missing
// rather than failing without a word.

// listCameras finds no cameras here.
func listCameras() ([]CameraInfo, error) {
	return nil, unsupported("no cameras on this platform yet")
}

// openCamera opens nothing here.
func openCamera(info CameraInfo, q *liveFrames) (cameraHandle, error) {
	return nil, unsupported("no cameras on this platform yet")
}

// cameraPermission says the system asks nothing here, as there is no camera
// path to ask about.
func cameraPermission() PermissionStatus { return PermissionNotRequired }

// requestCameraPermission asks nothing here.
func requestCameraPermission(ctx context.Context) (PermissionStatus, error) {
	return PermissionNotRequired, nil
}

// openCameraPermissionSettings has nothing to show here.
func openCameraPermissionSettings() error {
	return unsupported("no camera settings on this platform yet")
}
