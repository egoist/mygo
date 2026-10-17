# Media

Package `media` plays video and sound, reads cameras and screens, and
records pictures to a file. It does all of it through the media stack the
operating system already has — AVFoundation and VideoToolbox on macOS,
Media Foundation on Windows, GStreamer on Linux — so it needs no cgo, ships
no codec and adds nothing to the size of an app that does not import it.

```go
import (
	"context"

	"github.com/egoist/mygo/media"
)

player, err := media.Open("movie.mp4") // pictures and sound
if err != nil {
	return err
}
defer player.Close()

for frame, err := range media.Frames(context.Background(), player) {
	if err != nil {
		return err // the source failed: the end of a file just ends the loop
	}
	show(frame.Image()) // *image.RGBA
	frame.Release()
}
```

MyGo pulls pictures rather than being called back: `Source.NextFrame`
blocks until a picture is due, and the caller owns it until
`Frame.Release`. A file player, a camera and a screen all answer that way,
so what takes one of them takes any of them.

## Playing a file

`Open` opens a path and `OpenURL` a location the system can read over the
network, and both start playing. `Tracks` lists what the file holds,
`SelectTracks` picks a picture and a sound track by index, `Duration` and
`Position` say how long it is and where it is, `Pause`, `Play` and `Seek`
move it, and `Close` stops it.

The sound plays through package `audio`, so an app's music and its video
share one volume, one device and one mixer. Pictures are handed out to the
sound's clock: a picture is due when the sound at its time has been heard,
not earlier, and one whose time has passed while the app was busy is
dropped rather than shown late. From a file with no sound the player keeps
a clock of its own.

### Network locations on macOS

On macOS, `OpenURL` reads an `http` or `https` URL through a transport of
MyGo's own: the asset's media loading requests are answered over Go's
`net/http`, and the bytes AVFoundation reads are the origin's bytes, not a
representation a content encoding made: the loader asks for the identity
encoding and refuses a `Content-Encoding` it does not understand. The origin
must therefore answer with its length — a `Content-Length`, or a
`Content-Range` for a range request — or the open fails. The container is
recognised from the URL's extension, or from the origin's content type when
the extension says nothing: MP4, MOV, M4A, MP3 and WAV.

This is what lets `Close` stop a read that waits for the network: it fails
the loading requests, and the native read that waited on their data returns
before the readers are freed. `Seek` does not depend on the origin's range
support: a range it cannot get as a range is skipped to from the start of
the reply. A URL of any other scheme than `file` or `http(s)` is reported as
unsupported, because AVFoundation's reader would load it outside this
transport, where a read of it could not be stopped. Playlists (HLS) and
streaming protocols (RTSP, RTMP) are not read this way: the reader cannot
read them at all.

`Open` and `OpenURL` take no context yet: the first load of an asset's
metadata is synchronous, and `Close` cannot stop an open that has not
returned. `Close` covers what a returned source is doing.

## Showing pictures in native UI

`media/mediaui` draws a source's pictures in a window MyGo draws itself,
with no web page and no `<video>` element: a bitmap that the frames are
copied into, and the goroutine that pulls them.

```go
import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/media"
	"github.com/egoist/mygo/media/mediaui"
	"github.com/egoist/mygo/ui"
)

type viewer struct{ video *mediaui.Video }

func (v *viewer) view(c *ui.Context) {
	if v.video != nil {
		v.video.Element(c).Fill()
	}
}

player, _ := media.Open("movie.mp4")
v := &viewer{}
mygo.App.WhenReady(func() {
	win := mygo.NewWindow(mygo.WindowOptions{Title: "Movie", Content: ui.View(v.view)})
	v.video = mediaui.NewVideo(win, player)
	v.video.Start()
})
mygo.App.Run()
```

`NewVideo` takes any `Source`, so a window shows a file, a camera and a
screen the same way. `WithFit` (or `SetFit`) picks how a frame fills its
element: the whole picture with bars by default, as `ui.Contain` does.

In a window that shows a web page, keep the page's own `<video>`: the
webview plays media with the system's framework, and `media` is for
pictures an app draws and for what a page cannot do, such as recording.

## Cameras

`Cameras` lists what the machine offers and `OpenCamera` opens one by id,
or the first when the id is empty. A camera hands out `PixelRGBA` frames
as it takes them, with no clock, duration or tracks, and its methods are
safe to call from any goroutine.

A camera that a reader falls behind on loses the pictures in between
rather than slowing the camera's own thread down: live sources keep a
small queue and drop the oldest picture.

macOS's capture stack hands out its own formats, which MyGo converts in
Go after it has tried to have the system convert them — asking
`AVCaptureVideoDataOutput` for 32-bit colour makes macOS abort inside its
own renderer — so a camera costs a conversion per picture. Linux reads
`v4l2src` through GStreamer and Windows reads Media Foundation's source
reader.

## Screens

`Screens` lists the displays and `OpenScreen` opens one to capture.
`Screen.Info` says its name and, where the system knows it before
capturing, its size. Frames come as the screen changes rather than at a
fixed rate, so a still screen shows fewer pictures and a busy one more.

| platform | what captures | needs |
|---|---|---|
| macOS | ScreenCaptureKit | the Screen Recording permission |
| Windows | GDI, on a session logged in to a desktop | a real desktop: a service or a bare remote shell has none |
| Linux | X11's `ximagesrc`, through GStreamer | an X11 display, with Wayland's XWayland working too |

