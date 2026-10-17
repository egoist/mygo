# MyGo 多媒体能力：计划与进度

范围（Q11，v1）：**播放 + 采集 + 编码落盘**；不做音视频通话/直播推流。三平台同为完整面（macOS /
Windows / Linux），wasm 与其他平台只要还能编译、构造时返回结构化错误即可（Q12）。

## 基本决策

| 编号 | 决策 |
| --- | --- |
| Q2 | 平台解码器为界：macOS AVFoundation/VideoToolbox、Windows Media Foundation、Linux GStreamer；不打包媒体引擎，把系统栈当「带时戳的采样源」 |
| Q5 | Linux 一等公民：用系统 GStreamer、运行时不打包 codec、capability probing、缺失时 `errors.Is(err, errors.ErrUnsupported)`、文档给发行版安装建议 |
| Q6 | v1 走 CPU-backed RGBA/BGRA；播放由平台转换，相机在 macOS 上必须按原生格式收、由 Go 转（见 M4），`media.Frame` 从第一天抽象像素格式与存储形态 |
| Q7 | `NextFrame(ctx)` 是唯一内核交付语义；`Frames`/`Handle` 是便利封装；有界队列 + DropOldest（live 源）；文件源 pull 可真正 block 生产者做背压；不做 pacing |
| Q8 | 平台只当编码器，容器由纯 Go mux 写（mp4ff / ebml-go） |
| Q9 | 新建顶层包 `github.com/egoist/mygo/media`，不经过 `internal/platform.Backend` |
| Q10 | media 自带三平台互操作层，不动 `internal/{darwin,windows,linux}` |
| Q13 | macOS 12.3 为下限，能力按 API availability 分层 |
| Q14 | 录制默认组合：macOS/Windows MP4+H.264+AAC；Linux WebM+VP9+Opus |
| Q15 | Linux 屏幕捕获：X11/XWayland 用 `ximagesrc`，Wayland 用 XDG ScreenCast portal + `pipewiresrc`，缺失则结构化 ErrUnsupported，绝不黑帧 |
| Q16 | `media` 与 `ui` 互不 import；适配层放 `media/mediaui` |
| Q17 | 权限用 `PermissionStatus` + 每对象 `Permission()/RequestPermission()` + `OpenPermissionSettings()` |
| Q18 | audio 补齐采集；media 只做容器/编解码；主时钟 = 音频设备（`audio.Clock`） |

## 里程碑

| | 内容 | 状态 |
| --- | --- | --- |
| M1 | macOS 文件播放（AVFoundation，含 `audio.Clock`、`mediaui`、`examples/video-player`、e2e） | 完成 |
| M2 | Windows 文件播放（Media Foundation） | 完成（真机 14 测试通过） |
| M3 | Linux 文件播放（GStreamer）+ capability probing | 完成（容器 15 测试通过；`WithVideo(false)` 三平台行为一致） |
| M4 | 相机采集（AVCaptureSession / MediaCapture / GStreamer v4l2src） | 完成（Windows 真机 23/23；Linux 容器 23 通过 + 3 无摄像头跳过、`videotestsrc` 钩子测试全通过；macOS 真机 22/22，真实 1920x1080 帧，原生 `2vuy` 在 Go 里转 RGBA） |
| M5 | 屏幕捕获（ScreenCaptureKit / Graphics Capture / pipewiresrc+ximagesrc） | 未开始 |
| M6 | 编码与 Recorder（VTCompressionSession / MFT / GStreamer appsink + 纯 Go mux） | 未开始 |
| M7 | 文档、示例、发行版安装说明、权限 API 收尾 | 未开始 |

## 已定不变量

- **no-cgo**：全部经 `purego`/`syscall`；先例 `plugins/sqlite`（Zig 编译）、`plugins/terminal`
  （libghostty-vt）、`plugins/game/gamepad`。
- **体积**：`examples/counter-native` 与 `examples/hello` 每次 push 逐字节对比；media 不在两者依赖里
  （当前 9,031,522 B / 9,271,522 B 与基线一致）。
- **purego 回调**：每个签名启动建一次，按 user data 路由，spec 不可释放（GStreamer pad-added 已是此形态）。
- **`Source` 读什么就解码什么**：`WithVideo(false)`/`WithAudio(false)` 是「不读就不要解码」。Linux 的
  push 管线里，没人读的分支会把整个文件拖住，所以只想要声音的 app 必须说。

## 平台实现要点

