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
| Q14 | 录制默认组合：v1 三平台统一 MP4+H.264（音频采集与 WebM mux 都还没做，macOS/Windows 的 AAC、Linux 的 WebM+VP9+Opus 留到音频采集落地后再切默认）；编码器仍是系统栈：VideoToolbox / Media Foundation / GStreamer `x264enc` |
| Q15 | Linux 屏幕捕获：X11/XWayland 用 `ximagesrc`（已做）；Wayland 的 XDG ScreenCast portal + `pipewiresrc` 这版未做，遇到 Wayland 会话给结构化 `ErrUnsupported`，绝不黑帧 |
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
| M5 | 屏幕捕获（ScreenCaptureKit / GDI，DXGI 桌面复制留作升级 / ximagesrc） | 完成（macOS 真机 4/4、SCK 3420x2224 变化驱动；Linux 容器 Xvfb + ximagesrc 6/6；Windows 交互式桌面 GDI 6/6，ssh 非交互会话枚举与钩子通过、真采集给结构化 `MissingComponentError`） |
| M6 | 编码与 Recorder（VTCompressionSession / MFT / GStreamer appsink + 纯 Go mux） | 完成（三平台各 6/6 录制测试：macOS VideoToolbox 真机、Linux 容器 `x264enc`、Windows 真机 MFT；写出的分片 MP4 由各自系统播放器读回 30/30 帧） |
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

### 屏幕捕获（M5）
公共面：`media.Screens()` / `media.OpenScreen(id)` / `Screen.NextFrame(ctx)`，屏幕和相机、文件一样是
`media.Source`；`ScreenInfo{ID,Name,Width,Height}`、`ErrNoScreen`、`ErrPermissionDenied`，权限
`ScreenPermission()/RequestScreenPermission(ctx)/OpenScreenPermissionSettings()`。画面统一成
`PixelRGBA` 一平面，队列沿用 `liveFrames`（4 帧、丢最旧）。

- macOS：ScreenCaptureKit（`SCShareableContent`、`SCContentFilter`、`SCStreamConfiguration`、`SCStream`）。
  列表是异步的：`getShareableContentWithCompletionHandler:` 用一个 `objc.NewBlock` 接答案，Go 侧 channel
  等；`SCDisplay` 的 `width/height` 是**点数**（Retina 上是像素的一半），所以采集尺寸取
  `CGDisplayCopyDisplayMode` + `CGDisplayModeGetPixelWidth/Height`（本机 3420x2224）。像素格式直接请求
  `'BGRA'`，由系统转换（和相机相反：这里配置的是自己的 stream，不像 `AVCaptureVideoDataOutput` 会 abort）。
  `SCStream` 的 delegate 是 weak，要自己 retain 一个 `MyGoScreenOutput` 对象（`objc.RegisterClass` 带上
  `SCStreamOutput` 协议，`stream:didOutputSampleBuffer:ofType:` 按对象路由到各自的 capture）；
  `addStreamOutput:type:sampleHandlerQueue:error:` 用共享的 capture queue，`startCapture`/`stopCapture`
  各用一个 completion block 等完成。**ScreenCaptureKit 是变化驱动的**：画面不动就没有新帧（实测帧间隔
  33ms → 350ms 跳变），所以 `NextFrame` 只保证"有新画面时给最新的"，不承诺固定帧率。权限是 TCC 的屏幕录制：
  `CGPreflightScreenCaptureAccess`/`CGRequestScreenCaptureAccess`；macOS 分不出"没问过"和"拒绝了"，
  `ScreenPermission()` 未授权时一律报 `PermissionNotDetermined`，改答案后要重启 app；设置页
  `x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture`。
- Windows：GDI `EnumDisplayMonitors` + `GetMonitorInfoW` 枚举，`CreateDIBSection`（32bpp、自顶向下）
  + `BitBlt(SRCCOPY)` 抓图，30fps 轮询；像素是 BGRA，Go 里翻成 RGBA。**ssh 的非交互会话没有桌面**：
  `GetDC(0)` 能拿到句柄但 `BitBlt` 返回 `ERROR_INVALID_HANDLE`，这里翻译成
  `MissingComponentError{Component:"an interactive desktop"}`；真采集要用交互式会话验证（本机用
  `schtasks /create /it` 在已登录桌面里跑，6 个用例全过）。变化驱动的 DXGI Desktop Duplication 需要
  Direct3D 设备与 GPU 纹理拷贝，留作后续升级；测试钩子 `screenTestCapture` 让无桌面的会话也能覆盖出帧/失败路径。
