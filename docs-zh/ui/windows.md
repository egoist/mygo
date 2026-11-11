# 使用原生 UI 的窗口

带 `Content` 的窗口接受任何窗口的[窗口选项](../windows.md#options)，例如大小、`StateKey`、`Frameless` 和 `Parent`，也支持菜单、对话框和其他[桌面 API](../native.md)。它没有页面：`Page()` 为 nil，`URL` 和 `WindowOptions.Page` 会被忽略。它的 Go 代码不需要绑定，视图会直接调用。`CapturePage` 返回它所显示内容的 PNG。

```go
mygo.NewWindow(mygo.WindowOptions{
	Title:    "Notes",
	Width:    900,
	Height:   600,
	StateKey: "main",
	Content:  ui.View(app.view),
})
```

视图会在 `NewWindow` 返回后首次运行，因此可以使用从它那里留下的窗口引用，如 `app.win.Title()`：在主线程上调用时，`NewWindow` 会在主循环的下一轮显示窗口（见[无闪烁地显示窗口](../windows.md#show-windows-without-flashing)）。

## 标题栏

使用 `TitleBarStyle: mygo.TitleBarHidden` 时，视图会在窗口控件下方绘制标题栏，就像页面通过 `--mygo-titlebar-*` CSS 变量所做的那样：`c.TitleBar()` 返回控件占用的空间，全屏时为零，`DragWindow` 让元素可以拖动窗口，双击它们则会像标题栏一样缩放或最小化：

```go
bar := c.TitleBar()
ui.Row(c).Height(max(bar.Height, 32)).Padding(0, bar.Right+12, 0, bar.Left+12).DragWindow().Children(func() {
	ui.Text(c, "Inbox").Bold()
})
```

`bar.Bottom` 是控件结束的位置，从窗口顶边算起：在 macOS 上是红绿灯按钮的底部，`TrafficLightPosition` 可以移动它们；Linux 和 Windows 上则是 `bar.Height`。如果侧边栏一直延伸到窗口顶部、红绿灯按钮浮在它上面，侧边栏的条目就要从按钮下方开始：

```go
ui.Column(c).Width(220).Padding(bar.Bottom+8, 8, 8, 8).DragWindow().Children(func() {
	ui.Text(c, "Inbox")
})
```

在 `Frameless` 窗口中，`DragWindow` 让元素可以移动窗口，双击它则会使窗口最大化。

如果某个拖动区域在双击时有别的用途，例如侧边栏空白处用来新建内容，可以用 `WindowDoubleClicked` 接管双击，同时仍然可以移动窗口：

```go
side := ui.Scroll(c.Key("sidebar")).Grow(1).DragWindow()
if side.WindowDoubleClicked() {
	app.newItem()
}
```

如果还想自己绘制窗口控件，并放在视图希望的位置，可以在隐藏标题栏的基础上加上 `HideWindowButtons: true`：系统控件会消失，`c.TitleBar()` 变为零，但窗口仍保留标题栏的边缘，可以用来调整大小，顶部边缘也包括在内。视图中的按钮可以调用 `Minimize`、`Maximize`、`Unmaximize` 和 `Close`。

## 缩放

`c.SetZoom(z)` 会缩放窗口内容，就像浏览器缩放页面：视图会在大小为原来 `1/z` 的窗口中布置，再放大 `z` 倍显示。它不是把窗口截图放大：帧会按屏幕分辨率乘以 `z` 来绘制，因此文本、图标、矢量、图像及其边缘都和在原生环境中以该尺寸显示一样锐利，窗口帧的像素尺寸保持不变。在缩放系数为 2 的屏幕上，2 倍缩放绘制出的帧，效果等同于在缩放系数为 4 的屏幕上绘制，字形也一样；不缩放时则没有任何变化。缩放值会一直保持，直到再次设置，范围为 0.25 到 5；`c.Zoom()` 返回当前值，未缩放时为 1。

```go
c.SetZoom(app.zoom) // 1.25 for 125%, every frame or once
width, height := c.Size() // the content's: the window's divided by the zoom
```

视图看到的尺寸都是缩放后的：`c.Size()`、`c.TitleBar()`、`Bounds` 以及所有长度都以内容 DIP 计，`z` 个内容 DIP 等于一个窗口 DIP。如果某一帧设置了另一个缩放值，它会先按新尺寸重新构建，然后再显示。从内容传给窗口的坐标则使用窗口的 DIP：指针命中的是缩放后显示在其下方的内容，输入法在光标处组合，辅助技术在元素实际显示的位置勾勒它，上下文菜单和拖动从指针下方开始，Web 视图拿到内容分配给它们的盒子，`MaximizeButton` 会在它显示的位置弹出 Windows 的贴靠布局。触摸板会按手指移动的距离滚动内容；鼠标滚轮则按未缩放的距离滚动。窗口控件保持原有大小：`c.TitleBar()` 是它们占用的空间，这个空间也经过缩放，因此为避开它们而布置的标题栏仍然不会重叠。

窗口自身的尺寸不参与缩放：`WindowOptions.Width` 和 `Height`、`Window.SetContentSize` 和 `Window.ContentSize` 以窗口的 DIP 计，是视图所见尺寸的 `z` 倍，和页面在其缩放下的情况一样（`Page.SetZoomFactor`）。一个 320 × 400 的浮出窗口缩放 1.5 倍后，其视图会布置在约 213 × 267 的范围内。如果要用视图自己的 DIP 给窗口一个尺寸，就乘以缩放值：

```go
z := c.Zoom()
flyout.SetContentSize(int(math.Ceil(float64(320*z))), int(math.Ceil(float64(400*z))))
```

相比之下，变换的 `Scale` 会按内容自身的尺寸缩放绘制内容：其中的文本会以未变换尺寸的位图绘制并做双线性采样（[变换](transforms.md#渲染)），适合动效，用来缩放窗口则会模糊。

## 材质

窗口的 `Vibrancy` 会显示在原生 UI 不绘制背景的地方：macOS 上始终如此；Windows 11 22H2 及更高版本上，则仅在创建时启用了材质的窗口中如此。`c.Vibrancy()` 说明它是否生效：在其他地方，视图留作透明处不会显示材质，所以应改为在那里绘制背景。根元素默认绘制主题背景：可以把它设为透明，再给需要背景的部分加上背景，例如不透明内容旁的侧边栏：

```go
if c.Vibrancy() {
	c.Root().Background(ui.Transparent)
}
ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
	sidebar := ui.Column(c).Width(240)
	if !c.Vibrancy() {
		sidebar.Background(c.Theme().Surface)
	}
	sidebar.Children(func() { app.sidebar(c) }) // over the material
	ui.Column(c).Grow(1).Background(c.Theme().Background).Children(func() { app.content(c) })
})
```

在透明根元素下，不透明背景上的文本仍保留系统的次像素抗锯齿（ClearType）；而在材质上，则使用灰度抗锯齿。`examples/vibrancy` 会在这样的侧边栏中选择窗口材质。

### 覆盖在其他应用上的 Liquid Glass

`VibrancyGlass` 和 `VibrancyGlassClear` 用 macOS 26 的 Liquid Glass 填充窗口，系统会结合窗口背后的内容来绘制它：其他应用的窗口、桌面。因此，带 `CornerRadius` 的无边框窗口就成了浮在它们之上的一块玻璃，例如启动器的面板、平视显示器或浮动工具栏：

```go
hud := mygo.NewWindow(mygo.WindowOptions{
	Frameless:    true,
	Transparent:  true,
	AlwaysOnTop:  true,
	Vibrancy:     mygo.VibrancyGlassClear, // Acrylic on Windows 11
	CornerRadius: 18,
	Width:        360,
	Height:       64,
	Content:      ui.View(view), // its root transparent where c.Vibrancy()
})
hud.SetActiveAppearance(true) // stays clear over the app in front (macOS)
```

`VibrancyTint` 按其 alpha 给那块玻璃着色，和 AppKit 的 `NSGlassEffectView` 一样：

```go
hud.SetVibrancyTint("light-dark(rgba(0, 122, 255, 0.2), rgba(255, 159, 10, 0.2))")
```

其他材质和平台会忽略它。`WindowOptions` 中的 `VibrancyTint` 可以在创建时设置它。

[glass 插件](../plugins/glass.md)也会在每个平台上绘制 Liquid Glass，但它用的是窗口自身绘制的内容：例如位于下方滚动内容之上的工具栏。窗口的玻璃显示应用自身的任何窗口都未绘制的内容，并且只在系统支持它的地方：在 macOS 26 之前是浮层或 HUD 材质，Windows 11 上是 Acrylic，Linux 上则没有；`c.Vibrancy()` 会告知结果。

清晰的玻璃只有在窗口被绘制为活动状态时才显得清晰，也就是位于前面的应用的主窗口；否则系统会把它磨砂，就像对待浮在用户当前应用之上的面板那样。`SetActiveAppearance` 会用一次私有的 AppKit 调用让窗口始终保持活动外观，未来的 macOS 可能移除这一调用，到那时窗口就会像其他窗口一样绘制：面向 Mac App Store 的应用应让它保持关闭。

## 适应其内容的浮出窗口

带 `FitContent` 的浮出窗口会采用其原生 UI 测量出的尺寸，并随后续变化调整，同时限制在 `MinWidth`、`MinHeight`、`MaxWidth`、`MaxHeight` 和工作区内：根元素内容按最大内容宽度，高度取该宽度下的高度，就像托盘面板的列表或错误行时有时无时那样。如果某一帧测量出另一个尺寸，就会调整浮出窗口的大小，下一帧再按该尺寸布置内容。隐藏的浮出窗口在显示时，会布置隐藏期间发生变化的内容，并按内容实际需要的尺寸显示。见[让浮出窗口适应其内容](../windows.md#size-a-flyout-to-its-content)。

## 活动与非活动窗口

原生应用会把接收用户输入的窗口画得和其他窗口不同，让用户一眼看出输入落在哪个窗口：macOS 会把非活动窗口侧边栏的材质淡成不透明灰色，把其选区和控件的强调色淡成灰色，Windows 把它的 Mica 画成纯色填充，GTK 则将其样式设为 `:backdrop`。当窗口自身或它的某个浮出窗口拥有键盘焦点时，窗口是活动的（菜单或浮层属于其窗口）；如果 `SetActiveAppearance` 让它保持这种状态，则始终活动。没有父窗口的面板和浮出窗口会浮在任何前台应用之上，始终是活动的。

内置控件会自动跟随：选中的行、侧边栏的图标、选中的复选框、单选按钮和开关、滑块、进度条以及所选标签页的线条，会从强调色淡成主题的 `InactiveAccent`（一种灰色）；主要按钮则看起来和其他按钮一样（[主题](styling.md#主题)用 `InactiveAccent: Accent` 保留强调色）。视图可以用 `c.WindowActive()` 自行处理：

```go
dot := c.Theme().Accent
if !c.WindowActive() {
	dot = c.Theme().TextMuted
}
```

读取它的帧会在它变化时重新绘制；不读取它的帧则不会因此多绘制任何内容。`Tester.SetWindowActive` 可以在测试中切换活动状态，键盘焦点仍由 `SetFocused` 控制。窗口材质会跟随系统（macOS 会在非面板、非浮出窗口的窗口中淡化它，Windows 会淡化其 Mica）；`examples/vibrancy` 一起展示了这两者。

## 隐藏的窗口

`c.WindowHidden()` 报告窗口是否完全没有显示在屏幕上，和 Web 的 `document.hidden` 一样：显示时钟、价格或播放位置的视图会在隐藏时停止工作，并在重新显示时刷新。查询它的帧会在窗口隐藏时重新构建一次（尽管隐藏期间不会绘制任何帧），窗口显示时再构建一次：

```go
app.ticker.SetPaused(c.WindowHidden()) // a ticker of your own
```

为动画或会动的绘制所请求的帧，在窗口隐藏期间仍会等待。macOS 会在窗口最小化、隐藏、位于其他空间或被其他窗口覆盖时告知；浏览器在标签页或窗口不可见时告知；Linux（X11 下）和 Windows 则在最小化时告知。`Tester.SetWindowHidden` 可以在测试中隐藏和显示窗口。

`Window.IsVisible` 回答的是另一个问题：窗口是否被 `Show` 显示过，并且此后没有因 `Hide` 而隐藏，因此最小化的窗口仍然算作可见。视图之外的工作（例如轮询的 goroutine）可以通过视图得知窗口离开了视野；窗口离开视野时视图会重新构建：

```go
paused.Store(c.WindowHidden()) // an atomic.Bool the goroutine reads
```

## 关闭

视图可以在按钮单击或其回调中关闭自己的窗口。窗口会立即消失：可以调用 `Destroy`，也可以调用未被 `OnClose` 监听器阻止的 `Close`；视图其余部分仍会照常构建，`Context` 仍保留主题和窗口尺寸。这一帧到此结束：不再布置或绘制任何内容，也不运行其他回调。关闭的窗口不会保留任何显示过的内容，包括视图及其触及的对象，但应用仍保留 `*mygo.Window`。先打开接替它的窗口，因为应用会在最后一个窗口关闭时[退出](../app.md)：

```go
if ui.Button(c, "Done").Clicked() {
	app.showMain()
	win.Close()
}
```

## 没有 webview

所有窗口都显示原生 UI 的应用不需要 webview：在 Linux 上只需要 GTK 3，不需要 WebKitGTK；在 Windows 上则不需要 WebView2 Runtime。