### macOS（`media/media_darwin.go` + `media/internal/darwin/darwin.go`）
每一轨一个 `AVAssetReader`，`kCVPixelFormatType_32BGRA` 转 RGBA，音频用 LPCM 48k/2ch/float32；
`CMTime`/`CMBlockBuffer`/`CVPixelBuffer` 全部手写结构 + typed `objc_msgSend` 发送器。
踩过的坑：`AVLinearPCMIsNonInterleavedKey` 只是宏（真符号 `AVLinearPCMIsNonInterleaved`）；
`[AVAssetReader addOutput:]` 返回 void；`assetReaderTrackOutputWithTrack:` 返回 autoreleased 要 `Retain`；
AAC 轨 subtype 是 `'aac '` 不是 `'mp4a'`；autorelease pool 必须 `LockOSThread`。

### Windows（`media/media_windows.go` + `media/internal/windows/{com,mf}.go`）
COM vtable 索引 **0 基**（`QueryInterface=0`、`AddRef=1`、`Release=2`）；按 1 基编号时 `SetUINT32` 会调到
`SetUINT64`，`MFCreateSourceReaderFromByteStream` 报 `E_INVALIDARG 0x80070057`。每线程一次
`CoInitializeEx(MTA)` 用 TLS 标记；`MFStartup` 不配 `MFShutdown`。同一 MP4 上 MF 的流顺序是音频在前，
所以测试从 `Tracks()` 推导索引，不硬编码 0/1。

### Linux（`media/media_linux.go` + `media/internal/linux/gst.go`）
`playbin` + 每路一个 `videoconvert/audioconvert ! capsfilter ! appsink`（`sync=false`，不 drop，
用队列做背压）；appsink 不接信号，只有被 pull 才产出，所以解码发生在调用者 goroutine 上。
探轨用 `filesrc ! parsebin`（每个新 pad 接一个 `fakesink`，否则 demuxer 会在第一个没人读的 pad 上
报 not-linked 而停住），pad 稳定 150ms 即认为流已出齐。
坑：`gst_buffer_get_pts`/`gst_message_type` 不存在（只有宏），只能按偏移读（`GstBuffer` 112 字节、
pts=72；`GstMessage` 120、type=64）；`gst_filename_to_uri` 会把相对路径解析成绝对路径；
`gst_uri_has_protocol` 需要两个参数，不能用来判断「是否 URI」。
**最容易亏的一处**：`unsafe.String` 只是别名不复制，读完 `g_free` 就悬垂——`goString` 必须用
`string(unsafe.Slice(p, n))` 复制。

### 相机（M4）
公共面：`media.Cameras()` / `media.OpenCamera(id)` / `Camera.NextFrame(ctx)`，相机和文件一样是
`media.Source`（`mediaui.NewVideo` 两者通用）；`ErrNoCamera`、`ErrPermissionDenied`，以及
`CameraPermission()/RequestCameraPermission(ctx)/OpenCameraPermissionSettings()`。
live 源用 `liveFrames`：4 帧有界队列，生产者永不阻塞、满了丢最旧，`NextFrame` 无帧时等唤醒或 ctx 结束。

- macOS：`AVCaptureSession` 跑在自建 dispatch queue 上，delegate 回调里把 pixel buffer 拷成一帧，权限由 TCC
  回答。**采集输出不能配置 `videoSettings`**：本机 macOS 26 上，只要给 `AVCaptureVideoDataOutput` 设了
  `videoSettings`（无论 BGRA 还是原生 `2vuy`），AVFoundation 就在自己的 render 线程里
  `BUG IN CLIENT OF LIBPLATFORM: Trying to recursively lock an os_unfair_lock` 然后 abort（EXIT 137）；
  同一台机器上 ffmpeg 请求 `-pixel_format rgba/bgra` 也是 `Error opening input: Input/output error`，
  只有 `uyvy422` 能开。所以这条路径不请求任何格式，按相机原样收帧（实测原生是 `2vuy`，1920x1080、
  stride 3840），由 `media/camera_darwin_pixels.go` 在 Go 里转成 RGBA：`2vuy` 按 UYVY 4:2:2 解，
  `420v/420f` 按双平面 4:2:0 解，32BGRA/32RGBA 直接翻；高度 ≥720 用 BT.709，否则 BT.601
  （换算有 `camera_darwin_pixels_test.go` 的单测，含全范围 420f）。相机时戳是机器自己的时钟，`deliver`
  用第一帧的时戳把它挪到相机起点；首帧常常全黑（ISP 还没收敛），属于相机行为，不是转换问题。
