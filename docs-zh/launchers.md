# 启动器

Spotlight、Raycast、Alfred、PowerToys Run 和 Ulauncher 这类启动器，都能通过快捷键随时唤出，在当前应用上方接收输入，用完即可快速收起。MyGo 提供了构建启动器所需的功能：用于显示界面的[面板](windows.md#面板)、唤出面板的[全局快捷键](native.md#全局快捷键)、可供搜索的[已安装应用](native.md#shell)列表及图标，以及不显示 Dock 图标、驻留在[托盘](menus.md#托盘图标)中的应用模式。[`examples/launcher`](https://github.com/egoist/mygo/tree/main/examples/launcher) 演示了如何用原生 UI 将它们组合起来：

```sh
go run ./examples/launcher
```

## 后台应用

启动器没有 Dock 图标，也不出现在应用切换器中，运行时不显示窗口，而且只运行一个实例：

```go
func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return // the launcher that runs shows itself
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnSecondInstance(func([]string, string) { show() })
	mygo.App.WhenReady(ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

打包后的应用可以从一开始就声明这一点，让 macOS 从不把它显示在 Dock 中：在 [`mygo.json`](configuration.md#mygojson) 中设置 `LSUIElement`：

```json
{
  "macos": { "infoPlist": { "LSUIElement": true } }
}
```

托盘图标给用户提供快捷键之外的进入方式，以及退出的方式；[`App.SetOpenAtLogin`](app.md#登录时启动) 让启动器随会话启动，`App.WasOpenedAtLogin` 则让它在登录时保持隐藏。

## 面板

面板浮在每个应用之上，包括全屏应用，无论用户在哪个 Space 或虚拟桌面；在 macOS 上它取得键盘而不激活启动器：用户所在的应用保留其菜单栏，并在面板隐藏后重新获得键盘。以隐藏状态创建它，只创建一次；隐藏而不是关闭它，以便它再次显示时立即恢复原样：

```go
panel := mygo.NewWindow(mygo.WindowOptions{
	Panel:         true,
	Hidden:        true,
	Frameless:     true,
	Transparent:   true,
	Vibrancy:      mygo.VibrancyGlass, // Liquid Glass on macOS 26, Acrylic on Windows 11
	CornerRadius:  16,
	Width:         680,
	Height:        420,
	DisableResize: true,
	Content:       ui.View(app.view), // or URL: "/launcher"
})
panel.OnClose(func(e *mygo.CloseEvent) { // Escape, Cmd-W, Alt-F4
	e.PreventDefault()
	panel.Hide()
})
panel.OnBlur(panel.Hide) // the user went elsewhere
```

页面或原生 UI 没有处理的 Escape 会关闭面板，`OnClose` 监听器把它变成隐藏。如果 Escape 先要做别的事，就自己处理它，如同示例中先清空查询那样。

在用户正在看的显示器上显示它（指针会告诉你），像 Spotlight 那样放在从上往下三分之一处，并让它的搜索框取得键盘：

```go
func show() {
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	w, h := panel.Size()
	panel.SetPosition(d.WorkArea.X+(d.WorkArea.Width-w)/2, d.WorkArea.Y+(d.WorkArea.Height-h)/3)
	app.query = ""
	app.input.Focus()
	panel.Show()
}
```

## 快捷键

```go
err := mygo.GlobalShortcut.Register("Alt+Shift+Space", func() {
	if panel.IsVisible() {
		panel.Hide()
	} else {
		show()
	}
})
```

选择一个其他启动器留空的快捷键：Raycast 和 PowerToys Run 占用 Alt+Space，Spotlight 占用 Cmd+Space，而 macOS 会把快捷键给每个注册它的应用，因此两个应用都会打开。让用户在启动器的设置中选择另一个：`Unregister` 旧的，`Register` 新的，并在 `Register` 因已被占用而失败时告诉他们。在 Windows 上，按键让面板取得前台；在 Wayland 上，由桌面绑定快捷键，见[全局快捷键](native.md#全局快捷键)。

## 查找应用

`Shell.InstalledApps` 列出计算机上安装的应用，按名称排序，名称采用系统以用户语言显示的形式。它会读取磁盘，耗时几十毫秒：面板显示时，在 goroutine 上列出它们，并用 `Window.Update`（或通过绑定服务交给页面）把它们交给视图：

```go
go func() {
	apps, err := mygo.Shell.InstalledApps()
	if err != nil {
		log.Print(err)
	}
	panel.Update(func() {
		app.apps = apps
		app.filter()
	})
}()
```

`Shell.FileIcon(app.Path, size)` 把应用的图标绘制为 PNG，单位为像素：DIP 大小乘以显示器的缩放因子。它要花几毫秒：只绘制显示出来的行的图标，并按路径保留它们。`Shell.LaunchApp(app)` 启动所选应用，或把它带到前面；先隐藏面板，以便用户所在的应用在启动的应用取得键盘之前保有键盘。

## 平台

| | macOS | Windows | Linux |
|---|---|---|---|
| 没有 Dock 图标，没有任务栏按钮 | `LSUIElement`、`ActivationPolicyAccessory` | 面板没有 | 面板没有 |
| 面板 | 一个不激活应用的 `NSPanel`，无论常规应用还是辅助应用的全屏应用，都浮在其上 | 一个置顶的工具窗口 | X11 上保持在上的工具窗口；Wayland 合成器支持时是一个绘制表面（需要 `libgtk-layer-shell.so.0`） |
| 快捷键 | Carbon 热键 | `RegisterHotKey` | X11 抓取，Wayland 下是 XDG desktop portal |
| 应用 | 各 Applications 文件夹 | 开始菜单的“所有应用” | 桌面条目 |

在 Wayland 上，作为绘制表面的面板在显示期间一直保有键盘，如同那里的启动器，因此只在隐藏时失焦：用 Escape 关闭它。