Wayland has no X11 screen to read: a Wayland session without XWayland
fails with an error that matches `errors.ErrUnsupported` and names the
missing piece, rather than capturing black pictures. A portal-based
capture is what a later version adds.

## Permissions

A camera and a screen are the system's to grant. Each has a status and a
request:

```go
switch media.CameraPermission() {
case media.PermissionGranted, media.PermissionNotRequired:
	// open the camera
case media.PermissionNotDetermined:
	status, err := media.RequestCameraPermission(ctx) // shows the system's question
default:
	media.OpenCameraPermissionSettings() // where the answer was "no"
}
```

`PermissionGranted`, `PermissionDenied`, `PermissionRestricted`,
`PermissionNotDetermined` and `PermissionNotRequired` are the answers.
macOS asks with a dialog once per app; Windows has a privacy setting per
app and says `PermissionNotDetermined` until a camera is first opened;
Linux has no gate, the device nodes are the permission, and says
`PermissionNotRequired`. `ScreenPermission` and `RequestScreenPermission`
are the same for screens; macOS cannot tell "never asked" from "answered
no", so both return `PermissionNotDetermined` until the grant is there.
`OpenScreenPermissionSettings` opens the settings where the answer was no.

`OpenCamera` refuses a camera the app may not use with `ErrPermissionDenied`
instead of letting the system's stack fail with something obscure.

On macOS a camera also needs a purpose in the app's `Info.plist`,
`NSCameraUsageDescription`, or macOS refuses the camera without asking;
[Building and distributing](distribution.md#infoplist-and-entitlements) shows
where it goes. A screen needs its permission alone.

## Recording

A `Recorder` writes the pictures of any source into an MP4 file, encoded
by the machine's own encoder: VideoToolbox on macOS, Media Foundation on
Windows, `x264enc` on Linux. `Record` is the loop, for recording a source
until it ends or `ctx` is done:

```go
camera, _ := media.OpenCamera("")
defer camera.Close()

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
err := media.Record(ctx, "camera.mp4", camera,
	media.WithRecordFrameRate(30),
	media.WithRecordBitrate(4_000_000))
```

A `Recorder` pulled by hand takes frames as they come, which is what a
preview that also saves does:

```go
file, _ := os.Create("screen.mp4")
rec, _ := media.NewRecorder(file, media.WithRecordContainer(media.ContainerMP4))
defer rec.Close()

for frame, err := range media.Frames(ctx, screen) {
	if err != nil {
		break
	}
	rec.WriteFrame(frame) // the recorder copies what it keeps
	frame.Release()
}
```

The first picture fixes the size: a later picture of another size is a
mistake, not a file of two sizes. A recording is picture only for now —
sound capture comes with a later version, which is also why every
platform writes MP4 with H.264 rather than Linux's WebM. A failure still
finishes the file, so what was recorded plays up to the last picture.

The muxer is Go, in package `media`: the platform's encoder hands over
packets and MyGo writes the container, so no bundled library is needed to
write a file. `NewMuxer` writes one to any `io.Writer`, `CanMux` says
which containers it writes, and both are what a recording app that
encodes something else of its own would use.

## What the machine can do

`Capabilities` reports what the system's stack can do now — the codecs,
the containers and the features — as the platform answers it, not as a
promise of MyGo's:

```go
caps := media.Capabilities()
if !caps.CanEncode(media.CodecH264) {
	return errors.New("this machine has no H.264 encoder: enable Media Foundation on Windows N")
}
```

`FeaturePlayback`, `FeatureDecode`, `FeatureEncode`, `FeatureCamera`,
`FeatureScreenCapture` and `FeatureMicrophone` are what else it may have.
A source that needs something the machine has not got fails with a
`MissingComponentError`, which matches `errors.ErrUnsupported` and names
the component and what to install:

```go
var missing *media.MissingComponentError
if errors.As(err, &missing) {
	log.Printf("cannot play: %s is missing (%s)", missing.Component, missing.Hint)
}
```

## What to install

Nothing is bundled, so what a machine can play is its operating system,
distribution and installed components:

| platform | for playback | for cameras and screens | for recording |
|---|---|---|---|
| macOS 12.3 or later | nothing: AVFoundation is part of macOS | Screen Recording permission for screens | nothing: VideoToolbox is part of macOS |
| Windows 10 and 11 | nothing, but an N edition needs the Media Feature Pack | a desktop session for screens; the camera privacy setting for cameras | nothing, but an N edition needs the Media Feature Pack |
| Linux | GStreamer, with `gstreamer1.0-plugins-base`, `gstreamer1.0-plugins-good` and `gstreamer1.0-libav` | `gstreamer1.0-plugins-good` for `v4l2src`, `gstreamer1.0-x` for `ximagesrc` | `gstreamer1.0-plugins-ugly` for `x264enc` |

On Debian and Ubuntu that is:

```sh
apt install libgstreamer1.0-0 gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
	gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav gstreamer1.0-x
```

`gst-inspect-1.0` lists what a machine has; a missing element, decoder or
codec is what `Capabilities` leaves out and what `MissingComponentError`
names. An app should say what is missing and what to install rather than
fail without a word.

`mygo doctor` checks a development machine for the parts MyGo itself
needs, and does not check a machine's codecs and capture stacks.

## Examples

- [examples/video-player](../examples/video-player) plays a file in a
  window of native UI, with its sound, and either scales the picture or
  turns it over.
- [examples/camera](../examples/camera) shows what a camera sees, asks for
  it the first time, and lists what the machine offers.
- [examples/recorder](../examples/recorder) records a screen or a camera
  to an MP4 file while showing it, then plays the file back.
