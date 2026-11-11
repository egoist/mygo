# 渲染

MyGo 在 macOS 上用 Metal 绘制，在 Windows 上用 Direct3D 11，没有可用的 GPU 驱动时则用 WARP（Windows 自带的软件渲染器），在 Linux 上使用 GTK 的 GtkGLArea 所提供的 OpenGL，在[浏览器](browsers.md)中使用 WebGL 2 和 Linux 的着色器。着色器根据到边缘的距离计算圆角矩形、边框、渐变和阴影，因此无论尺寸和缩放如何都保持锐利；文本则来自一个只上传变化部分的字形图集。

有 GPU 时，每一帧都在 GPU 上绘制。在 Linux 上，原生 UI 的第一个窗口会加载 OpenGL：驱动 Mesa 会占用约 50 MB，并一直保留，约等于一个小应用其余部分的大小。在 OpenGL 无法运行于 GPU 的场合，如虚拟机中或 WSL 中（其中 `GALLIUM_DRIVER=d3d12` 让 Mesa 使用 GPU），Linux 会改在 CPU 上绘制：在高密度显示器上，绘制一个完整的大窗口在多个核心上约需一毫秒，而通常发生变化的内容（例如指针下的按钮）只需不到十分之一毫秒，因为它只重绘这部分，并只把这一部分交给合成器。设置 `MYGO_GPU=0` 可以处处使用 CPU 渲染器，例如用于比较；在 Linux 上设置 `MYGO_GPU=1`，即使 OpenGL 运行在 CPU 上，也用 OpenGL 绘制。

设置 `MYGO_FRAME_STATS=1` 记录每个耗时超过 8 毫秒的帧，或设置 `MYGO_FRAME_STATS=4` 使用另一个以毫秒为单位的阈值（`all` 记录每一帧）。每一行说明构建该帧（以及经过几遍）、布局、绘制和呈现各花了多久，它是在 GPU 上还是在内存中绘制的（窗口没有 GPU 渲染器时），进程在此期间分配了多少、进行了多少次文本排版，以及垃圾回收器是否运行过，还有 goroutine 协助它标记所花的时间：

```
mygo: frame 412 took 17.3 ms: build 3.1 (2 passes), layout 1.2, paint 0.8, present 11.5 (drawn on the GPU), other 0.7; meanwhile the process made about 2312 allocations, 171.0 KB, 12 text layouts; GC: 1 cycles ended, 4.1 ms of assists
```