- Windows：Media Foundation `IMFSourceReader` 的 `MF_SOURCE_READER_FIRST_VIDEO_STREAM`，RGB32；
  `DefaultStride` 为负表示自下而上，`pictureOf` 负责翻正。**`Close` 必须先 `Flush(StreamFirstVideo)`
  再 `ShutdownObject`**：正在等帧的 `ReadSample` 在停止的设备上不会自己返回——真机上撞到过，另一个进程
  占着摄像头时关闭相机，goroutine 卡在 `ReadSample`，整个 test binary 超时。摄像头被别的 app 占用时
  `ReadSample` 返回 `0xc00d3704`（MF_E_VIDEO_DEVICE_LOCKED），表现为快速失败而不是挂住。
- Windows 权限：`CameraPermission()` 在打开设备前只能是 `PermissionNotDetermined`（Windows 不提前告诉
  应用答案），`RequestCameraPermission` 就是去开第一台设备，成功即 Granted；设置页
  `ms-settings:privacy-webcam`。测试 helper 的规则因此是：`PermissionDenied/Restricted` 跳过；
  `NotDetermined` 只在 darwin 跳过（打开会弹 TCC 对话框），Windows 继续开。
- Linux：`v4l2src device=… ! videoconvert ! video/x-raw,format=RGBA ! appsink sync=false
  max-buffers=2 drop=true`；缺 GStreamer 时 `MissingComponentError{Component:"GStreamer",
  Feature:FeatureCamera}`；权限 `PermissionNotRequired`。容器/CI 里没有摄像头，包内 `cameraTestSource`
  钩子让测试改用 `videotestsrc is-live=true` 走真实管线（覆盖正常出帧、`num-buffers` 后报错结束、
  缺元素报 `errors.ErrUnsupported`）。
- 示例：`examples/camera`（`-list`、`-camera id-or-name`）。

macOS 真机采集已补上：系统设置 > 隐私与安全性 > 相机里给 Terminal 打勾后，从 Terminal 跑
`go test -c -o /tmp/media-darwin.test ./media` 再执行该二进制，本机 26 个用例全过（含
`TestPicturesComeOutAsFrames` 1.7s 真实 1920x1080 帧、`TestCloseEndsThePictures`，以及 4 个像素换算单测）。同一份二进制在
DSH Desktop 里跑会因为没有 `com.apple.security.device.camera` 被 TCC 拒绝（tccd 不给弹窗），相机用例跳过。
上面「不能配置 `videoSettings`」的结论就是这次用临时探针（`media/internal/camprobe`，已删除）逐项试出来的：
跳过 `setVideoSettings:` 才活下来，并且把收下来的帧转成 PNG 与原生长相一致。

## 测试与验证通道

- macOS：本机 `go test ./media/...`（相机用例要 Terminal 有相机权限）；GUI 闭环
  `MYGO_E2E=1 go test ./internal/e2e -run TestVideoInAWindow`。本机 e2e 全量里
  `TestLocaleAtDocumentStart`/`TestPlugins`/`TestSQLitePlugin` 这 3 个 JS-bridge 用例在干净 HEAD 上也失败
  （DSH Desktop 环境问题，与本任务无关，已用 `git stash -u` 在干净树上复现）；media 相关 e2e 通过。
- Linux：colima 容器 `mygo-linuxtest`（Debian 13 + Go 1.27，仓库只读挂 `/src`，已装 GStreamer 1.26.2）；
  本机 `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o /tmp/media-linux.test ./media`，
  `docker cp` 进去，`docker exec -w /src/media mygo-linuxtest /tmp/media.test -test.v`。
- Windows：本机 `GOOS=windows GOARCH=amd64 go test -c -o /tmp/media.test.exe ./media`；`scp` 落在
  `C:\Users\zhanw`，再 `move` 到 `d:\Program\mygo\media-test\`（该机 proxy 不通，只能送预编译二进制）；
  `media/testdata` 用 `tar czf` 打包后一起送过去解压（Mac 带的 `._*` 无妨）。远端 shell 是 cmd：一条
  `ssh` 命令里用 `&` 串联、`|` 会被当成管道，输出先 `chcp 65001` 才不是 GBK 乱码。
- 每次改动的 gate：`gofmt -l`；三平台 build+vet；`go vet -tags mygo_noinspector`；wasm build+vet
  （用 `go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}'` 排除 `/cmd/mygo`）；`go test ./...`；
  `bun run test` + `bun run typecheck`；两条 release 体积命令。
