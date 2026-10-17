# Multimedia in cross-platform desktop frameworks — and what a no-cgo Go framework can do

Research compiled for MyGo (system webview *or* self-drawn native UI, `CGO_ENABLED=0` always,
[AGENTS.md](../../AGENTS.md)). Sources are primary (project READMEs, official docs, source repos,
release notes, issue trackers); every non-obvious claim carries its URL. Where a claim could not be
verified against a primary source it is marked **unverified**.

Status: in progress. Ordering below follows the two questions asked — *what Go can do with no cgo
today*, then *what the other frameworks do and what it costs them*.

---

## Part 1 — Pure-Go / no-cgo multimedia today

### 1.1 Video decode

**AV1 / AVIF — solved in pure Go, and this is the single most important finding.**

- [`github.com/gen2brain/gav1d`](https://github.com/gen2brain/gav1d) — "AV1 video and AVIF image
  decoder **and encoder** in pure Go. A port of [dav1d](https://code.videolan.org/videolan/dav1d),
  **bit-exact against it**. No CGo, no dependencies." SIMD for amd64 (AVX2, AVX-512), arm64 (NEON),
  riscv64 (RVV with `GORISCV64=rva23u64`); `-tags noasm` gives pure Go everywhere. Supports
  8/10/12-bit, 4:2:0/4:2:2/4:4:4/monochrome, **film grain**, scalable streams, container features
  (alpha, `grid`, `clap`/`irot`/`imir`, `colr`, image sequences, Exif/XMP, gain maps). API:
  `avif.Decode(r) (*image.NRGBA | *image.NRGBA64)`, registered with `image.RegisterFormat`;
  `av1.Decoder{}.DecodeOBUs(temporalUnit) ([]*Picture, error)` decodes a whole temporal unit;
  `avif.Encode(w, img, avif.EncodeOptions{Quality, Speed})` is **all-intra, 8-bit, 4:2:0 or
  grayscale, alpha as a second item**. License: decoder is a BSD-2-Clause port of dav1d, encoder's
  forward transform a BSD-2-Clause port of libaom — with an explicit note that the implementation
  grants no AV1 patent rights, pointing at the AV1 patent licence. This is a *bitstream* decoder: it
  decodes OBUs/temporal units, so an app still has to demux a container and drive the frame loop.
- [`github.com/gen2brain/avif`](https://github.com/gen2brain/avif) — AVIF with animated support.
  **CGo-free**, but not pure Go in the same sense: libavif + dav1d (decode) + aom (encode) compiled
  to WASM and run on [wazero](https://wazero.io/); it *first* tries a dynamic/shared library via
  [purego](https://github.com/ebitengine/purego) and falls back to WASM. Build tags `nodynamic`
  (WASM only) and `wasm2go` (transpile the WASM to Go with
  [wasm2go](https://github.com/ncruces/wasm2go) instead of running it on wazero). The README itself
  now points at `gav1d` as "a pure Go alternative ... with SIMD support, no CGo/WASM and no
  dependencies".

**H.264 — partial, and honest projects say so.**

- [`github.com/Eyevinn/hi264`](https://github.com/Eyevinn/hi264) — "Pure Go H.264/AVC IDR decoder &
  bitstream generator". Explicit scope, from the README: the decoder handles **IDR frames**
  (including multi-slice) **plus P_Skip frames**; "**full P/B-frame decoding is out of scope**"; all
  processing is **8-bit 4:2:0 only**. The encoder is *not* a general encoder — it emits flat-colour
  `I_16x16` DC-prediction frames from a grid pattern, no motion estimation; it exists to make
  DASH/HLS test fixtures and to extend fMP4 segments with empty frames. Pure Go, only dependency
  [`github.com/Eyevinn/mp4ff`](https://github.com/Eyevinn/mp4ff) (which is itself pure Go) for
  SPS/PPS/slice headers and fragmented MP4 I/O. Claims pixel-perfect output against FFmpeg over 50
  golden tests for its supported feature set. Verdict: **useful for thumbnails/keyframe extraction
  and test fixtures, not for playing video.**
- [`github.com/oops1/go.264`](https://github.com/oops1/go.264) — **"H.264/AVC encoder and decoder in
  pure Go. No cgo, ever."** Read from the repo's own
  [README](https://raw.githubusercontent.com/oops1/go.264/master/README.md); BSD-2-Clause. The README
  states it builds and passes its full test suite with `CGO_ENABLED=0`, with no C toolchain and no
  shared-library link step. *These are the project's claims, read from its README — not independently
  built, benchmarked or conformance-tested for this note.* Claimed scope:
  - **Decoder**: High profile, "complete, bit-exact against ffmpeg on twenty-nine clips".
  - **Encoder**: High profile — 8x8 transform, `Intra_8x8`, scaling matrices, all intra block sizes,
    all P partitions, 8x8 sub-macroblocks, multiple references, **B slices** (bi-prediction, spatial
    direct, POC reordering), weighted prediction, SEI (recovery point / buffering period / picture
    timing / user data), **complete CAVLC and CABAC in both directions** (streams "18–30% smaller"
    with CABAC), in-loop deblocking, reference picture management (sliding window + MMCO including
    long-term), rate-distortion quantisation, level selection, slice-parallel encoding ("ten times
    faster on twenty threads for 2 to 12 per cent more bits") and macroblock-row wavefront
    parallelism, intra refresh, coded-picture-buffer model, lossless transform bypass. SIMD kernels:
    transformed differences, six-tap and bilinear interpolation, block matching, 4x4 transform and
    quantisation.
  - **Hardware acceleration without cgo** — the pattern most relevant to MyGo: encode on Windows via
    **Media Foundation**, on 64-bit Linux via **NVENC** and **VA-API**, all loaded at run time through
    `purego`/`dlopen`; decode on Windows through **Direct3D** ("nine times our own decoder at
    1080p"). `-tags go264_nohwaccel` leaves the backends out so the binary stays static (documented
    consequence: purego alone makes a Linux binary dynamically linked against glibc even with no cgo).
  - **API**: `go264.NewEncoder(go264.EncoderConfig{Width, Height, FPSNum, FPSDen, GOPSize, QP,
    BitrateKbps, VBVBufferKbits, VBVMaxrateKbps, CBR, RateFactor, QComp, IPRatio, PBRatio, AQMode,
    AQStrength, ForceSoftware, RepeatParameterSets})`, `enc.Encode(i420Frame) (packet, error)`,
    `go264.NewDecoder()`, `dec.Decode(annexB) ([]frame, error)`, `f.AppendI420(buf)`, and
    `Encoder.Backend()`/`Decoder.Backend()` reporting `"vaapi"|"nvenc"|"mediafoundation"|"cpu"`. CLI:
    `go264 encode -s 1280x720 -qp 24 -gop 30 -i in.yuv -o out.264`, also `-b <kbps>` and `-crf <n>`.
  - **Documented caveats**: the VA-API backend probes its driver in a child process because a driver
    can abort inside libva "which no recover can catch"; AQ (adaptive quantisation) is off by default
    because it measured "1.8 dB worse at equal rate" on scrolling text; before v1.9.0 the hardware
    backends were separate modules (`.../hwaccel/vaapi`, `.../hwaccel/nvenc`).
  This changes the H.264 verdict from "partial" to "at least *claimed* pure-Go and complete, with the
  hardware path following MyGo's own purego-with-build-tag-opt-out pattern" — but it is a young,
  single-maintainer project, so it belongs behind a plugin boundary until it has real-world mileage.
- [`github.com/y9o/go-openh264`](https://pkg.go.dev/github.com/y9o/go-openh264) — binds Cisco's
  **OpenH264** binaries from Go **without cgo** ("Since `purego` is used instead of Cgo, there is no
  need to prepare a C compiler at build time"), with an optional `-tags cgoopenh264` path; the Go
  bindings themselves are generated with cgo. This is the *bind-a-native-codec* pattern, not a pure
  Go codec. OpenH264's own licensing (Cisco pays MPEG-LA royalties only for its *binary* builds) is
  the reason the OpenH264 README tells you to download the prebuilt library — worth checking before
  relying on it.

**VP8 — solved in pure Go too; VP9 still open.**

- [`github.com/gen2brain/vpx`](https://pkg.go.dev/github.com/gen2brain/vpx) — "WebP image decoder and
  encoder, and a **VP8 video codec, in pure Go**. A port of libwebp, **byte-exact against it**. No
  CGo, no dependencies." SIMD on amd64 (SSE2, AVX2), arm64 (NEON), riscv64 (RVV, `GORISCV64=rva23u64`);
  `-tags noasm` builds pure Go everywhere. This is a real VP8 decoder (the WebP *lossy* format **is**
  VP8 keyframes, which is why one library gives both), and it supersedes the earlier "no pure-Go VP8
  decoder" reading.
- **VP9: no pure-Go decoder found.** The remaining gap in the royalty-free-codec set; VP9 is also what
  WebM/YouTube-mostly uses, so its absence is the practical limit on "play web video with pure Go".
  (libvpx is BSD, but the format's patent position is covered by Google's royalty-free grant, not by
  the licence alone.)

**MJPEG — the one "video" path that is genuinely pure Go and production-viable today.** Motion-JPEG
is a sequence of JPEG images, so `image/jpeg` from the standard library is the decoder; the transport
is what differs (HTTP multipart, RTP, a file). Everything else about the pipeline (colour convert,
scale, upload to GPU) is ordinary Go.

**WASM as the escape hatch.** The `gen2brain/avif` design — compile a C codec to WASM, run it with a
pure-Go WASM runtime (`wazero`), or transpile it to Go with `wasm2go` — is the general trick that
makes "any C codec" reachable from a `CGO_ENABLED=0` build. It costs CPU (interpreter or transpiled
code rather than optimised native), memory (the WASM linear memory plus the code) and binary size
(the module is embedded unless it is shipped as a downloaded asset). The `gav1d` port shows the
higher-effort, much better-performing alternative: port the hot codec to Go and use SIMD assembly.

**Pure-Go demux / bitstream plumbing that does exist:**

- [`github.com/Eyevinn/mp4ff`](https://github.com/Eyevinn/mp4ff) — MP4/fMP4/CMAF read+write,
  AVC SPS/PPS/slice-header parsing, NAL extraction. Pure Go.
- `container/mp4` in the wider Go module cache, `github.com/abema/go-mp4`,
  `github.com/bluenviron/mediacommon`, `github.com/pion/...` — all pure Go and all *parsing*
  (box/NAL/sample tables), not decoding. (See also home-grown parsers inside MyGo's own repo if we
  add one.)

### 1.2 Video encode

Nothing production-viable in pure Go was found for H.264/H.265/VP8/VP9/AV1 *video*:

- AV1 **stills** can be encoded (`gav1d`'s all-intra 8-bit 4:2:0/grayscale encoder; `avif`'s
  aom-via-WASM encoder). "All-intra" means no inter-frame prediction, so this is an image encoder,
  not a video encoder.
- H.264 encoding in pure Go (from `hi264`'s README): grid-pattern `I_16x16` frames and `P_Skip`
  frames only, no motion estimation, explicitly "not a general-purpose video encoder".
- **The exception, if it holds up: [`oops1/go.264`](https://github.com/oops1/go.264)** claims a
  general-purpose pure-Go H.264 encoder with B slices, CABAC, rate control and slice/wavefront
  parallelism, plus NVENC/VA-API/Media-Foundation hardware encoding through `purego` and a
  `go264_nohwaccel` build tag (§1.1). Treat it as the one credible pure-Go *video* encoder candidate;
  verify by building and encoding a real clip before depending on it.
- x264/x265 bindings ([`github.com/gen2brain/x264-go`](https://github.com/gen2brain/x264-go) and
  friends) are cgo bindings to the C library. **Unverified** for this note: whether gen2brain
  ships a CCGO/wasm variant of x264 the way it does for avif.

Practical conclusion: **encoding video to a compressed file without cgo means (a) MJPEG/PNG/GIF/animated
WebP frames, (b) AV1 still-image sequences via `gav1d`, or (c) shelling out to a bundled external
`ffmpeg` binary and piping raw frames/Y4M to it.** Option (c) is what several frameworks effectively
do and it keeps the no-cgo rule (an external process is not a C dependency of the app), at the cost
of shipping or requiring a ~30–70 MB binary.

### 1.3 Container and bitstream parsing (pure Go, useful)

`mp4ff` (above) plus the parsing side of `pion`/`bluenviron` are enough to implement HLS/DASH
playlists, MP4 sample tables and NAL extraction. This is the layer that makes "use the OS decoder on
the extracted NAL units" possible (see §1.6).

### 1.4 Camera capture from Go

- [`github.com/kbinani/screenshot`](https://github.com/kbinani/screenshot) is screen capture, not
  camera (see §1.5).
- [`github.com/blackjack/webcam`](https://github.com/blackjack/webcam) — **Linux V4L2, pure Go, no
  cgo.** Verified from the README: "This is a go library for working with webcams ... It depends
  entirely on V4L2 framework, thus will compile and work only on Linux machine." API:
  `webcam.Open("/dev/video0")`, `cam.StartStreaming()`, `cam.WaitForFrame(timeout)` (returns
  `*webcam.Timeout` on timeout), `cam.ReadFrame()`, `cam.StopStreaming()`, `cam.SetBufferCount(64)`.
  Consequences to know: only **MMAP** streaming is supported, and "currently image format is defined
  by 4-byte code received from V4L2 ... not very handy if you want to do some image manipulations" —
  i.e. **it gives you bytes, not an `image.Image`**, so the YUYV/MJPEG→RGBA conversion is yours. That
  is the Linux half of a no-cgo camera backend, and it is the model to copy.
- [`github.com/pion/mediadevices`](https://github.com/pion/mediadevices) — the broadest Go capture
  API, but its platform drivers are the problem: **v4l2 on Linux, AVFoundation on macOS, Media
  Foundation on Windows**, and the macOS/Windows ones have historically been cgo. **Unverified** per
  driver, verify before relying on it.
- [`github.com/hybridgroup/gocv`](https://github.com/hybridgroup/gocv) — OpenCV bindings, **cgo**
  (and a huge OpenCV dependency).
- [`github.com/gen2brain/malgo`](https://github.com/gen2brain/malgo) — miniaudio bindings, **cgo**
  (the same author's `gav1d`/`avif` work is deliberately cgo-free; malgo is not).
- [`github.com/gen2brain/cam2ip`](https://github.com/gen2brain/cam2ip) — camera-to-HTTP-MJPEG
  server; built on the above driver stack.
- [`github.com/gordonklaus/portaudio`](https://github.com/gordonklaus/portaudio) — PortAudio
  bindings, **cgo**. This is also the only microphone input path Beep points at
  ([MarkKremer/microphone](https://github.com/MarkKremer/microphone), "a wrapper around PortAudio").

The honest summary: **desktop camera capture from Go exists but is cgo-bound in every maintained
cross-platform option.** A no-cgo path means writing per-platform bindings ourselves — V4L2 ioctls
(Linux), AVFoundation via purego + `objc_msgSend` (macOS), Media Foundation COM via `syscall`
(Windows) — which is exactly the kind of work MyGo already does in `internal/darwin`,
`internal/linux`, `internal/windows`.

### 1.5 Screen capture from Go

- [`github.com/kbinani/screenshot`](https://github.com/kbinani/screenshot) — "Go library to capture
  desktop screen. Multiple display supported. Supported GOOS: windows, darwin, linux, freebsd,
  openbsd, netbsd. **cgo free except for GOOS=darwin**." API: `Capture(x, y, w, h) (*image.RGBA,
  error)`, `CaptureDisplay(displayIndex)`, `CaptureRect(rect)`, `GetDisplayBounds(displayIndex)`.
  MIT. Effectively the de-facto standard (used by Sliver and similar tooling). **The macOS path is
  cgo**, so MyGo cannot use it as-is on darwin; the Windows and Linux paths show what a pure-Go
  backend looks like (GDI/BitBlt-class APIs; X11 `XShm`).
- **Pure-Go macOS screen capture exists — three of them, all purego-based.** This retires the "nobody
  has done it in the open" reading:
  - [`github.com/go-macos/screencapture`](https://pkg.go.dev/github.com/go-macos/screencapture) —
    "Screen and window capture on macOS from pure Go — `CGO_ENABLED=0`, via
    [`purego`](https://github.com/ebitengine/purego) and `go-macos/objc`. A thin, honest wrapper over
    **ScreenCaptureKit**, built for a compositor that redraws every frame." That description is
    almost a spec for MyGo's own renderer.
  - [`github.com/LocalKinAI/sckit-go`](https://github.com/LocalKinAI/sckit-go) — "Pure-Go binding to
    macOS ScreenCaptureKit. No cgo. Sub-20ms frames. Display, window, app, region, and exclude-list
    capture". Its companion write-up describes the distribution pattern MyGo would need for shipping
    an embedded shim: "purego + `//go:embed` + a synchronous Objective-C shim"
    ([localkin.dev/papers/embedded-dylib](https://www.localkin.dev/papers/embedded-dylib)).
  - [`github.com/tmc/apple/screencapturekit`](https://github.com/tmc/apple/screencapturekit) —
    generated Go bindings for the ScreenCaptureKit framework.
  Context: `CGWindowListCreateImage` is deprecated since macOS 14 Sonoma and ScreenCaptureKit replaced
  both `CGDisplayStream` and `CGWindowListCreateImage`
  ([Apple developer forums](https://developer.apple.com/forums/thread/740493)); ScreenCaptureKit
  delivers `CMSampleBuffer`s and can capture **audio** as well
  ([Apple docs](https://developer.apple.com/documentation/screencapturekit)).
- No pure-Go **Windows DXGI Desktop Duplication** binding was found; reachable via `syscall` COM
  vtable calls (MyGo already does COM-free `syscall` work in `internal/windows`).
- Linux: X11 is doable in pure Go (shared-memory images); **Wayland requires
  `xdg-desktop-portal` ScreenCast + PipeWire**, which is a significantly larger binding surface.
  **Unverified** whether a pure-Go PipeWire capture client exists.

### 1.6 Audio output

- [`github.com/ebitengine/oto/v3`](https://github.com/ebitengine/oto/v3) — "A low-level library to
  play sound." README states **Windows, macOS, Linux, FreeBSD, OpenBSD, WebAssembly all "no Cgo
  required"**; Android, iOS, Switch and Xbox are also supported. macOS needs
  `AudioToolbox.framework` (auto-linked); Linux/BSD use **`github.com/jfreymuth/pulse`** (a pure-Go
  PulseAudio client) and **fall back to ALSA by loading `libasound.so.2` dynamically at runtime**,
  so no ALSA dev headers are needed to build. API: one `oto.Context` per program (sample rate,
  channel count 1 or 2, `FormatSignedInt16LE`, …), `Context.NewPlayer(io.Reader)` for streaming,
  `Play/Pause/IsPlaying/Seek/SetBufferSize/BufferedSize`, plus `PauseAndStopReading` so a file can be
  closed. Cross-compiles by setting `GOOS` alone.
- [`github.com/hajimehoshi/oto`](https://github.com/hajimehoshi/oto) v2 is the older module path;
  `ebitengine/oto/v3` is the maintained line.
- [`github.com/gopxl/beep`](https://github.com/gopxl/beep) v2 — a higher-level layer **on top of
  Oto**: "Decode and play WAV, MP3, Ogg Vorbis, FLAC and MIDI", "Encode and save WAV", stereo-only
  by design, a `Streamer` interface with Loops/Pause/Volume/Mix/Seq/Resample compositors, ~1K LOC
  core, MIT. Notably it **does not** do microphone capture; its README points at the PortAudio
  wrapper for that. `faiface/beep` is the predecessor module path.
- MyGo already ships its own `audio` package with the same shape (decode-once `Sound`, streaming
  `OpenStream`, `Player` with volume/pan/speed/loop/loop-range and 5 ms ramps, `audio.SetVolume`) —
  see [docs/audio.md](../audio.md). That is the "own the mixer" approach rather than delegating to
  Oto, which is the same trade Qt/Godot/SDL make.

### 1.7 Audio decode / encode, pure Go

- MP3 decode: [`github.com/hajimehoshi/go-mp3`](https://github.com/hajimehoshi/go-mp3) — used by
  Oto's own README example; pure Go.
- Ogg Vorbis decode:
  [`github.com/jfreymuth/oggvorbis`](https://github.com/jfreymuth/oggvorbis) +
  [`github.com/jfreymuth/vorbis`](https://github.com/jfreymuth/vorbis) — pure Go (same author as the
  pure-Go PulseAudio client Oto uses).
- FLAC: [`github.com/mewkiz/flac`](https://github.com/mewkiz/flac) — pure Go decode (and some
  encode); Beep's FLAC support comes from this family.
- WAV: `encoding/binary` + [`github.com/go-audio/wav`](https://github.com/go-audio/wav) — pure Go.
- MIDI: Beep lists MIDI among its decodable formats (via its own MIDI synth path).
- **AAC: no pure-Go decoder found.** **Opus: no pure-Go decoder found**
  (`hraban/opus`, `pion/opus` are cgo/wasm-adjacent respectively — **unverified**; treat Opus as
  unavailable without a native library).
- **Encoding** is essentially WAV-only in pure Go (Beep: "Encode and save WAV").

### 1.8 Microphone capture / recording to file

**No pure-Go microphone capture was found.** Every maintained cross-platform option is cgo:
PortAudio (`gordonklaus/portaudio`, and everything built on it), miniaudio (`gen2brain/malgo`).
Oto's v3 README documents playback only — no capture API. The same reasoning as §1.4 applies: a
no-cgo path means driving CoreAudio AudioUnit (macOS), WASAPI (Windows) and ALSA/PulseAudio (Linux)
ourselves through purego/syscall, and the pure-Go PulseAudio client (`jfreymuth/pulse`) already
proves the Linux half is feasible.

Recording *to file* then only needs an encoder: WAV is trivial and pure Go, everything else is the
§1.7 problem.

### 1.9 Images

- Standard library: `image/jpeg` (decode+encode), `image/png` (decode+encode), `image/gif`
  (decode+encode), `image/draw` for compositing (no fast scaler).
- [`golang.org/x/image`](https://pkg.go.dev/golang.org/x/image): `webp` (**decode only**),
  `bmp`, `tiff`, `vp8`/`vp8l` (used by webp), `draw` (including a `scaler`), `font`, `riff`.
  So **WebP encode is not in the standard library or x/image.**
- AVIF/AV1 stills: `gav1d` (pure Go, decode **and** a limited all-intra encode) and `gen2brain/avif`
  (cgo-free, WASM or a dlopen'd libavif).
- **WebP: [`gen2brain/vpx`](https://pkg.go.dev/github.com/gen2brain/vpx)** (above) is a pure-Go WebP
  decoder **and encoder** (byte-exact against libwebp, with SIMD and a `noasm` fallback) — this closes
  the "WebP encode is not in the standard library or `x/image`" gap in pure Go.
  [`gen2brain/webp`](https://github.com/gen2brain/webp) is the sibling that additionally handles
  **animated** WebP, via libwebp compiled to WASM and transpiled with `wasm2go` (cgo-free; prefers a
  system shared library through purego if one is installed). The same author ships AVIF, JXL and HEIC
  libraries on that identical "purego-else-WASM/transpiled" pattern.
- WebP third-party options split sharply by cgo: `chai2010/webp`, `kolesa-team/go-webp`,
  `Kagami/go-avif` are all cgo; `gen2brain/webp` and `gav1d` are on the cgo-free side.
- Pure-Go image processing: [`github.com/disintegration/imaging`](https://github.com/disintegration/imaging)
  (resize/rotate/crop/filter), `nfnt/resize`, `anthonynsimon/bild`.
- EXIF/ICC: not in stdlib; `rwcarlsen/goexif`-class libraries are pure Go but the stdlib decoders
  ignore profiles.

### 1.10 purego, CCGO, wasm2go — the generic no-cgo interop toolbox

- [`github.com/ebitengine/purego`](https://github.com/ebitengine/purego) — "A library for calling C
  functions from Go without Cgo", via `dlopen`/`LoadLibrary` and assembly stubs; self-described as
  **beta software** ("expect bugs and potentially API breaking changes"). It is what lets Oto v3
  load `libasound.so.2` at runtime and what `gen2brain/avif` uses to prefer a system libavif.
  MyGo already depends on this pattern for `internal/darwin`/`internal/linux`.
- [`codeberg.org/puregotk/puregotk`](https://codeberg.org/puregotk/puregotk) — "Autogenerated GTK4 +
  Adwaita bindings for Go" built on purego/dlopen explicitly to avoid cgo's cross-compilation cost.
  Existence proof that a whole GUI toolkit can be bound without cgo.
- WASM route: `wazero` (pure-Go runtime) and `wasm2go`
  ([github.com/ncruces/wasm2go](https://github.com/ncruces/wasm2go)) — transpiles a `.wasm` module
  into Go source, so the C codec becomes Go code with no runtime interpreter.
- **CCGO** (compile C to Go, `eliasnaur.com/ccgo` / the Ebitengine fork) — **not verified in this
  pass**; it is the other classic route to "C library as Go source".
- [`github.com/bnema/purego-ffmpeg`](https://pkg.go.dev/github.com/bnema/purego-ffmpeg) — FFmpeg C
  API bindings generated from FFmpeg's C headers, **no cgo, `CGO_ENABLED=0`**, but
  **"Linux only ... early stage. API is not stable. Currently targets FFmpeg 7.x"**, and the README
  carries a WARNING. This is the closest thing to "FFmpeg without cgo" and it is not yet a dependency
  anyone should take.
- [`github.com/obinnaokechukwu/ffgo`](https://github.com/obinnaokechukwu/ffgo) — "Pure Go FFmpeg
  bindings without CGO. Decode, encode, transcode ... zero C dependencies at build time." Existence
  noted; **unverified maturity**.
- [`github.com/gen2brain/...`](https://github.com/gen2brain) as an author is the best signal in this
  space: gav1d (pure Go codec), avif/webp (cgo-free via purego-or-WASM), against malgo (cgo).

---

## Part 2 — Framework-by-framework

### 2.1 Electron

- **Audio playback**: Chromium's Web Audio API / `<audio>` / `<video>` inside the bundled Chromium.
  No Go-style host API; the renderer is a full browser engine, so Web Audio, AudioWorklet,
  MediaElement and the platform audio backends underneath Chromium all come for free.
- **Audio input / recording**: `getUserMedia` + `MediaRecorder` in the renderer (Web Audio / WebRTC
  stack), saved to file from the renderer.
- **Video playback**: HTML5 `<video>`; codecs are Chromium's, but **the shipping Electron binary's
  `libffmpeg` is built *with* proprietary codecs (H.264, AAC) — that is what "Electron ships with
  proprietary codecs" means, and distributions that forbid them must swap the library**.
  electron-builder exposes exactly that as a config option: "Whether to download the FFmpeg library
  variant from Electron's release assets and replace the default (**proprietary codec**) FFmpeg
  library before signing. The alternate FFmpeg is compiled without proprietary codec support
  (H.264, AAC, etc.) and is required for distributing on certain platforms (e.g. Linux
  distributions that prohibit proprietary ...)" —
  [electron.build configuration](https://www.electron.build/docs/configuration/). There is a
  dedicated packager plugin for the same swap
  ([electron-packager-plugin-non-proprietary-codecs-ffmpeg](https://github.com/amilajack/electron-packager-plugin-non-proprietary-codecs-ffmpeg)).
- **DRM**: Electron does **not** ship the Widevine CDM "for license reasons"; you must obtain it
  (historically by installing Chrome of a matching version) and point Electron at it with
  `--widevine-cdm-path`/`--widevine-cdm-version`
  ([Electron docs, using the Widevine CDM plugin](https://zeke.github.io/electron.atom.io/docs/tutorial/using-widevine-cdm-plugin/)).
  Castlabs maintains a fork — "Castlabs Electron for Content Security", `wvcus`/`wvvmp` — that
  integrates Widevine ([castlabs/electron-releases](https://github.com/castlabs/electron-releases)).
  **Any answer to "can MyGo do DRM video?" has to start here: the CDM is not redistributable, so no
  framework can bundle it.**
- **Screen capture / recording**: `desktopCapturer` (Electron API) feeding `getUserMedia` with a
  `chromeMediaSource: 'desktop'` constraint, plus `MediaRecorder`; on macOS this needs the Screen
  Recording TCC permission.
- **Camera**: `getUserMedia({video:true})` — i.e. the OS camera via Chromium, plus TCC/permission
  prompts.
- **Images**: Chromium's decoders (PNG/JPEG/WebP/AVIF/GIF/BMP/ICO), i.e. Skia + the same ffmpeg
  library.
- **Size / optionality**: the ffmpeg codec set is a *library swap at package time*, not a runtime
  plugin; Widevine is an external file. There is no feature-flag system — the engine is the feature.
- **Pain points**: proprietary-codec licensing and the ffmpeg swap; Widevine redistribution; heavy
  app size; native addons needing `electron-rebuild` against Electron's ABI.

### 2.2 Tauri v2

- **Audio**: nothing built in. The Rust side is a normal Rust program, so people add `rodio`/`cpal`
  (or play audio in the webview with `<audio>`/Web Audio). **Unverified**: no first-party Tauri
  audio API was found in the plugin list.
- **Audio input**: the webview's `getUserMedia`, gated by Tauri's permission system
  (`core:webview:allow-...`-style capabilities).
- **Video**: no built-in video beyond what the OS webview renders. This is the widely repeated
  complaint; a first-party media story does not exist.
- **Camera / mic**: `getUserMedia` inside the webview + Tauri permissions; mobile (iOS/Android)
  uses the platform webview as well.
- **Images**: whatever the OS webview supports (WKWebView/WebView2/WebKitGTK), plus Rust image
  crates on the backend.
- **Size / optionality**: the plugin system is the model — features are separate crates + npm
  packages, each with its own permission set, so an app that doesn't add the plugin doesn't pay
  ([Tauri plugin index](https://v2.tauri.app/plugin/) lists no audio/video/camera plugin).
- **Pain points**: no first-party multimedia at all; codec support is whatever three different
  webviews do; on Linux WebKitGTK's media stack (GStreamer) becomes a deployment dependency.

### 2.3 Wails v2 / v3

- **Audio / video**: delegated entirely to the webview — `<audio>`, `<video>`, Web Audio. There is
  no Go-side media API.
- **Known gaps**: [wailsapp/wails#456 "Can not use `<video>`"](https://github.com/wailsapp/wails/issues/456)
  (the old IE-based Windows webview could not play it), and
  [wailsapp/wails#3184 "How to play local audio files"](https://github.com/wailsapp/wails/issues/3184)
  — local-file playback requires serving the file (asset server / custom scheme) because the
  webview will not load `file://` media the same way. The current line replaces the old Windows
  webview with **WebView2**, which fixes most of #456's cause.
- **Wails v3 does ship a first-party permissions surface**: "Control camera, microphone,
  geolocation and other capability requests from web content ... Web content that calls
  `navigator.mediaDevices.getUserMedia()`, the Geolocation API, or the Notifications API needs the
  host application to grant or deny those requests. Wails exposes a cross-platform Permissions map
  on `WebviewWindowOptions`"
  ([v3.wails.io Features › Windows › Permissions](https://v3.wails.io/features/windows/permissions/)).
  **This is a pattern MyGo should copy** — see the webview permission handling MyGo already exposes
  in [permissions.go](../../permissions.go).
- **Camera**: `getUserMedia` through that permission gate.
- **Screen capture**: nothing first-party; `getDisplayMedia` in the webview where the platform
  webview supports it.
- **Pain points**: mp4/local-file gap, platform webview divergence, no Go-side media.

### 2.4 Neutralinojs

Minimal by design: a small C++/Node server + the OS webview, and **no media engine at all**. From the
[README](https://raw.githubusercontent.com/neutralinojs/neutralinojs/main/README.md): "Neutralinojs
doesn't bundle Chromium and uses the existing web browser library in the operating system (Eg:
gtk-webkit2 on Linux). Neutralinojs implements a WebSocket connection for native operations and embeds
a static web server to serve the web content." Its entire third-party library list is non-media
(websocketpp, cpp-httplib, Mbed TLS, nlohmann/json, `webview/webview`, `zserge/tray`,
portable-file-dialogs, base64, PlatformFolders, easyloggingpp, tiny-process-library, asio, clip,
infoware, efsw, postject, libtrashcan) — no decoder, no audio API. Media therefore lands as
user questions: [discussion #682 "playing video file from local filesystem"](https://github.com/neutralinojs/neutralinojs/discussions/682)
and [discussion #1408](https://github.com/neutralinojs/neutralinojs/discussions/1408) asking how to
disable the WebView2 **autoplay policy** — the same device-permission + autoplay-policy class of
problem MyGo hits in webview mode, and a reminder that "the webview does media" is not the same as
"the webview does media *usefully*".

### 2.5 Capacitor (mobile + Electron desktop)

- Web-standard APIs in the WebView (`<video>`, Web Audio, `getUserMedia`, `MediaRecorder`), plus a
  **plugin per capability**.
- **Camera, in detail** — from the
  [`@capacitor/camera` README](https://raw.githubusercontent.com/ionic-team/capacitor-plugins/main/camera/README.md)
  (v8.0.2; from 8.1.0 the plugin moved to `ion-team/capacitor-camera`):
  `Camera.getPhoto(ImageOptions) -> Photo`, `pickImages(GalleryImageOptions) -> GalleryPhotos`,
  `pickLimitedLibraryPhotos()`, `getLimitedLibraryPhotos()`, `checkPermissions()`,
  `requestPermissions(permissions?)`; `CameraResultType = Uri | Base64 | DataUrl`;
  `CameraSource = Prompt | Camera | Photos`; `CameraDirection = Rear | Front`; permission states are
  `'prompt' | 'prompt-with-rationale' | 'granted' | 'denied'` **plus `'limited'`**. Notable, and worth
  copying into MyGo's own permission vocabulary:
  - The result is a `Photo` with `base64String`, `dataUrl`, `path`, `webPath`, `exif`, `format`,
    `saved` — i.e. **the platform gives you a file/URI, not frames**.
  - iOS needs `NSCameraUsageDescription`, `NSPhotoLibraryUsageDescription` and
    `NSPhotoLibraryAddUsageDescription` in Info.plist.
  - "**iOS and Android only support jpeg. Web supports jpeg, png and gif**", and the `quality` option
    "is only supported on Android and iOS" — the classic case of a cross-platform media API that is
    really a lowest-common-denominator union with per-platform holes.
  - "because the Camera API launches a separate Activity to handle taking the photo, you should listen
    for `appRestoredResult` in the App plugin to handle any camera data that was sent in the case your
    app was terminated by the operating system while the Activity was running" — the OS can kill your
    process mid-capture, so a camera API must be **resumable**, not just a blocking call.
- Desktop target is an Electron shell, so Electron's ffmpeg/codec situation applies unchanged.

### 2.6 Qt 6 (Multimedia)

Source: [Qt Multimedia index](https://doc.qt.io/qt-6/qtmultimedia-index.html) (the Qt 6.12.0 page).

- **Scope**: "Qt Multimedia is an add-on module that provides a rich set of QML types and C++ classes
  to handle multimedia content ... an easy to use API for playing back audio and video files and
  rendering those on screen, as well as a comprehensive API for recording audio and video from various
  sources, including system cameras and microphones, **screen or window captures**, and audio or video
  memory buffers." Qt 6 is the most complete first-party multimedia surface of any framework surveyed:
  QML `MediaPlayer`, `CaptureSession`, `Camera`, `AudioInput`, `AudioOutput`, `VideoOutput`,
  `MediaRecorder`, `ImageCapture`, `Video`, `ScreenCapture`, `WindowCapture`; C++ `QMediaPlayer`,
  `QVideoWidget`, `QMediaCaptureSession`, `QCamera`, `QAudioInput`, `QAudioOutput`, `QImageCapture`,
  `QMediaRecorder`, `QVideoSink`, `QAudioSink`, `QScreenCapture`, `QWindowCapture`,
  `QVideoFrameInput`, `QAudioBufferInput`.
- **Audio/video playback**: `QMediaPlayer` + `QAudioOutput` + `QVideoWidget` (or `QVideoSink` to
  receive frames yourself). The last of those is the one that matters for an own-renderer framework:
  **`QVideoSink` hands you individual decoded video frames**, which is exactly the seam MyGo would
  need.
- **Backends**: "The main media backend, built on FFmpeg, ensures seamless cross-platform
  functionality, and is the default on all platforms except WebAssembly and embedded Linux/Boot2Qt.
  With Boot2Qt, the default backend is built on top of GStreamer." The FFmpeg backend "relies on the
  **FFmpeg 9.0.1** libraries, which are included with the Qt Online Installer ... The binary packages
  from the online installer use **dynamic linking** to FFmpeg. Therefore, applications must either
  bundle FFmpeg binaries in their installer or depend on FFmpeg being installed on the operating
  system." Native backends are kept only "for compatibility with existing applications":
  GStreamer on Embedded Linux; AVFoundation on macOS/iOS; "**Windows Media Foundation on Windows
  (deprecated since Qt 6.10)**"; MediaCodec on Android ("deprecated as of Qt 6.8 and will be removed
  in the next major release"); WebAudio/WebVideo on WebAssembly. "**New features will only be
  implemented on the FFmpeg media backend**, with the exception of WebAssembly." Switch with
  `export QT_MEDIA_BACKEND=darwin|windows|gstreamer|android|ffmpeg`, or cmake
  `QT_DEFAULT_MEDIA_BACKEND` at build time.
- **Screen/window capture**: [`QScreenCapture`](https://doc.qt.io/qt-6/qscreencapture.html) (since Qt
  6.5) and `QWindowCapture`. Documented limitations, verbatim: "**It is only supported with the FFmpeg
  backend.** On some platforms, **no new video frames are emitted while the captured screen content
  remains unchanged.** Applications should therefore **not rely on receiving a continuous stream of
  frames at the requested frame rate**." A capture API must therefore be *change-driven*, and a screen
  recorder cannot assume 30/60 fps. `QMediaCaptureSession`
  ([docs](https://doc.qt.io/qt-6/qmediacapturesession.html)) is the hub that wires a camera, an audio
  device, a screen/window capture, an `QImageCapture` (stills) and a `QMediaRecorder` (recording)
  together — a good shape for MyGo's own session object.
- **Licensing, verbatim**: "Note that video compression standards, such as the H.264 media compression
  standard, may be covered by patents and can incur royalty fees. **This can apply to any
  implementation, also if the implementation is provided as an operating system service, through a
  third-party library, or any of Qt Multimedia's backends. The Qt licenses do not cover such fees.**"
  Bundled third-party attributions include DR Libs 0.14.6, FFmpeg 9.0.1, Signalsmith Stretch, TLSF,
  boost, libjpeg, zlib.
- **Permissions**: "Starting from Qt 6.6, the Qt Multimedia module uses the new `QPermission` API to
  handle camera and microphone permissions. This means that **Qt itself no longer queries for these
  permissions, so this needs to be done directly from the client application.**"
- **Images**: `QImage`/`QImageReader` with plugin-based format support (`imageformats` plugins).
- **Size / optionality**: Qt Multimedia is a **separate module** whose FFmpeg/GStreamer backends are
  plugin libraries, so a Widgets-only app doesn't carry them — but an app that wants video gets a full
  FFmpeg. Note the Linux exception: on Linux/X11 the FFmpeg libraries are **not** deployed
  automatically by Qt's deployment tools.
- **Pain points**: backend churn across 6.x (FFmpeg became the desktop default; Windows/A​ndroid native
  backends are deprecated and slated for removal), GStreamer/plugin deployment pain on Linux,
  LGPL/GPL module split, the explicit patent-fee disclaimer, and the ARM-Linux hardware-acceleration
  problems the docs admit to.
- **From Go**: only via [`mappu/miqt`](https://github.com/mappu/miqt), which is **cgo** (and needs a
  Qt C++ toolchain) — Qt is not a path for MyGo, but it is the best-documented *reference design* for
  what a complete multimedia module looks like.

### 2.7 Flutter desktop

- **Audio/video**: not in the engine; it is plugins all the way down. The official
  [`video_player`](https://raw.githubusercontent.com/flutter/packages/main/packages/video_player/video_player/README.md)
  README's own summary line is "**A Flutter plugin for iOS, Android and Web**", and its support table
  lists exactly Android SDK 24+, iOS 13.0+, macOS 10.15+ and Web — **Windows and Linux are absent**.
  Backing players: AVPlayer (iOS/macOS), ExoPlayer (Android), the browser on Web. The README also
  documents `VideoViewType.platformView` vs texture view and warns that platform views may have
  correctness issues.
- **The desktop answer is [`media_kit`](https://github.com/media-kit/media-kit)** — "A cross-platform
  video player & audio player for Flutter & Dart" (MIT), deliberately split to "improve modularity &
  reduce bundle size": `media_kit` (core) + `media_kit_video` (rendering) + `media_kit_libs_video`
  **or** `media_kit_libs_audio`, and "**the `media_kit_libs_video` & `media_kit_libs_audio` packages
  should not be mixed**". Android/iOS/macOS/Windows/GNU-Linux/Web all get video and audio.
  Implementation: "**libmpv is used** for leveraging audio & video playback ... provides hardware
  acceleration & bundle size is also minimal (select only required decoders etc. in FFmpeg/mpv)";
  80%+ of the code is shared across platforms via `dart:ffi`. The rendering notes are the most
  directly relevant part of this whole survey for an own-renderer framework:
  - Windows: **ANGLE** to translate libmpv's OpenGL into Flutter's D3D11 texture ("requires DirectX 11
    or higher"), with a CPU pixel-buffer fallback.
  - macOS: render OpenGL into a `CVPixelBuffer` for **Metal** interop — the same bridge MyGo's Metal
    renderer would need.
  - Linux: both a GL-texture and a pixel-buffer path.
  - Android: `--hwdec=mediacodec --vo=mediacodec_embed --wid=<Surface>` through a JNI helper dylib.
  - Web: embed an HTML `<video>` element — i.e. **even media_kit falls back to the browser** there.
  - Maintainer's own war story: before Dart 3.1's `NativeCallable`, Dart FFI could not invoke native
    callbacks off-thread, so events needed an Isolate event loop and then a C++ `std::thread` helper
    package (`media_kit_native_event_loop`) because many simultaneous instances deadlocked. **The
    lesson transfers directly to MyGo: a media player is a callback-heavy, cross-thread native
    integration, and that is where the bugs are** (cf. AGENTS.md's "purego callbacks are a scarce,
    never-freed resource").
- **Camera**: the `camera` plugin (`CameraPreview`, `startImageStream`) is the first-party path but is
  mobile-first; `video_player_media_kit` exists solely to make `package:video_player` work on
  Windows/Linux/macOS with media_kit underneath — an admission that the official plugin never got a
  desktop backend.
- **Images**: Skia/Impeller decodes PNG/JPEG/WebP/GIF/BMP; the `image` Dart package for more.
- **Size / optionality**: every app carries the engine + Dart VM; plugins are pub packages, so the
  media stack is opt-in — but on desktop it is opt-in to a **libmpv + FFmpeg** build, which is where
  the tens of megabytes live.

### 2.8 .NET MAUI

Source: the CommunityToolkit's
[MediaElement docs](https://raw.githubusercontent.com/MicrosoftDocs/CommunityToolkit/main/docs/maui/views/MediaElement.md).

- **Audio/video**: `MediaElement` is "a control for playing video and audio", playing "media that's
  supported by the underlying platform" from a URI, an embedded resource (`embed://`), the app's local
  filesystem (`filesystem://`), or any `Stream`. Platform handlers, verbatim: **Android → ExoPlayer;
  iOS/macOS → AVPlayer; Windows → `Windows.Media.Playback.MediaPlayer`**. "MediaElement is available
  on iOS, Android, Windows, macOS, and Tizen" — **Linux is not a MAUI target at all**, so the
  cross-platform story excludes the platform MyGo cares most about.
- **Honest limitations the docs state**: "If the user is using a **Windows N edition**, no video
  playback is supported by default. Windows N editions have no video playback formats installed by
  design." And "the supported multimedia formats can be different per platform. In some cases it can
  even be dependant on **what decoders are available or installed on the operating system**."
- **API shape worth copying** (it is the mature shape of a managed media control): `Play`/`Pause`/
  `Stop`/`SeekTo`; `CurrentState = None|Opening|Buffering|Playing|Paused|Stopped|Failed`;
  `ShouldAutoPlay`, `ShouldLoopPlayback`, `ShouldKeepScreenOn`, `ShouldMute`,
  `ShouldShowPlaybackControls` (default true), `Aspect` (`AspectFit` default), `Speed`, `Volume` 0–1,
  `MediaWidth`/`MediaHeight`; events `MediaOpened`, `MediaEnded`, `MediaFailed`, `PositionChanged`,
  `SeekCompleted`. Note the explicit `AspectFit` default and the `HasVideo`-equivalent width/height —
  a player has to expose the frame's aspect ratio to the layout system, which is the detail MyGo's
  `ui` package would otherwise get wrong first.
- **Render surface**: `AndroidViewType.SurfaceView` (default) vs `TextureView`, with the same warning
  Flutter gives: "We do not recommend using `TextureView` unless you have a specific need for it. It
  has possible performance related issues."
- **Camera**: `MediaPicker.Default.CaptureVideoAsync()` → `FileResult` → `MediaSource.FromStream(...)`;
  stills and video go through the platform picker/camera app rather than a frame API. The separate
  `CommunityToolkit.Maui.Camera` `CameraView` is the in-app preview path.
- **Pain points**: thin first-party multimedia; Android/iOS first; **Linux absent**; codec support is
  the OS's.

### 2.9 Avalonia

**Correction to the common claim that Avalonia has no media player: it has one, and it is paid.**
[avaloniaui.net/media-player](https://avaloniaui.net/media-player/) advertises "Add cross-platform
video and audio playback to your Avalonia apps. Native hardware-accelerated media player component for
Windows, macOS, Linux, and mobile", and the docs guide
([Media playback](https://docs.avaloniaui.net/controls/media/mediaplayer/media-playback)) says "This
is a practical guide to implementing media playback in Avalonia applications using the **Avalonia Pro
MediaPlayer**" and "This control is available as part of **Avalonia Pro** or higher". That is itself
the headline lesson: for a self-rendering toolkit, video is expensive enough that the vendor put it
behind a commercial tier.

- **Backends per platform** (verbatim from the same docs): Windows "uses Windows's **Media Foundation**
  to render multimedia content, while utilizing **Vulkan** Graphics API whenever possible"; macOS/iOS
  "uses Apple's **AVFoundation**"; Android "uses Android's **ExoPlayer** component together with Vulkan
  Graphics API" (requires enabling `UseAndroidPlayer` in the app builder); Linux "uses the
  **system-installed LibVLC** library to render multimedia content for Linux distros. **Requires LibVLC
  3.0.21 or higher. The LibVLC plugins (demuxers, decoders and audio outputs) are required as well —
  LibVLC cannot create an instance without them**"
  (`sudo apt install libvlc5 libvlccore9 vlc-plugin-base vlc-plugin-video-output`). Embedded-Linux DRM
  is LibVLC too.
- **Codec expectations, verbatim**: "The media codecs that `MediaPlayer` supports will depend on the
  target platform's built-in codecs & additional plugins. **The safest assumption is that most
  platforms can support for video is MPEG-4 Part 10 - Advanced Video Coding or more commonly known as
  H.264, with MPEG-4 Part 14 or MP4 as video container. As for audio, the safe to assume supported
  codecs are MP3, AAC and WAV.**" — i.e. even the paid component's documented floor is the
  patent-encumbered codec, never AV1/VP9.
- **API**: `MediaPlayerControl`; `MediaSource` subclasses `UriSource` / `StreamSource` /
  `StorageFileSource`; `PlayAsync`/`PauseAsync`/`StopAsync`; `Position`, `Volume` (0.0–1.0),
  `IsMuted`, `Duration`, `HasVideo`, `IsSeekable`, and an `ErrorOccurred` event.
- **A lifecycle trap worth recording**: "`MediaPlayer` is not ready to accept a media source until the
  Avalonia UI has fully loaded. Setting the `Source` property too early (for example, in a `Window` or
  `UserControl` constructor) **will fail silently**."
- **Community route**: NuGet `LibVLCSharp.Avalonia` 3.10.2 plus libvlc —
  [AvaloniaUI/Avalonia discussion #10683](https://github.com/AvaloniaUI/Avalonia/discussions/10683)
  ("is there any way to play the video through Avalonia? I found a project, but it works only on
  Windows"). The recurring complaint is that `LibVLCSharp.Avalonia`'s `VideoView` renders **on its own
  separate window** and cannot be embedded in a `UserControl` — the classic failure mode when a native
  player owns its own surface instead of handing you frames.
- **`Avalonia.Media`** is 2D drawing/imaging (Skia), **not** a media player.
- **Lessons**: (a) the "own renderer + Skia" architecture MyGo shares with Avalonia gets **no** media
  for free; (b) the two shapes available are *embed a native player and let it own a surface* (cheap,
  but it breaks layout/z-order and, on Linux, drags in LibVLC) or *decode yourself and upload frames*
  (control, cost); (c) MyGo's `QVideoSink`-style alternative — owning the decoder and the frame — is
  the one Avalonia's paid component can't offer and pure-Go codecs can.

### 2.10 JavaFX

- `javafx.scene.media` provides `Media` / `MediaPlayer` / `MediaView`
  ([package summary](https://openjfx.io/javadoc/21/javafx.media/javafx/scene/media/package-summary.html)):
  "Provides the set of classes for integrating audio and video into Java FX Applications. **The primary
  use for this package is media playback.**" — i.e. JavaFX's first-party media module is
  **playback-only: no camera, no microphone, no capture, no recording** (contrast Qt, which has all of
  it, and MAUI, which at least has a picker).
- **Exact codec set, from the official table** (rare among these frameworks — most decline to
  enumerate):
  - Encodings: **AAC**, **MP3** (layers I/II/III, "file must contain at least 3 MP3 frames"), **PCM**
    audio; **H.264/AVC** and **H.265/HEVC** video.
  - Containers: AIFF (PCM), MP3, WAV (PCM), MP4 (H.264/AAC and H.265/AAC, `.mp4/.m4a/.m4v`), and
    **HLS** in MP2T (H.264/AAC), fMP4 (H.264/AAC and H.265/AAC), MP3-only and AAC-only flavours
    (`.m3u8`). Protocols: `FILE`, `HTTP`, `HTTPS`, `JAR`, HLS; HLS gives on-demand and live playlists
    with automatic bit-rate switching.
  - So JavaFX ships a **patent-encumbered-by-default** codec set (AAC + H.264, plus HEVC) and does
    *not* offer VP8/VP9/AV1/Theora — the same "safe assumption is H.264 + AAC" ceiling Avalonia's docs
    admit to, except JavaFX states it as a guarantee because it ships its own engine.
- **Backends**: a reading of the OpenJFX source tree states that the media module "relies on native
  backends to perform heavy-lifting tasks such as demuxing, decoding, and audio processing. On Linux
  and Windows, JavaFX utilizes a customized version of GStreamer (**GStreamer-lite**), while macOS uses
  the native **AVFoundation** framework. These backends communicate with the Java layer through a
  **JNI bridge**"
  ([DeepWiki, openjdk/jfx21u native media backends](https://deepwiki.com/openjdk/jfx21u/10.2-native-media-backends:-gstreamer-and-avfoundation)).
  Secondary source; the codec table above is consistent with "a bundled GStreamer-lite with a fixed
  plugin set" rather than the system GStreamer.
- **Why it matters to MyGo**: "`MediaView` is a `Node` that provides a view of `Media` being played by
  a `MediaPlayer`" — video is an ordinary **node in the same scene graph** as the rest of the UI, so
  it participates in layout, clipping and transforms like any other control. That is the closest
  analogue to what MyGo must build, and it is achieved by having the media engine **hand over frames**
  (or a GPU texture) to the UI layer rather than owning a window. The alternative — a native player
  with its own surface — is what breaks in Avalonia ([§2.9](#29-avalonia)).
- **API details worth stealing**: `AudioClip` for "low latency playback of short clips" (distinct from
  the streaming `MediaPlayer`); `AudioEqualizer` + `EqualizerBand` + `AudioSpectrumListener` (a
  spectrum callback, i.e. the visualization hook); `Track`/`AudioTrack`/`VideoTrack`/`SubtitleTrack`
  for track selection; `Duration`-typed `position`/`seek`; and an honest error model — **synchronous**
  errors surface as `MediaException` from the constructors, **asynchronous** ones set an `error`
  property *and* fire `onError` callbacks, and "some errors might be duplicated ... a `MediaPlayer`
  will propagate an error that it encounters to its associated `Media`, and a `MediaPlayer` to all its
  associated `MediaView`s". A media API needs two error channels, because decoding fails both at open
  time and mid-stream.
- **DRM**: none. **Screen capture**: none. **Camera**: none.

### 2.11 Fyne

**Correction: Fyne core ships no multimedia API at all — there is no `audio` package in Fyne.**
Checked three ways: `pkg.go.dev/fyne.io/fyne/v2/audio` returns **HTTP 404** ("Oops! We couldn't find
`fyne.io/fyne/v2/audio`"); `https://api.github.com/repos/fyne-io/fyne/contents/audio` returns
**404** `{"message":"Not Found"}`; and the API package listing (navigation rendered on
[docs.fyne.io/api/v2](https://docs.fyne.io/api/v2/)) enumerates `app`, `canvas`, `container`,
`data/binding`, `data/validation`, `dialog`, `driver{,/desktop,/embedded,/mobile,/software}`,
`fyne`, `lang`, `layout`, `storage{,/repository}`, `test`, `theme`, `widget` — **no audio, no video,
no camera**. (An earlier draft of these notes claimed Fyne has an Oto-based audio package; that was
wrong and is retracted here.)

- **Audio, video, camera, screen capture: all zero first-party.**
- **The official position** is a tracking issue:
  [fyne-io/fyne#449 "Multimedia support for fyne"](https://github.com/fyne-io/fyne/issues/449) —
  "Providing multimedia support for fyne such as playing back audios and videos which may take
  advantage of the available codecs in the platform. **This requires a media engine or similar.**"
  A separate issue, [#6037](https://github.com/fyne-io/fyne/issues/6037), asks for camera and audio
  recording.
- **Community answers are all GStreamer**, and they are the oldest and most instructive datapoint in
  this survey: [`metal3d/fyne-streamer`](https://github.com/metal3d/fyne-streamer) — "Gstreamer widgets
  for Fyne (audio, video)", whose own README admits "For the moment, Fyne-Streamer **doesn't provide
  an Audio widget** (but it will soon), although the video widget plays video sound correctly" (so
  even the community answer is video-first, audio-later); and
  [`sdassow/fyne-gstreamer`](https://codeberg.org/sdassow/fyne-gstreamer), presented as
  [Playing Videos With Fyne](https://conf.fyne.io/archive/2025/simon-lightning.pdf) at Fyne Conf 2025
  — years after the framework shipped.
- **The lesson for MyGo**: the community solution to "own-renderer framework needs video" is *bind the
  system GStreamer and blit decoded frames into your GL texture*. It is doable — GStreamer's decode
  path already ends in a texture/buffer, which is why it wins over libmpv here (see Flutter's ANGLE /
  CVPixelBuffer bridge in [§2.7](#27-flutter-desktop)) — but it stays third-party, arrives late, and
  drags a runtime GStreamer dependency into the app. MyGo should decide deliberately whether to own
  that or to make video a plugin, and the pure-Go codecs in Part 1 are what make a third option real.

### 2.12 Gio (gioui)

- **No multimedia at all.** Gio is an immediate-mode UI toolkit with OpenGL/Metal/D3D/Vulkan backends;
  it ships no audio, no video, no camera, no capture. Video, when done, is decoded elsewhere and
  uploaded as a texture; audio is likewise external.
- The ecosystem gap is acknowledged, not solved: the community extensions repo
  [`gioui-plugins`](https://github.com/gioui-plugins/gio-plugins) carries an open request,
  [issue #25 "Video Player"](https://github.com/gioui-plugins/gio-plugins/issues/25) — "Video/Music
  players are very common on websites and apps. I think multiple apps would like to play videos or
  even streaming videos from external source." Gio's own platform-integration add-ons are
  non-multimedia (notifications, storage, app lifecycle, secure storage, cross-platform permissions).
- **Relevance**: Gio is the closest architectural sibling to MyGo's `ui` package (own renderer, pure
  Go, no cgo), and it demonstrates both halves of the trade: total control, and **zero** media. MyGo's
  existing [`audio` package](../audio.md) already puts it ahead of Gio; the interesting question is
  whether MyGo wants to be the first no-cgo framework with a *video* story, which Gio, Fyne and
  winit/egui all declined to build.

### 2.13 SDL3

- **Audio**: `SDL_audio` (`SDL_AudioStream`, `SDL_OpenAudioDevice`, callback or
  `SDL_PutAudioStreamData`), with platform backends and PipeWire preferred on modern Linux.
- **Camera**: **`SDL_Camera` is new in SDL 3** — "This API lets apps read input from video sources,
  like webcams. Camera devices can be enumerated, queried, and opened. Once opened, it will provide
  `SDL_Surface` objects as new frames of video come in. These surfaces can be uploaded to an
  `SDL_Texture` or processed as pixels in memory." Permission handling is explicit
  (`SDL_GetCameraPermissionState`, `SDL_EVENT_CAMERA_DEVICE_APPROVED`/`_DENIED`), and the docs are
  blunt about hardware: cameras warm up and "it's not uncommon that a newly-opened camera will
  provide a couple of completely black frames", so drop the first several frames. **SDL cameras
  "only provide video as individual frames; they will not provide full-motion video encoded in a
  movie file format" and provide no audio**
  ([SDL3/CategoryCamera](https://wiki.libsdl.org/SDL3/CategoryCamera)). SDL 3.2 also added PipeWire
  camera support ([Phoronix](https://www.phoronix.com/news/SDL3-PipeWire-Camera-Capture)).
- **Video playback: not SDL's job.** SDL provides GPU textures and (in SDL3) an `SDL_GPU` API; the
  decoding is external (`ffmpeg`, `libavcodec`, or platform decoders). There is no `SDL_Video`-the-
  player.
- **Screen capture**: no SDL API.
- **Images**: `SDL_image` (satellite library) for PNG/JPEG/WebP/AVIF/etc.; core SDL has BMP.
- **Optionality**: satellite DLLs (`SDL_image`, `SDL_mixer`, `SDL_ttf`) are separate shared
  libraries, which is the "media as separate download/plugin" model in C.
- **Lesson**: SDL3's camera API is the best-documented example of what a *minimal, honest* media
  capability looks like — frames only, permission-aware, no container, no audio — and it is exactly
  the surface MyGo could expose without pretending to be a player.

### 2.14 Godot 4

- **Audio**: `AudioServer` with `AudioStreamPlayer`/`AudioStreamPlayer2D/3D` nodes and drivers
  (PulseAudio/ALSA/CoreAudio/WASAPI/Dummy). Ogg Vorbis and WAV built in; MP3 added later.
- **Video: exactly one royalty-free codec, plus GDExtension for everything else.** From the generated
  class reference: "`VideoStreamPlayer` — A control used for playback of `VideoStream` resources ...
  **Supported video formats are Ogg Theora (`.ogv`, `VideoStreamTheora`) and any format exposed via a
  GDExtension plugin.**"
  ([class_videostreamplayer.rst](https://raw.githubusercontent.com/godotengine/godot-docs/master/classes/class_videostreamplayer.rst)).
  The frame reaches the renderer through `get_video_texture() -> Texture2D`, i.e. **the same
  blit-the-decoded-frame-into-a-GPU-texture model as everyone else**. Properties:
  `audio_track=0`, `autoplay=false`, `buffering_msec=500`, `bus=&"Master"`, `expand=false`,
  `loop=false`, `paused=false`, `speed_scale=1.0`, `stream`, `stream_position`, `volume`,
  `volume_db=0.0`; methods `play`/`stop`/`get_stream_length`/`get_stream_name`/`is_playing`; signal
  `finished`. Documented warning: "On Web, video playback *will* perform poorly due to missing
  architecture-specific assembly optimizations."
- **Camera**: `CameraServer`/`CameraFeed` — "keeps track of different cameras accessible in Godot.
  These are external cameras such as webcams or the cameras on your phone." Verbatim limitation:
  "**This class is currently only implemented on Linux, Android, macOS, and iOS. On other platforms no
  `CameraFeed`s will be available**" — i.e. **no Windows camera**, from the most popular open-source
  game engine.
  ([class_cameraserver.rst](https://raw.githubusercontent.com/godotengine/godot-docs/master/classes/class_cameraserver.rst)).
  `monitoring_feeds` defaults false with a documented warning that "This has a performance cost, so
  only set it to true when you're actively accessing the camera"; two feeds per device are exposed
  (RGBA and YCbCr/planar variants: `FEED_RGBA_IMAGE`, `FEED_YCBCR_IMAGE`, `FEED_Y_IMAGE`,
  `FEED_CBCR_IMAGE`); API `add_feed`/`feeds`/`get_feed`/`get_feed_count`/`remove_feed`; signals
  `camera_feed_added`, `camera_feed_removed`, `camera_feeds_updated`.
- **Recording the engine's own output (Movie Maker)**: "Godot can record non-real-time video and
  audio from any 2D or 3D project. This kind of recording is also called **offline rendering**."
  Three built-in `MovieWriter`s: **OGV** (Theora video + Vorbis audio, "recommended", 4 encoding-speed
  levels, no transparency), **AVI** (MJPEG video + uncompressed audio, "currently limited to a file of
  4 GB in size at most") and a **PNG image sequence + WAV**. CLI:
  `godot --path /path/to/your_project --write-movie output.avi --resolution 1280x720 --fixed-fps 30`;
  custom writers via GDExtension.
  ([creating_movies.rst](https://raw.githubusercontent.com/godotengine/godot-docs/master/tutorials/animation/creating_movies.rst)).
  The documented warning is the single best argument against "our framework will record the screen":
  "**This feature is not designed for capturing real-time footage during gameplay. Players should use
  something like OBS Studio or SimpleScreenRecorder to record gameplay videos, as they do a much
  better job at intercepting the compositor than Godot can do using Vulkan or OpenGL natively.**"
  Note also that offline rendering is *deterministic and non-real-time* — it renders frame N and
  encodes, free of the compositor — which is a fundamentally easier problem than capture. MyGo's
  analogue is rendering a `ui.Window` off-screen to a video file; that is a *different feature* from
  screen recording.
- **Images**: `Image` with PNG/JPEG/WebP/SVG/TGA/BMP/KTX etc., plus compressed-texture formats.
- **Optionality**: export templates can be built with modules disabled (`disable_3d` and friends); the
  engine is statically linked, so a small export is a **build-time** decision, not a runtime plugin.
  The GDExtension video seam is the runtime escape hatch.
- **Lesson**: Godot picks **one royalty-free container/codec** (Ogg Theora) and makes "MP4 with H.264"
  somebody else's GDExtension — a defensible product decision, and evidence for how expensive the
  H.264 ask is. It also shows that a "capture my own output" feature and a "record the desktop"
  feature should not share a design.

### 2.15 Rust winit / egui / wgpu

- **winit**: windowing only. **No audio, no video, no camera.**
- **egui**: immediate-mode UI with *no* media; images are decoded by the `image` crate and
  uploaded as textures. Video means: bring your own decoder (`ffmpeg-next`, `gstreamer-rs`,
  platform APIs), then hand frames to `egui-wgpu`/`wgpu` as textures — the same YUV→RGB + texture
  upload problem MyGo has.
- **Audio in Rust**: `cpal` (cross-platform audio I/O, raw to the OS APIs) + `rodio` (playback on
  top of cpal) + `kira` (game audio). These are crates, not a framework feature.
- **Camera**: `nokhwa` (cross-platform camera capture, uses platform APIs/V4L2/MSMF/AVFoundation).
- **Lesson**: the whole Rust desktop-UI ecosystem has settled on "UI toolkit ships no media; the
  decoder is a crate you choose", with `cpal`/`rodio` as the de-facto audio layer — exactly Oto's
  role in Go.

### 2.16 The system-webview path — what the webview itself gives you (MyGo's other mode)

MyGo has two modes, and the second one is not free either. The webview's media engine is the *host
OS's*, so the story differs per platform, and on Linux it is a **runtime dependency the user must
already have**:

- **WebKitGTK / WPE (MyGo's Linux webview)**: "**The WPE and GTK ports depend on the GStreamer
  multimedia framework** for their multimedia-related features, such as video playback (with or
  without MediaSource Extensions and Encrypted Media Extensions), **WebRTC, WebAudio, WebCodecs and
  MediaRecorder**."
  ([docs.webkit.org — WebKitGTK and WPE WebKit: Multimedia](https://docs.webkit.org/Ports/WebKitGTK%20and%20WPE%20WebKit/Multimedia.html)).
  So on Linux, `<video>`, `getUserMedia`, Web Audio and `MediaRecorder` all work *only if* the
  user's system has the right GStreamer plugins (`gst-plugins-base/good/ugly/bad`, `gst-libav`;
  see the [GStreamer troubleshooting FAQ](https://gstreamer.freedesktop.org/documentation/frequently-asked-questions/troubleshooting.html)).
  H.264/AAC in WebKitGTK specifically needs the `ugly`/`libav` sets, which is where the patent
  question lands on distro packagers rather than on MyGo.
- **The user-visible symptom**, from Tauri's tracker:
  [tauri-apps/tauri#8579 "Can not play videos"](https://github.com/tauri-apps/tauri/issues/8579) —
  "Recently, after I updated webkit2gtk version to 2.42.4, my tauri app can not play videos. **A video
  player is rendered, but the video is not rendered.**" That is exactly the failure MyGo's webview
  mode inherits on Linux: the element and the controls appear, no frames arrive, and the cause is a
  missing/renamed GStreamer plugin outside the app's control.
- **Deployment weight**: [tauri-apps/tauri#6151](https://github.com/tauri-apps/tauri/issues/6151)
  records the `libwebkit2gtk-4.1` shared-object dependency at **4.4 GB** — the webview mode trades
  bundle size for "the OS provides the engine", and on Linux the OS sometimes provides it badly.
- **macOS (WKWebView)** and **Windows (WebView2)**: the OS webview brings the OS media stack
  (AVFoundation / Media Foundation + Edge's Chromium media stack), so `<video>`, `getUserMedia` and
  `MediaRecorder` are as good as the browser the user already has — *except* that Widevine/DRM and
  some codecs depend on the platform, and device permissions are the host app's to grant.
- **Autoplay policy and device permission are the same class of problem everywhere**:
  [neutralinojs#1408](https://github.com/neutralinojs/neutralinojs/discussions/1408) is a user asking
  how to disable the WebView2 autoplay policy, and Wails v3 shipped a whole permissions surface for
  the same reason ([§2.3](#23-wails-v2--v3)).
- **Lesson**: in webview mode MyGo should *document* the platform media dependency (Linux: GStreamer
  plugins; Windows N editions: no codecs) and expose a permission surface — and it should not promise
  that media "just works", because that is the exact claim whose failure produced Tauri #8579.
  In native-UI mode MyGo owns the whole stack, so the pure-Go codecs in Part 1 are the only way to
  make media work *without* asking anything of the system.

---

## Part 3 — Comparison table

Legend for the framework columns: **E**=Electron, **T**=Tauri v2, **W**=Wails v2/v3, **N**=Neutralinojs,
**C**=Capacitor, **Q**=Qt 6, **F**=Flutter desktop, **M**=.NET MAUI, **A**=Avalonia, **J**=JavaFX,
**Fy**=Fyne, **G**=Gio, **S**=SDL3, **Go**=Godot 4, **we**=winit/egui, **My**=MyGo today.
Rows are capabilities, as asked; the table is split in two so it stays readable (engine-based vs
own-renderer frameworks).

**Table 3.1 — frameworks whose UI *is* a web/browser engine** (media = the engine's, or a plugin's)

| Capability | E | T | W | N | C |
| --- | --- | --- | --- | --- | --- |
| **Audio playback** | Web Audio/`<audio>` (Chromium); ships H.264+AAC libffmpeg | none built in (`<audio>`/Web Audio in the OS webview, or `rodio`/`cpal` in Rust) | webview only | webview only | webview only |
| **Audio capture → file** | `getUserMedia` + `MediaRecorder`/AudioWorklet | webview + Tauri permission caps | webview + v3 `Permissions` map | webview (where available) | webview |
| **Video playback** | `<video>`; DRM/Widevine external | OS webview only; Linux needs system GStreamer | OS webview; local-file/custom-scheme gotcha (#3184) | OS webview; autoplay policy issue | OS webview |
| **Camera** | `getUserMedia` + TCC | `getUserMedia` + permissions | `getUserMedia` + permissions | `getUserMedia` where available | `@capacitor/camera` plugin (jpeg-only on iOS/Android; `appRestoredResult` resume) |
| **Screen capture** | `desktopCapturer` → `getUserMedia` → `MediaRecorder`; macOS Screen Recording TCC | `getDisplayMedia` where the webview supports it | `getDisplayMedia` (nothing first-party) | none | web only |
| **Image codecs** | Chromium (PNG/JPEG/WebP/AVIF/GIF/BMP/ICO) | OS webview + Rust crates | OS webview | OS webview | OS webview |
| **DRM** | Widevine **not shipped** (not redistributable); Castlabs fork | none | none | none | none |
| **How media stays optional** | not at all — the engine *is* the feature; codecs swapped by rebuilding/swapping `libffmpeg` at package time | plugin + per-plugin permission model (no media plugin exists yet) | none | none | one plugin per capability |
| **The one thing to steal** | — | plugin/permission economics | v3 permission surface | — | camera permission vocabulary incl. `limited`, resumable capture |

**Table 3.2 — frameworks that draw their own UI** (media = native engine, bundled codec, or nothing)

| Capability | Q | F | M | A | J | Fy | G | S | Go | we | My |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **Audio playback** | ✅ `QMediaPlayer`/`QAudioSink` | plugins (`just_audio`, `audioplayers`, media_kit) | `MediaElement` (ExoPlayer/AVPlayer/MediaPlayer) | ✅ paid `MediaPlayer`; Linux=LibVLC | ✅ `MediaPlayer`/`AudioClip` | ❌ none | ❌ none | ✅ `SDL_audio` | ✅ `AudioServer` | crates (`rodio`/`cpal`) | ✅ own mixer ([docs/audio.md](../audio.md)) |
| **Audio capture → file** | ✅ `QAudioSource` + `QMediaRecorder` | plugins (mobile-first) | `MediaPicker` (no frame API) | via paid player | ❌ playback-only | ❌ | ❌ | ✅ (`SDL_AudioStream`) | ✅ | crates | ⚠️ webview mode only (`getUserMedia`) |
| **Video playback** | ✅ `QMediaPlayer`+`QVideoSink`; **FFmpeg default**; platform backends deprecated | `video_player` = iOS/Android/Web **only**; desktop = `media_kit` (libmpv) | ✅ but **no Linux**; Windows N = none | ✅ paid; LibVLC on Linux | ✅ `MediaView` node; GStreamer-lite/AVFoundation; H.264/HEVC/AAC | ❌ only via community GStreamer | ❌ (frames uploaded as textures) | ❌ (bring ffmpeg) | ✅ **Ogg Theora only** + GDExtension | ❌ (bring your decoder + `wgpu` texture) | ⚠️ webview mode: OS webview (Linux: GStreamer) |
| **Camera** | ✅ `QCamera` | `camera` plugin (mobile-first) | `MediaPicker`/`CameraView` | ✅ via paid player | ❌ | ❌ | ❌ | ✅ `SDL_Camera` (frames, permission-aware) | ✅ **Linux/Android/macOS/iOS only** | `nokhwa` (crate) | ⚠️ webview mode only |
| **Screen capture** | ✅ `QScreenCapture`+`QWindowCapture` (**FFmpeg backend only**, change-driven, not constant fps) | ❌ | ❌ | ❌ (via player) | ❌ | ❌ | ❌ | ❌ | ⚠️ Movie Maker = **offline** render, explicitly *not* real-time capture | ❌ | ❌ |
| **Image codecs** | `QImageReader` plugins | Skia/Impeller + `image` pkg | OS codecs | Skia | `javafx.scene.image` | stdlib-ish via `canvas` | none built in | `SDL_image` (satellite DLL) | `Image` (PNG/JPEG/WebP/SVG/…) | `image` crate | `image/*`, `golang.org/x/image`, own decoders |
| **DRM / HW decode** | none / via FFmpeg, ARM-Linux problems documented | none / OS webview | none / OS | none / AVFoundation·MF·ExoPlayer | none / native backends | none | none | none / none | none / platform | none | — / purego-able (VideoToolbox, D3D11VA, VA-API) |
| **Bundle/optionality** | separate module + backend plugins; FFmpeg **not auto-deployed on Linux/X11** | pub packages = libmpv+FFmpeg when used | OS codecs = free; thin API | paid tier; Linux needs system LibVLC installed | bundled in the JavaFX distribution (fixed codec set) | none to carry | nothing to carry | satellite DLLs (the C "media as separate download" model) | static engine, modules cut at build time | crates you pick | `audio` core + side-effect decoder imports (`audio/vorbis`) |
| **Headline pain point** | backend churn; H.264 patent fees **"not covered by Qt's licences"**; Windows MF deprecated, Android MediaCodec to be removed | desktop plugins never shipped | no Linux; OS codec lottery | video is paid, and on Linux it's LibVLC | playback-only, no capture | multimedia issue open since #449; community = GStreamer | total control, zero media | honest but frames-only; no player | one royalty-free codec | no media | native path has no video yet |

**Table 3.3 — the "no-cgo Go" column** (what MyGo can have with `CGO_ENABLED=0`; detail + URLs in Part 1)

| Capability | Pure Go today | Maturity | Where it lives |
| --- | --- | --- | --- |
| Audio output | ✅ `ebitengine/oto/v3` (PulseAudio client in Go, `libasound.so.2` via purego, CoreAudio/`AudioToolbox`, WASAPI) | **production** | MyGo already owns its mixer |
| Audio capture (mic) | ❌ nothing | — | write WASAPI/ALSA/CoreAudio ourselves; pure-Go PulseAudio client proves Linux |
| Audio decode | ✅ MP3 (`hajimehoshi/go-mp3`), Ogg Vorbis (`jfreymuth`), FLAC (`mewkiz/flac`), WAV | **production** | side-effect imports, MyGo already does Vorbis |
| Audio encode | ⚠️ WAV only | — | AAC/Opus have no pure-Go decoder/encoder found |
| Image codecs | ✅ PNG/JPEG/GIF (stdlib), WebP **decode** (`x/image`), **WebP encode + VP8** (`gen2brain/vpx`), AVIF/AV1 stills (`gav1d`), TIFF/BMP | **production** | opt-in imports (`plugins/`, like `audio/vorbis`) |
| Video decode **AV1/AVIF** | ✅ `gen2brain/gav1d` (port of dav1d, bit-exact, SIMD, film grain, 8/10/12-bit) | **credible**, young | plugin; bitstream-level, needs demux |
| Video decode **VP8** | ✅ `gen2brain/vpx` (port of libwebp, byte-exact, SIMD) | **production** | plugin |
| Video decode **H.264** | ⚠️ `oops1/go.264` claims a *complete* High-profile decoder (bit-exact vs ffmpeg on 29 clips) + `Eyevinn/hi264` for IDR/thumbnails | **experimental** (single maintainer, README claims) | plugin, behind a build tag |
| Video decode **VP9 / H.265** | ❌ nothing | — | patents + no port exists |
| Video decode **MJPEG** | ✅ `image/jpeg` + your transport | **production** | the honest "video works today" path |
| Video encode | ⚠️ `oops1/go.264` (claims B slices/CABAC/NVENC/VA-API/MF), AV1 stills, MJPEG/PNG/GIF, or shell out to ffmpeg | **experimental** | plugin or external process |
| Camera | ⚠️ Linux V4L2 pure Go (`blackjack/webcam`, bytes not `image.Image`); macOS/Windows = write it (purego/`syscall`) | **half** | `plugins/camera` |
| Screen capture | ⚠️ Linux X11 + macOS ScreenCaptureKit (`go-macos/screencapture`, `LocalKinAI/sckit-go`) pure Go; Windows DXGI = write it; Wayland = portal+PipeWire (bigger) | **half** | `plugins/screen` |
| Native interop | ✅ `purego` (beta), `puregotk` (whole GTK4 bound), WASM on `wazero`, `wasm2go` transpile, `purego-ffmpeg` (Linux-only, unstable) | **production for dlopen-style use** | `internal/darwin`, `internal/linux` already |

**Bottom line of the tables**: audio playback and image codecs are *solved* without cgo; **AV1, VP8
and WebP are solved without cgo**; H.264 is claimed but unproven; VP9/H.265/AAC/Opus are not
available; camera and screen capture are half-done (Linux + macOS feasible today, Windows to write);
and no framework in the list — including the paid, commercial ones — offers the thing MyGo could:
**a self-rendering UI whose video path has no C media engine underneath it.**

## Part 4 — What MyGo should copy / avoid

*Grounding note: everything below is tied to a fact above, and every recommendation respects MyGo's
hard rules — `CGO_ENABLED=0`, "apps pay only for what they use" (with `examples/counter-native` and
`examples/hello` measured on every push), plugins in `plugins/<name>` as Go + npm pairs, purego
callbacks created once per signature at startup, and a 2.5 MB penalty for reaching
`reflect.Type.Method`/`MethodByName` from a common path.*

### 4.1 Five facts that decide the design

1. **Every framework that draws its own UI and ships real video does it by embedding a C media
   engine** — libmpv for Flutter/`media_kit` ([§2.7](#27-flutter-desktop)), LibVLC for Avalonia
   ([§2.9](#29-avalonia)), GStreamer-lite for JavaFX ([§2.10](#210-javafx)), FFmpeg for Qt
   ([§2.6](#26-qt-6-multimedia)), GStreamer for the Fyne community ([§2.11](#211-fyne)). The two
   exceptions are Godot, which ships **one royalty-free codec** and makes the rest a GDExtension
   ([§2.14](#214-godot-4)), and **MyGo's own option: pure-Go codecs**, which no framework in this
   survey has.
2. **The ones that work hand frames or textures to the UI layer; the ones that don't are the ones
   users complain about.** JavaFX's `MediaView` is a *node*; Qt exposes `QVideoSink`; Godot exposes
   `get_video_texture()`; Flutter's `media_kit` bridges into D3D11/Metal textures; `LibVLCSharp.Avalonia`
   renders on its **own window** and cannot be embedded in a `UserControl` — that is the anti-pattern.
3. **Media is where bundle size, licences and OS permissions collide at once.** Qt's docs say the
   licences "do not cover" H.264 patent fees; Electron's packagers carry a whole ffmpeg-swap
   mechanism for proprietary codecs; MAUI documents that **Windows N editions have no video codecs at
   all**; Widevine is not redistributable so *no* framework ships DRM.
4. **"The webview does media" is a claim that fails on Linux.** WebKitGTK delegates video, WebAudio,
   WebRTC and MediaRecorder to **system GStreamer**
   ([docs.webkit.org](https://docs.webkit.org/Ports/WebKitGTK%20and%20WPE%20WebKit/Multimedia.html)),
   which produced Tauri [#8579](https://github.com/tauri-apps/tauri/issues/8579) ("a video player is
   rendered, but the video is not rendered") and a 4.4 GB `libwebkit2gtk` dependency.
5. **Media is never one feature.** Playback, capture, container muxing, recording to file, and
   recording *the screen* are five different engineering problems with five different cost curves —
   Godot's own docs tell users to use OBS rather than the engine for the last one.

### 4.2 What to copy

- **Copy the plugin economics of Tauri v2 and the permission surface of Wails v3.** Tauri's official
  plugin index contains **no** media plugin, so every capability is an explicit crate + npm package +
  permission set. Wails v3's `Permissions` map on `WebviewWindowOptions` is a first-party,
  cross-platform gate for `getUserMedia`, geolocation and notifications
  ([v3.wails.io](https://v3.wails.io/features/windows/permissions/)). MyGo already has
  [`permissions.go`](../../permissions.go); the media work should *extend that surface*, not invent a
  parallel one, and every media feature should live in `plugins/<name>` so
  `examples/counter-native`/`examples/hello` don't grow.
- **Copy Qt's capture-session shape and its frame seam.** One session object that wires together a
  camera, an audio input, a screen/window capture, a still-image capture and a recorder
  ([`QMediaCaptureSession`](https://doc.qt.io/qt-6/qmediacapturesession.html)) is the right factoring,
  and **`QVideoSink` — hand the app decoded frames — is the seam MyGo needs**, because MyGo's own
  renderer is the consumer.
- **Copy `media_kit`'s split, including the "do not mix" rule.** `media_kit` / `media_kit_video` /
  `media_kit_libs_video` **or** `media_kit_libs_audio` ("should not be mixed") is precisely MyGo's
  "apps pay only for what they use" idea applied to media: an app that only plays audio must not link
  a video decoder. Mirror it with separate opt-in imports, e.g. `plugins/audio/vorbis`,
  `plugins/video/av1`, `plugins/video/h264`.
- **Copy SDL3's honesty about camera.** `SDL_Camera` gives **frames**, is permission-aware
  (`SDL_GetCameraPermissionState`, `SDL_EVENT_CAMERA_DEVICE_APPROVED/_DENIED`), warns that the first
  frames are black, and states outright that it provides "individual frames" and no encoded movie and
  no audio ([SDL3/CategoryCamera](https://wiki.libsdl.org/SDL3/CategoryCamera)). A `plugins/camera`
  that returns frames + a permission state + a documented drop-first-frames rule is achievable and
  honest; a "camera" that pretends to record MP4 is not.
- **Copy JavaFX's API ergonomics**: a separate low-latency `AudioClip` path distinct from the
  streaming `MediaPlayer`; a spectrum/visualization callback (`AudioSpectrumListener`); explicit
  track lists; and **two error channels** — synchronous open failures *and* asynchronous
  `error`/`onError` for mid-stream failures, because decode errors arrive both ways
  ([package summary](https://openjfx.io/javadoc/21/javafx.media/javafx/scene/media/package-summary.html)).
- **Copy MAUI's control surface for the widget**: `CurrentState` enum
  (`None|Opening|Buffering|Playing|Paused|Stopped|Failed`), `ShouldAutoPlay`/`ShouldLoopPlayback`/
  `ShouldMute`/`ShouldShowPlaybackControls`, `Aspect` defaulting to fit, `Volume`, and
  `MediaWidth`/`MediaHeight` so layout knows the frame's aspect ratio
  ([MediaElement docs](https://raw.githubusercontent.com/MicrosoftDocs/CommunityToolkit/main/docs/maui/views/MediaElement.md)).
  That is the mature shape of a media control, and it is cheap to design in from the start.
- **Copy Godot's posture — one royalty-free codec in the core, everything else an extension** — with
  a twist that only pure Go allows: MyGo's "core codec set" can be **AV1 (`gav1d`), VP8/WebP (`vpx`),
  Ogg Vorbis and WAV**, i.e. royalty-free *and* not requiring a C engine. MP4/H.264/AAC then become
  an explicitly optional plugin with an explicit licence warning, exactly as Qt and Avalonia
  document theirs.
- **Copy the `purego` + `//go:embed` + Objective-C shim distribution pattern** described in
  [LocalKinAI's embedded-dylib write-up](https://www.localkin.dev/papers/embedded-dylib) for the
  macOS ScreenCaptureKit/camera bindings — it is the same idea as MyGo's terminal plugin loading
  `libghostty-vt` through purego with release-asset pins (`mygo-plugin.json`), and MyGo's
  `internal/darwin` already does the call half.
- **Copy `go.264`'s hardware-acceleration pattern**: load the platform encoder (Media Foundation,
  NVENC, VA-API) at run time through purego/dlopen, and offer a build tag
  (`go264_nohwaccel`) that removes the backends entirely so the binary stays static — the same
  discipline as `mygo_noinspector`. Note the documented cost: *purego alone* makes a Linux binary
  dynamically linked against glibc even with `CGO_ENABLED=0`
  ([oops1/go.264 README](https://raw.githubusercontent.com/oops1/go.264/master/README.md)).
- **Copy Avalonia's and Qt's habit of documenting per-platform prerequisites and codec expectations
  up front** ("requires LibVLC 3.0.21 or higher; the plugins are required as well"; "the safest
  assumption is H.264 + MP4"). MyGo should say the same thing about webview mode on Linux (system
  GStreamer), and about which codecs native mode supports natively.
- **Copy Qt's change-driven capture contract**: "no new video frames are emitted while the captured
  screen content remains unchanged. Applications should therefore not rely on receiving a continuous
  stream of frames at the requested frame rate"
  ([QScreenCapture](https://doc.qt.io/qt-6/qscreencapture.html)). Frame-rate-independent capture is
  both the honest API and the cheaper implementation.

### 4.3 What to avoid

- **Don't build a player that owns its own native surface.** `LibVLCSharp.Avalonia`'s separate window
  breaks embedding and z-order ([§2.9](#29-avalonia)); MyGo's `ui` package must consume **frames**
  (CPU buffers or GPU textures), never a foreign window.
- **Don't put H.264/AAC/HEVC in core, and don't ship DRM.** Qt's licence text ("may be covered by
  patents and can incur royalty fees ... The Qt licenses do not cover such fees") and Electron's
  ffmpeg-swap apparatus are two independent proofs that this is a licensing problem, not an
  engineering one; Widevine is not redistributable at all. Core should be royalty-free codecs
  (AV1/VP8/Vorbis/WAV); H.264/H.265 support is opt-in, clearly labelled, and preferably *not shipped
  by MyGo at all*.
- **Don't promise "video" as a single feature, and don't promise desktop recording.** Godot's
  Movie Maker is explicitly offline and non-real-time, and its docs tell players to use OBS; Qt's
  capture is change-driven. If MyGo ever records, it must use the OS capture API
  (ScreenCaptureKit/DXGI/portal) and be frame-availability-driven — and "render a `ui.Window`
  off-screen to a file" should be a *separate*, much easier feature (Godot's PNG/OGV sequence model
  is the template).
- **Don't take a cgo dependency for camera even though good ones exist** (`hybridgroup/gocv`,
  `gen2brain/malgo`, `gordonklaus/portaudio` — all cgo, [§1.4](#14-camera-capture-from-go)). The
  V4L2 path in pure Go (`blackjack/webcam`) and the purego ScreenCaptureKit bindings prove the shape.
- **Don't reach media code from `App.Run`, window startup, `init` functions or the default menu
  path**, and never through reflection — the size rule in AGENTS.md makes media the single most
  likely way to blow up `examples/hello`. Register it through functions a constructor calls, like
  `linkedDocuments`.
- **Don't assume the webview gives you media.** On Linux it depends on the user's GStreamer plugin
  set (Tauri #8579); autoplay policy and device permissions are host-app responsibilities
  (neutralinojs #1408; Wails v3's permission map).
- **Don't ship a young codec as a core dependency.** `oops1/go.264`'s completeness is a README claim
  from a single maintainer (no independent conformance run here), and `hi264` is IDR-only. Put both
  behind a plugin boundary and a build tag; keep the core to codecs with multiple independent users
  (`gav1d`, `vpx`, `oto`, `go-mp3`, `oggvorbis`, `flac`).
- **Don't under-scope decode as "just the codec".** `gav1d` decodes **OBUs/temporal units**, not
  files; MP4/WebM/Ogg demuxing, sample tables, seeking, and the audio/video clock are the other half
  of a player — `Eyevinn/mp4ff` is the pure-Go starting point ([§1.3](#13-container-and-bitstream-parsing-pure-go-useful)).
- **Don't chase VP9 or H.265** (no pure-Go decoder; patent-encumbered) and don't chase Opus/AAC
  encoding. The royalty-free, pure-Go complete set is AV1 + VP8 + Vorbis + WAV + PNG/JPEG/GIF/WebP.
- **Don't use a WASM-transpiled C codec on the hot path** unless it is the only option: `gen2brain/avif`
  and `webp` are excellent as *fallbacks* behind a pure-Go decoder (`gav1d`, `vpx`), and their own
  READMEs point at the pure-Go versions. Prefer the pure-Go port; keep the WASM route for formats
  nobody has ported.

### 4.4 A sequencing that fits the constraints

Ordered by cost, each step shippable and each behind opt-in imports in `plugins/`:

1. **Now — posture and permissions.** Document this research's conclusions in the architecture guide;
   extend [`permissions.go`](../../permissions.go) so webview-mode camera/microphone requests have an
   explicit host-app gate (Wails v3's map is the model), and document the Linux webview caveat
   (system GStreamer) and the Windows-N-edition caveat.
2. **Images and animation first (cheapest "video").** Register the pure-Go decoders (`gen2brain/vpx`
   for WebP, `gav1d` for AVIF) as side-effect imports exactly like `audio/vorbis`, and expose
   animated GIF/WebP as a `ui` widget. For a large class of apps this *is* video, and it needs no
   container, no clock and no audio sync.
3. **Audio completeness.** Add FLAC (`mewkiz/flac`) and WAV to the side-effect-import set; keep the
   existing mixer as the only output path. AAC/Opus stay out.
4. **Camera frames (`plugins/camera`).** Linux V4L2 (pure Go, per `blackjack/webcam`), macOS
   AVFoundation via `purego` + the `internal/darwin` objc helpers, Windows Media Foundation via
   `syscall` COM — API shaped on `SDL_Camera`: enumerate, open with a permission state, deliver
   frames, document dropping the first frames, no audio, no container.
5. **Screen capture (`plugins/screen`).** macOS ScreenCaptureKit (copy `go-macos/screencapture` /
   `LocalKinAI/sckit-go`), Windows DXGI Desktop Duplication, Linux X11 first and portal+PipeWire
   later. Change-driven, not constant-fps.
6. **Video playback (`plugins/video`, last).** AV1 (`gav1d`) + VP8 (`vpx`) + MJPEG, demuxed with
   `mp4ff`/hand-written container code, frames uploaded to the renderer's texture path (Metal/D3D11/
   GL/WebGL2), audio through the existing mixer, paced by the audio clock. H.264 (`oops1/go.264`,
   and `hi264` for thumbnails) behind a separate opt-in import and marked experimental; hardware
   decode via purego (VideoToolbox/D3D11VA/VA-API) as a later, opt-out-taggable addition.
7. **Never in core**: bundled FFmpeg, H.264/AAC/H.265, DRM, a real-time desktop recorder.

### 4.5 Open questions to settle before building

- **Colour conversion and frame upload**: which of Metal/D3D11/GL/WebGL2 gets a YUV shader vs a CPU
  convert; `media_kit`'s ANGLE-to-D3D11 and OpenGL-to-`CVPixelBuffer` bridges are the precedents.
- **Is hardware decode worth the dynamic-loading complexity** when `gav1d` (AV1, SIMD, bit-exact) and
  `vpx` (VP8) are pure Go and `go.264` already ships a purego hardware path? Decide per platform.
- **Threading and callbacks**: a decode loop is a goroutine plus a ring buffer, never a purego
  callback per frame — the callback budget rule in AGENTS.md makes the latter a non-starter.
- **The browser target**: `GOOS=js GOARCH=wasm` must not link a WASM-transpiled codec; the browser
  build should use the browser's own decoders, which means the API must have a platform-shaped
  implementation from day one (`internal/web`).
- **Which container first**: MP4 (`mp4ff`) is the most useful; WebM/AV1 is the most licence-clean;
  Ogg is the most trivial to demux but has no pure-Go Theora decoder, so its video value is low even
  though MyGo already decodes Ogg Vorbis.
- **Frame pacing**: MyGo owns the mixer, so the audio clock exists — a video player must slave to it
  rather than to a timer, or it will drift and stutter exactly where the frameworks above get bug
  reports.
