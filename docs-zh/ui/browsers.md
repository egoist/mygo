# 浏览器中的原生 UI

原生 UI 应用也能在浏览器中运行：它编译为 WebAssembly，窗口显示在页面的画布上，由与 Linux 上相同的渲染器、相同的着色器以 WebGL 2 绘制。应用本身无需任何改动：

```go
func main() {
	s := &counter{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Counter", Content: ui.View(s.view)})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

`mygo dev -web` 在浏览器中开发它：把应用编译为 WebAssembly，在 `http://localhost:5174` 上提供并打开它，并在你编辑时重新构建，每次重新构建都会重新加载页面。`mygo build -platform web` 把它构建成 `<out>/web` 下的静态站点，可由任何服务器托管：`index.html`、Go 的 `wasm_exec.js` 以及应用的 WebAssembly（见 [CLI](../cli.md#mygo-build)）。

```sh
mygo dev -web
mygo build -platform web
```

不用 CLI 时，可以面向 `js/wasm` 构建，并把 Go 的 WebAssembly 加载器放在它旁边：

```sh
GOOS=js GOARCH=wasm go build -o app.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" .
```

再从页面中加载它：

```html
<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>html, body { margin: 0; height: 100%; }</style>
    <script src="wasm_exec.js"></script>
    <script>
      const go = new Go();
      WebAssembly.instantiateStreaming(fetch("app.wasm"), go.importObject).then((r) => go.run(r.instance));
    </script>
  </head>
  <body></body>
</html>
```

通过 HTTP 托管该目录，并让 WebAssembly 以 `application/wasm` 返回，多数服务器都会这样做。窗口填满 ID 为 `mygo` 的元素，没有该元素时则填满整个页面；后打开的窗口覆盖先打开的。窗口的大小由页面决定：尺寸、位置和标题栏选项都不起作用，最后打开的窗口的标题就是页面的标题。

## 可用的功能

- **绘制。** 帧按设备像素比用 WebGL 2 绘制，浏览器每有一帧发生变化就绘制一次；效果（如 [Liquid Glass](../plugins/glass.md)）的绘制与桌面上相同。浏览器没有 WebGL 2 时，帧先在内存中绘制，再通过 2D 画布显示。WebGL 上下文丢失后，浏览器恢复它时会再次绘制。
- **文本** 来自浏览器：浏览器负责查找字体、为字体缺少的字形回退并完成字形排布，就像处理它自己的页面一样：`system-ui` 是系统字体，与页面中的一样。换行与文字段的顺序（bidi）处处与 `text` 包一致。文本绘制不使用次像素抗锯齿。
- **输入**：指针，包括单击次数；滚轮和触控板；按键；输入法，它在光标处一个不可见的文本区域中组合；以及剪贴板中的文本，通过浏览器的复制、剪切和粘贴。在 Mac 上，视图快捷键中的 Control 是 ⌘，因为浏览器被当作 macOS 以外的系统。Mac 上的浏览器不会报告在按住 ⌘ 期间松开的按键：它随 ⌘ 一起松开（[`c.KeyDown`](input.md#按住的键)）。
- **路由器。** 窗口显示的第一个[路由器](navigation.md)跟随标签页的历史，记录在 URL 的片段中，如 `#/notes/42`：浏览器的后退和前进按钮会在它的页面之间切换，指向某个页面的链接或重新加载会在这里打开应用。
- **拖到窗口上的文件**：`DroppedFiles`、`FileDragOver` 和 `OnFileDrop` 与桌面上一样工作。浏览器不提供路径，因此后端在文件放下时读取它们，并存放在自己的路径下，如 `/mygo-drop/3/photo.png`，`os.Open`、`os.ReadFile`、`os.Stat` 和 `os.ReadDir` 会读取这些路径；最近 16 次拖放的文件会保留下来。
- **上下文菜单**显示为页面上的 HTML 菜单。
- **深色模式**、减少动效和更高对比度跟随浏览器设置，并随之改变。页面也可以在自己的根元素上用 `data-mygo-theme="dark"` 或 `"light"` 自行选择外观；嵌入应用的页面若想给它单独的主题，也会这样做（框架的 `prefers-color-scheme` 跟随浏览器，而不是它周围的页面）。
- `Dialog.Message` 用浏览器的 `alert` 和 `confirm` 询问，`Shell.OpenExternal` 会打开一个新标签页。

## 不可用的功能

网页窗口、应用菜单和 Dock 菜单、托盘、通知、全局快捷键、文件对话框，以及文件之外数据的拖放，在浏览器中都没有可对应的实现，并会如实报告。辅助技术还看不到内容。样式的 OpenType 特性中，画布只应用 `kern`。用 `ui.RegisterFont` 添加的字体需要给出字体族，等浏览器加载完成后才能绘制。剪贴板文本在粘贴时来自系统；视图自己的“粘贴”命令粘贴的则是应用上次复制的内容。

## 体积

counter 示例是 8.2 MB 的 WebAssembly，用 gzip 压缩后为 2.2 MB；这些文档的预览包含所有组件，为 9.8 MB（压缩后 2.6 MB）。为浏览器构建会去掉包 `mygo` 中供网页使用、浏览器用不到的部分：它的 IPC、自定义 scheme 及其 HTTP、TypeScript 生成器、更新器，以及它们引入的 JSON 和加密相关代码。它们的 API 仍在，但什么都不做：`Bind` 不绑定任何东西，事件不会到达任何页面，`Updater.Check` 报告 `ErrUpdatesDisabled`。只有接收 net/http 类型的 `Protocol.Handle`、`HandleFunc` 和 `FileServer` 缺失，因此使用它们的代码只用于桌面（`//go:build !js`）。`mygo build` 为生产构建设置的 `mygo_noinspector` 标签也会去掉原生 UI 的检查器，约 350 KB。为浏览器构建不会给桌面应用增加任何体积：浏览器的代码只出现在 `js/wasm` 构建中。

## 内部实现

Go 的主 goroutine 运行事件循环；浏览器的事件通过 JavaScript 调用的函数到达它，这些函数会等待它处理事件，从而仍能阻止浏览器的默认行为。架构指南的[浏览器](../architecture.md#browsers-internalweb)一节有细节。
