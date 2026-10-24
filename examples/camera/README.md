# Camera

Shows what a camera sees in native UI: MyGo pulls the pictures itself and
draws them in a window it draws. No web page, no `<video>` element, no
`getUserMedia`, and no media engine in the binary — the platform's own capture
stack hands MyGo the pictures.

```sh
go run ./examples/camera
go run ./examples/camera -list
go run ./examples/camera -camera "ASUS FHD webcam"
```

`-camera` takes a camera's id or its name; without it the first camera the
machine offers is opened. `-list` prints what this machine has and exits.
Close the window to stop.

The pieces:

- `media.Cameras` lists what the machine offers: `AVCaptureDevice` on macOS,
  Media Foundation's capture devices on Windows, `v4l2src`-visible devices
  through GStreamer on Linux.
- `media.OpenCamera` opens one and hands out pictures with `NextFrame`, with a
  small queue of the newest pictures that drops the oldest ones rather than
  making the platform's capture thread wait: a camera is live, and a reader
  that falls behind misses pictures rather than delaying the next one.
- `media.CameraPermission` says whether this app may use a camera, and
  `media.RequestCameraPermission` asks when the system has not been asked yet.
  macOS and Windows answer; on Linux the device nodes are the permission.
  `OpenCameraPermissionSettings` opens the system's settings where the answer
  was no.
- `media/mediaui` shows those pictures: it owns a bitmap and a goroutine, and
  updates the bitmap on the main thread. It shows any `media.Source`, so a
  camera and a file being played are drawn the same way.

A machine with no camera says so — `media.ErrNoCamera` — and one whose platform
capture stack is missing (GStreamer on Linux, for instance) fails with an error
that matches `errors.ErrUnsupported` and names what was missing.

The example's tests need no camera: they choose between the cameras a machine
lists, and draw the window from a source that stands in for one.