- Linux：`ximagesrc use-damage=false ! videoconvert ! video/x-raw,format=RGBA ! appsink sync=false
  max-buffers=2 drop=true`；`DISPLAY` 必须存在（X11/XWayland），缺 X11 插件或被 Wayland 的 XDG ScreenCast
  portal 挡住时给结构化 `MissingComponentError`（Wayland 原生的 portal + `pipewiresrc` 这版没做，只报
  `ErrUnsupported`，绝不黑帧）。相机的 live 管线抽成共用类型 `gstLive`，屏幕复用同一条 pull 队列；
  容器里用 `Xvfb :99 -screen 0 800x600x24` + `DISPLAY=:99` 实测真实 `ximagesrc` 出帧。测试钩子
  `screenTestSource` 走 `videotestsrc is-live=true` 覆盖管线本身。
- 关掉水管的竞态：`gstLive` 的 run 循环在 `PullSample` 返回空后先看 `stopped()` 再判 EOS，否则 Close 时
  EOS 会把 `ErrEnd` 覆盖成"the source stopped"（Linux 屏幕用例抓到的）。

### 编码与录制（M6）
公共面：`NewRecorder(w, opts...)` / `Recorder.WriteFrame(f)` / `Recorder.Close()`，以及一步到位的
`Record(ctx, path, src, opts...)`；选项 `WithRecordCodec/WithRecordContainer/WithRecordBitrate/
WithRecordFrameRate`。容器 v1 只写 MP4（分片写：一个 init segment + 每个关键帧一个 fragment），
`Muxer`/`NewMuxer`/`CanMux` 是底层，`EncodedPacket`/`CodecConfig` 是编解码器与 mux 之间的约定。
mux 用纯 Go 的 `github.com/Eyevinn/mp4ff`：`trak.SetAVCDescriptor("avc1", sps, pps, true)` 生成 avcC，
样本从 Annex B 的 start code 换成 4 字节长度前缀（`nalusOf`/`lengthSize`），avcC 也可由
`avcCFromSPSPPS` 手拼。Recorder 用第一帧的尺寸懒建编码器、把 PTS 归一到第一帧、最后一个包留到下一个包
或 `Close` 才确定时长（`flush(all)`）；`Record` 中途失败也会 `Close` 出一个能播到最后一帧的文件，并且不关源。

- macOS：VideoToolbox `VTCompressionSession`，输入是 Go 内存里的 `CVPixelBuffer`（`'BGRA'`），输出按
  refcon 路由回各自的 encoder（`routes sync.Map`）；像素 buffer 要留在 Go 里直到回调（`pending`）。
  踩过的坑：`kVTEncodeInfo_FrameDropped` 是 **bit 1（2）**，最初按 bit 0 判断，把所有帧都当丢弃，结果永远
  没有包；`AllowFrameReordering=false`、`RealTime=true`、`MaxKeyFrameInterval=fps`；参数集用
  `CMFormatDescriptionGetH264ParameterSetAtIndex` 取，只挂到第一个包。
- Linux：`appsrc is-live=true format=time ! videoconvert ! video/x-raw,format=I420 ! x264enc
  tune=zerolatency speed-preset=ultrafast bitrate=… key-int-max=… ! video/x-h264,
  stream-format=byte-stream,alignment=au ! appsink`（RGBA→I420 交给 GStreamer）；appsink 侧按 start code
  切 NALU，type 5 是关键帧、7/8 是参数集。缺插件时 `MissingComponentError{Feature:FeatureEncode}`，提示
  `apt install gstreamer1.0-plugins-ugly`（x264enc）。
