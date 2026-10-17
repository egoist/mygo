# Video player

Plays a media file in native UI: MyGo reads the pictures and the sound itself
and draws them into a window it draws itself. No web page, no `<video>`
element, and no media engine in the binary — the platform's own decoders do
the decoding, and MyGo owns the clock, the pacing and the sound.

```sh
go run ./examples/video-player clip.mp4
```

Space plays and pauses, the left and right arrows seek five seconds either
way, and F switches between showing the whole picture and filling the window
with it. `-audio=false` plays it silently.

The pieces:

- `media` opens the file and hands out frames and sound. Each platform reads
  with its own stack — `AVAssetReader` on macOS, `IMFSourceReader` on Windows,
  GStreamer on Linux — so the demuxers, the decoders and the sample rate
  conversion are the system's own, and what a machine can play is what its
  operating system has.
- `media/mediaui` shows those frames: it owns a bitmap and a goroutine, pulls
  each frame when the player's clock says it is due, and updates the bitmap on
  the main thread. The frame before is released only once the frame after it
  has been drawn, since the bitmap shares its pixels with the GPU.
- `audio` plays the sound, and is the clock: the sound's own device position
  decides when the next picture is due, so the pictures follow the sound
  rather than drifting away from it.

The example's tests play `media/testdata/clip.mp4`, a two second 160×120
H.264 file with AAC sound, without audio, and run the view in `ui.NewTester`
rather than a window.
