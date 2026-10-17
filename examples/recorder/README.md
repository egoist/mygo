# Recorder

Records a screen or a camera into an MP4 file while it happens, and plays
the recording back in the same window. MyGo reads the pictures through the
platform's own capture stack, encodes them with the machine's own encoder,
and writes the file with its own muxer — no bundled codec, no web page, no
`MediaRecorder`.

```sh
go run ./examples/recorder
go run ./examples/recorder -seconds 5s -out screen.mp4
go run ./examples/recorder -source camera
go run ./examples/recorder -source camera -id "ASUS FHD webcam"
go run ./examples/recorder -list
```

`-source` picks a screen (the default) or a camera, `-id` picks one by its id
or its name, `-out` is the file to write (an MP4 with H.264, so what records
plays in anything) and `-seconds` takes a duration, as in `5s`, and stops after it instead of waiting for
the button. `-list` prints what this machine offers and exits.

The pieces:

- `media.Screens` and `media.Cameras` list what the machine offers, and
  `media.OpenScreen` and `media.OpenCamera` open one as a source of pictures.
  Screens come as the system says they changed and cameras as they take
  pictures; a reader that falls behind loses the pictures in between rather
  than slowing the system's capture thread.
- `media.Record` pulls the source's pictures, encodes them with the platform's
  encoder — VideoToolbox on macOS, Media Foundation on Windows, `x264enc` on
  Linux — and writes them into the file with MyGo's muxer. A `Recorder` pulled
  by hand does the same for a preview that also saves what it shows.
- A failure still finishes the file, so what was recorded plays up to the
  last picture; a recording is picture only for now.
- `media/mediaui` shows the recorded file in the window, the way it shows any
  `media.Source`: a bitmap the frames are drawn into, pulled off the main
  thread and changed on it.

macOS asks for the Screen Recording permission the first time a screen is
recorded (System Settings > Privacy & Security > Screen Recording), and takes
that grant on the next launch of the app; a camera is asked for with a dialog.
Windows needs a session logged in to a desktop to capture a screen, and Linux
needs an X11 display: a Wayland session without XWayland says so rather than
capturing black pictures.

The example's tests need neither an encoder nor a screen: they record a
source that is only pictures, and skip where the machine has no encoder
wired up.