分配量和回收器统计的是整个进程，包括其他 goroutine，如同 `runtime/metrics` 的统计方式：一次一个 span，因此较小的计数会偏低。该变量未设置时，帧不做任何测量。连续的帧会等待显示器的下一次刷新，呈现也可能等待它；要查看帧最快能达到多快，同时设置 `MYGO_UNCAPPED=1`（[帧节奏](#帧节奏)）。

MyGo 只在有东西变化时绘制一帧：输入、`Invalidate`、`After`，或会移动的动画。空闲窗口不绘制任何东西，在最后一帧之后两秒，它会释放内存中绘制的帧。

在 macOS 上做动画的窗口会为显示器保留三帧，比它的帧轮流使用的两帧多一帧：只有两帧时，120 Hz 显示器会频繁地同时占住两者，导致帧要等待其中一帧，有些帧便未被绘制。第三帧在窗口进入空闲之前，每像素约占窗口的 4 字节（颜色超出 sRGB 时为 8 字节）。

## 帧节奏

彼此相继的帧，如动画的帧，随显示器的刷新绘制，因此每个耗时短于一次刷新的场景都能达到显示器的速率：在 120 Hz 显示器上每秒 120 帧，无论场景轻还是重。设置 `MYGO_UNCAPPED=1` 可以在不等待显示器的情况下呈现帧，用于测量一个场景还剩多少刷新时间、在任意负载下比较两个构建，或获得最低延迟，例如墨迹、远程桌面或游戏所需要的。于是下一帧在上一帧呈现后立即开始，只要有东西在动或请求帧；什么都不变的窗口仍然不绘制。同时设置 `MYGO_FRAME_STATS` 时，MyGo 记录每秒来了多少帧，记录的每一帧都标明为 uncapped：

```
mygo: 2481 frames a second, uncapped: 0.40 ms a frame
```

默认仍按节奏呈现：uncapped 的帧会尽量耗尽 CPU 和 GPU，并可能出现撕裂，而应用并不希望如此。各平台的做法：

| 平台 | Uncapped |
|---|---|
| Windows | 在系统允许时，交换链允许撕裂（`DXGI_FEATURE_PRESENT_ALLOW_TEARING`），每一帧都以同步间隔 0 呈现，既不等交换链的帧延迟对象，也不等 DWM 的下一次合成。只有当窗口占据整个显示器时，帧才会出现撕裂，如覆盖屏幕的无边框窗口；在其他地方，DWM 仍会在每次刷新时显示最新的帧。不允许撕裂时，帧仍会立即呈现，新帧会取代 DWM 尚未显示的那一帧。位于材质（DirectComposition）之上的窗口没有撕裂。窗口改变大小时，从开始到上次改变后半秒的那一帧为止，帧都按节奏呈现，如同没有 `MYGO_UNCAPPED`，并记录 `paced while resizing`：uncapped 的帧在 DWM 调整窗口大小时排队，使一次呈现停顿数秒。新尺寸的帧最多等待 DWM 两次刷新来显示，此后每一帧都用 `DXGI_PRESENT_DO_NOT_WAIT` 呈现：Present 无法在一次刷新内接受的帧，在下次合成后呈现，或让位给下一帧，同时窗口继续处理自己的消息，这样任何帧都不会长时间等待 DWM。100 毫秒后仍未被接受的帧会不带该标志呈现，在另一个线程上等待 DXGI，同时窗口继续处理消息，它完成后下一帧再绘制。此后，帧保持按节奏，使用 `DXGI_PRESENT_DO_NOT_WAIT`，直到调整大小稳定后 DXGI 立即接受一帧；缓冲区的调整大小（无论为了稳定还是增长）也在另一个线程上进行：除非有一次调用等待它，否则 DXGI 不会再以该标志接受帧，而在主线程上进行这样一次调用会让一帧卡住约一秒。这样一次调用等待期间到来的帧会被推迟，窗口显示最后一帧。Present 拒绝的帧记录 `Present still drawing N times`，等待时记录 `presenting once DXGI can`；被推迟的帧记录 `put off while Present waits on another thread`（或 `ResizeBuffers`），它之后的帧记录 `after Present waited on another thread`，调整大小稳定后按节奏的帧记录 `paced until DXGI takes a frame at once`。 |
| macOS | 显示链接不再为帧定节奏：每一帧都在运行循环的下一轮、在处理完到来的输入之后开始，进入一个 `displaySyncEnabled` 关闭的 `CAMetalLayer`。窗口服务器仍然每次刷新合成一次窗口，显示最新的帧，且不撕裂：高于显示器速率的帧会被绘制和测量，但不会全部显示。 |
| Linux | GTK 的帧时钟在上一帧绘制完成后立即开始下一帧，在 X11 上窗口的 GLX 交换间隔为 0（GTK 在 Wayland 上的 EGL 已经以 0 交换）。为窗口定节奏的合成器仍然如此：在 X11 上是发送 `_NET_WM_FRAME_TIMINGS` 的合成器，在 Wayland 上是内存绘制帧的帧回调。 |
| 浏览器 | 忽略：`requestAnimationFrame` 为帧定节奏，也不会更早呈现。 |

游戏在 uncapped 时仍保持自己的步进速率：无论来了多少帧，[`game.View`](../plugins/game.md) 都按经过的时间每秒运行 `Update` 60 次，并以每一帧进入下一步的进度运行 `Draw`。
