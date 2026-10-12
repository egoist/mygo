# Video player

`github.com/egoist/mygo/plugins/video` provides optional native-UI playback
with [libmpv](https://github.com/mpv-player/mpv/blob/master/include/mpv/client.h),
loaded through purego with `CGO_ENABLED=0`. It needs no webview, npm package,
Bun at runtime, or external media-player process.

```go
player, err := video.New(video.Options{Library: "/path/to/libmpv.2.dylib"})
if err != nil { return err }
if err := player.Load("movie.mp4"); err != nil { player.Close(); return err }
win := mygo.NewWindow(mygo.WindowOptions{
    Content: ui.View(func(c *ui.Context) { video.View(c, player).Fill() }),
})
win.OnClosed(player.Close)
// After App.Run returns: wait for player.Done().
```

`Load` accepts local files, file URLs and direct media URLs. It starts paused;
`Play` begins playback. A second `Load` during loading returns `ErrLoading`;
a source can be replaced after loading finishes or fails. libmpv's user
configuration, scripts, terminal input and external URL helper programs are
disabled. Network streams and codecs still depend on the native build.

`View` provides play/pause, stop, a seek slider, elapsed/total time, volume,
mute and playback speed. Space toggles playback; left/right seek five
seconds. `Play`, `Pause`, `Stop`, `Seek(time.Duration)`, `SetVolume(0..100)`,
`SetMuted` and `SetRate(0.25..4)` expose the same controls. Seeking clamps at
a known duration and returns `errors.ErrUnsupported` for an unseekable source.

`State` is an immutable snapshot of the engine's confirmed state: idle,
loading, paused, playing, ended, failed or closed, with position, duration,
volume, mute, rate, buffering, seekability and the last error. A control call
queues a command rather than waiting for playback; asynchronous failures
appear in `State.Err`. Source failures, including missing files and unsupported
codecs, set `Failed`. Replaced-file completion events are ignored. Playback
at EOF keeps the last frame; `Play` seeks back to the beginning when seekable.

All public methods are goroutine-safe. One worker drains native events and
another renders, following libmpv's [render API threading rules](https://github.com/mpv-player/mpv/blob/master/include/mpv/render.h).
Native callbacks are allocated once per signature for the process and route
by integer user data; they only signal workers. They never call UI or native
APIs. Worker frames invalidate MyGo safely; UI input, controls and painting
remain on the main thread. `Close` marks the player closed and returns at
once. Cleanup waits for both workers, frees the render context, removes
callback routing, then destroys the native engine; `Done` signals completion.
Loaded library mappings stay for the process lifetime; each player's native
handle, render context and frame resources are released separately.

## Capabilities and performance

`New` validates libmpv client API v2 and creates its software render context.
`Capabilities` reports playback and software rendering only after that
succeeds. Container/codec support, subtitle decoding and audio output depend
on the supplied libmpv/FFmpeg build; seekability is reported per source.

Video frames use libmpv's software RGB renderer, then MyGo's normal image
renderer. This works with MyGo's CPU, Metal, OpenGL and Direct3D paths, ordinary
clipping and screenshots, on both X11 and Wayland. It creates no native
window/view and has no dependency on
[native-view hosting PR #121](https://github.com/egoist/mygo/pull/121).
A future GPU/native-view backend can use that shared infrastructure without
creating another host abstraction in this plugin.

The default render surface is bounded to 1280×720; `MaxWidth`/`MaxHeight`
change it, up to 4096 per axis and 8 million pixels. Resolution follows the
viewport at up to two pixels per DIP inside that bound. Software scaling,
color conversion and frame upload cost CPU and memory bandwidth; large or
high-frame-rate videos may drop frames. This API advertises no hardware
decoding, HDR output or DRM playback. Output is opaque SDR RGB. Subtitles
and letterboxing are drawn by libmpv. Full-screen playback is the app's window
policy; track selection, playlists and recording are not exposed here.

## Installation and packaging

Install/build a matching 64-bit libmpv with the software render API, audio
output and desired codecs. The engine is feature detected by `New`; an
installed `mpv` executable alone is insufficient without its shared library.
Use the [upstream build instructions](https://github.com/mpv-player/mpv#compilation).
For development, Homebrew's `mpv` provides libmpv on macOS; many Linux
distributions provide a `libmpv2` runtime package. Windows needs a shared
libmpv distribution for the app's architecture.

| Target | Library names tried | Requirements |
| --- | --- | --- |
| macOS amd64/arm64 | `libmpv.2.dylib` | dependencies, audio output, matching architecture |
| Linux amd64/arm64 | `libmpv.so.2`, `libmpv.so.1` | client API v2, matching libc, dependencies/codecs |
| Windows amd64/arm64 | `libmpv-2.dll`, `mpv-2.dll` | matching architecture and dependency DLLs |
| Other targets | unavailable | compiles; loading returns `errors.ErrUnsupported` |

`Options.Library` explicitly selects one library; it never falls back to a
different one. Otherwise packaged resources are tried before system libraries
(and Homebrew locations on macOS). Windows uses packaged/explicit paths,
never the current working directory. Include the engine and **all** FFmpeg,
audio and other dependencies using MyGo's platform resource directories:

```text
resources/
  darwin-arm64/libmpv.2.dylib
  darwin-amd64/libmpv.2.dylib
  linux-amd64/libmpv.so.2
  windows-amd64/libmpv-2.dll
```

Add the other architectures you build and dependency files beside the engine.
Set relative dylib install names / ELF rpaths appropriately. MyGo signs native
resources as part of packaging. Check the selected build's GPL/LGPL terms and
third-party codec/license notices before distributing it; those depend on the
build configuration. No engine is downloaded at runtime, and no automatic
`mygo-plugin.json` downloads are configured by this plugin.

`examples/content-native -mpv /path/library -video movie.mp4` is a usable app.
`MYGO_MPV_LIBRARY=/path/library CGO_ENABLED=0 go test ./plugins/video` runs
native integration tests; `ffmpeg` generates a short local test clip. Tests
use `Silent` to select a null audio output; normal playback uses real audio.
