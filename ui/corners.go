package ui

import "runtime"

// continuousCorners curves rounded corners as Apple does (scene.Op.Continuous),
// by default on macOS, where AppKit's and SwiftUI's controls have them, and
// circular elsewhere, as Windows and GTK draw them; SetContinuousCorners
// changes it for the app.
var continuousCorners = runtime.GOOS == "darwin"

// gpuCorners, once SetContinuousCorners turned continuous corners on, has a
// window's GPU renderer draw them (continuousGPU): Metal's always does, and
// Direct3D's and OpenGL's only when asked, so that the apps not asking link
// none of their code.
var gpuCorners func(gpuRenderer)

// SetContinuousCorners sets how the app's rounded corners curve: those of
// the widgets and of your own elements (Radius), clips, shadows, images,
// web views' holes and materials. On, they curve continuously, as Core
// Animation draws macOS's controls, SwiftUI's rounded rectangles and
// CALayer's continuous corner curve: the curve leaves the straight edges
// gradually, about 1.53 radii from the corner, where a quarter circle meets
// them with a visible change of curvature, which shows most at larger radii.
// Off, they are quarter circles, as Windows and GTK draw them. They are on
// by default on macOS, and off elsewhere.
//
// An app that has its own look, the same on every platform, turns them on
// (or off) for good, before it opens its windows:
//
//	func main() {
//		ui.SetContinuousCorners(true)
//		mygo.NewWindow(mygo.WindowOptions{Content: ui.View(app.View)})
//		mygo.App.Run()
//	}
//
// Every renderer draws them the same, on the GPU (Metal, Direct3D 11 and
// OpenGL, in browsers too) and in memory, as the Tester does; on Direct3D
// and OpenGL, only the parts of a frame near those corners draw with the
// shaders of the curve, as on Metal. A web view's own corners are those its
// system draws, circular on Windows and Linux.
func SetContinuousCorners(on bool) {
	continuousCorners = on
	if on {
		gpuCorners = continuousGPU
	}
}

// continuousGPU has the GPU renderer r draw continuous corners, where it
// draws them only when asked to (d3d11's and gl's ContinuousCorners).
func continuousGPU(r gpuRenderer) {
	if c, ok := r.(interface{ ContinuousCorners() }); ok {
		c.ContinuousCorners()
	}
}