- Windows：`MFTEnumEx(MFT_CATEGORY_VIDEO_ENCODER, NV12→H264)` 枚举编码器 MFT 逐个试；输入 NV12 在 Go 里
  从 RGBA 转（BT.601 limited range）。**MFT 是异步的**：要先 `MF_TRANSFORM_ASYNC_UNLOCK`；发完
  `MFT_MESSAGE_NOTIFY_BEGIN_STREAMING` 还必须发 `MFT_MESSAGE_NOTIFY_START_OF_STREAM`，否则它永远不发
  `METransformNeedInput`，阻塞式 `GetEvent` 直接卡死（真机上就卡在这）。一个 `METransformHaveOutput` 只对应
  **一次** `ProcessOutput`，多调一次就是 `E_UNEXPECTED 0x8000ffff`；编码中途会给
  `MF_E_TRANSFORM_STREAM_CHANGE 0xc00d6d61`，要枚举 `GetOutputAvailableType` 重设输出类型再继续；
  `Close` 发 `COMMAND_DRAIN` 等 `METransformDrainComplete` 再收尾。属性坑：`MF_MT_FRAME_SIZE` 是
  **width 在高 32 位**（一开始写反，录出来是 120x160）；`MFCreateMemoryBuffer` 之后要 `SetCurrentLength`
  （`Lock` 返回的是当前长度，新 buffer 是 0，得用 `GetMaxLength`/新加的 `LockWrite`）；编码器自己提供输出
  样本，所以 `ProcessOutput` 传 `pSample=nullptr`。

## 测试与验证通道

- macOS：本机 `go test ./media/...`（相机用例要 Terminal 有相机权限；屏幕用例要屏幕录制权限，本机已授予
  DSH Desktop，`screencapture` 能截到别的窗口）；GUI 闭环
  `MYGO_E2E=1 go test ./internal/e2e -run TestVideoInAWindow`。本机 e2e 全量里
  `TestLocaleAtDocumentStart`/`TestPlugins`/`TestSQLitePlugin` 这 3 个 JS-bridge 用例在干净 HEAD 上也失败
  （DSH Desktop 环境问题，与本任务无关，已用 `git stash -u` 在干净树上复现）；media 相关 e2e 通过。
- Linux：colima 容器 `mygo-linuxtest`（Debian 13 + Go 1.27，仓库只读挂 `/src`，已装 GStreamer 1.26.2 +
  `ximagesrc`/`Xvfb`；屏幕用例先 `Xvfb :99 -screen 0 800x600x24 &` 再 `DISPLAY=:99` 跑）；
  本机 `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -o /tmp/media-linux.test ./media`，
  `docker cp` 进去，`docker exec -w /src/media mygo-linuxtest /tmp/media.test -test.v`。
- Windows：本机 `GOOS=windows GOARCH=amd64 go test -c -o /tmp/media.test.exe ./media`；`scp` 落在
  `C:\Users\zhanw`，再 `move` 到 `d:\Program\mygo\media-test\`（该机 proxy 不通，只能送预编译二进制）；
  `media/testdata` 用 `tar czf` 打包后一起送过去解压（Mac 带的 `._*` 无妨）。远端 shell 是 cmd：一条
  `ssh` 命令里用 `&` 串联、`|` 会被当成管道，输出先 `chcp 65001` 才不是 GBK 乱码。
  需要桌面（屏幕采集）的用例走交互式会话：写一个 `runtest.bat` 送到 `d:\Program\mygo\media-test\`，
  `schtasks /create /tn … /tr <bat> /sc once /st 23:59 /it /f` 再 `schtasks /run`，结果重定向到文件，
  读完后 `schtasks /delete`。非交互 ssh 会话里 GDI 真采集会报 `MissingComponentError{an interactive desktop}`。
  录制用例在非交互会话里也能跑（不需要桌面）：`-test.run Recorder` 与 `-test.run Recording` 分开跑，
  因为 cmd 里 `|` 是管道，`-test.run 'Recorder|Recording'` 传不进去。
- 每次改动的 gate：`gofmt -l`；三平台 build+vet；`go vet -tags mygo_noinspector`；wasm build+vet
  （用 `go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}'` 排除 `/cmd/mygo`）；`go test ./...`；
  `bun run test` + `bun run typecheck`；两条 release 体积命令。
