# Web 视图

`ui.WebView` 在原生 UI 的窗口中显示网页：Web 引擎在原生控件旁渲染的文稿、每个标签页一个页面，或应用生成的 HTML 预览。这个页面就是系统的 web 视图（WKWebView、WebView2、WebKitGTK），用 `Window.NewWebView` 创建，工作方式与窗口的页面相同：它加载应用的前端，用类型化的客户端调用绑定的 Go 方法并接收事件。视图在 web 视图之后绘制的内容会显示在页面之上：从上方按钮打开的菜单、下拉选择框的选项、对话框及其变暗的背景、工具提示、toast、角上的徽章。

```go
type browser struct {
	win  *mygo.Window
	docs *mygo.WebView
	menu bool
}

func (app *browser) view(c *ui.Context) {
	ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Padding(8).Gap(8).Children(func() {
			menu := ui.Button(c, "Menu ▾")
			if menu.Clicked() {
				app.menu = !app.menu
			}
			// The popover shows over the page.
			ui.Popover(c, menu, &app.menu, func() { ui.Text(c, "Over the page") })
		})
		ui.WebView(c, app.docs).Grow(1).Radius(8)
	})
}

func main() {
	app := &browser{}
	mygo.App.WhenReady(func() {
		app.win = mygo.NewWindow(mygo.WindowOptions{Title: "Docs", Content: ui.View(app.view)})
		app.docs, _ = app.win.NewWebView(mygo.WebViewOptions{URL: "/docs"})
		app.win.Invalidate()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

## 创建 web 视图

`Window.NewWebView` 为显示 `Content` 的窗口创建 web 视图，窗口关闭时它也随之关闭。它的选项包括页面选项（`Page`，与窗口的 `PageOptions` 相同）、要加载的 `URL`、在页面绘制之前用于填充的 `BackgroundColor`、`Transparent`（通过页面的透明部分显示 web 视图背后的内容：窗口的背景或材质，因为原生 UI 不会在 web 视图下方绘制任何内容），以及 `ScrollToContainer`（把页面上的滚轮交给周围的原生 UI，[见下文](#滚动周围的原生-ui)）。`WebView.Page` 返回其页面，其方法与窗口的页面相同：`LoadURL`、`LoadHTML`、`Eval`、`GoBack`、`OnDidNavigate`、`OnWillNavigate` 等。`Focus` 把键盘交给它，`SetBackgroundColor` 改变它用来填充的颜色，例如应用自身的主题改变时（以 `light-dark(<light>, <dark>)` 给出的颜色会自行跟随外观），`CapturePage` 截取它的页面，`Destroy` 关闭它。

web 视图会像窗口一样信任应用自己的页面，包括使用自定义协议（scheme）的页面：把第三方或用户提供的 HTML 放在[不受信任的页面](../frontend.md#untrusted-pages)中（`PageOptions.Untrusted`），或让它来自不信任的来源，参见[信任](../frontend.md#trust)。

web 视图要像窗口一样只创建一次，不要在视图中创建：视图只负责显示它。视图没有构建的 web 视图会隐藏起来并保留页面，因此标签页视图只构建当前标签页的 web 视图，切回来时页面还在原处。

```go
if app.tab == "docs" {
	ui.WebView(c, app.docs).Grow(1)
} else {
	ui.WebView(c, app.changelog).Grow(1)
}
```

## 它的盒子

web 视图没有自己的尺寸。元素会横跨容器（`AlignSelf(Stretch)`），并用 `Grow` 或一个尺寸占据沿该方向的空间。页面显示在元素的内容盒中：位于边框和内边距之内，边框和内边距用元素的背景绕它绘制，并按 `Radius` 让边框内边缘变圆。页面本身会被裁剪到这些圆角，因此即使 web 视图下方没有绘制任何内容（如在透明窗口中），页面也不会超出圆角，指针只有落在圆角内才会到达页面。它周围的滚动容器和裁剪会像裁剪任何元素一样裁剪它，滚出视野的 web 视图会隐藏。`Opacity` 让页面随元素一起淡出，如 `Router` 的页面淡入那样：页面混合在元素下方绘制的内容之上。当那里没有绘制任何内容时（如在透明窗口中），页面不会淡出：系统的 web 视图没有 MyGo 可以设置的不透明度。

```go
ui.WebView(c, app.preview).Height(320).Radius(12).Border(1, t.Border)
```

### 随周围的原生 UI 缩放

缩放 web 视图的变换（它自己的或围绕它的）会把页面当作它的一张图片来缩放显示：页面保持排版时的尺寸，即变换前元素的内容盒，因此面板放大或缩小时，文本不会重新换行，`innerWidth` 保持不变。指针会在页面显示的位置按其自身坐标到达页面，且只在圆角之内。一个在窗口上方打开的速览面板，其 web 视图和原生控件作为一个整体，在变暗的背景上一起缩放：

```go
func (app *browser) peek(c *ui.Context) {
	scale := c.Root().AnimateTo("peek", app.peekScale(), ui.Animation{Duration: 250 * time.Millisecond})
	ui.Box(c).Absolute().Fill().Background(ui.RGBA(0, 0, 0, .4)) // the backdrop, unscaled
	ui.Column(c).Absolute().Left(80).Top(60).Right(80).Bottom(60).Scale(scale, scale).
		AlignItems(ui.Stretch).Radius(16).Background(t.Surface).Children(func() {
		toolbar(c)
		ui.WebView(c, app.peekPage).Grow(1).Radius(16)
	})
}
```

让 web 视图旋转、倾斜或处于透视中的变换，会把页面正立显示，并在变换后盒子的边界内排版。

- **macOS** 和 **Windows** 由合成器缩放页面：页面作为图片显示，动画以完整大小结束时再次变清晰。
- **Linux**：GTK 3 无法缩放控件。web 视图的高度与页面显示的大小一致，WebKitGTK 会缩放页面以适应，按相同的 CSS 像素宽度重新排版，因此每个尺寸下都清晰：`innerWidth` 保持不变，但页面在缩放改变时会收到 `resize` 事件，而且它只按宽度缩放。它的圆角按整像素裁剪，没有抗锯齿。

## 它上方显示什么

web 视图位于窗口的原生 UI 之下：元素在窗口绘制的内容中切出一个洞，页面就在那里显示。在元素之后绘制的一切都会画在页面之上，如同在任何元素之上一样：它的子元素、绝对定位在其上的元素，以及浮层中的浮层、菜单、下拉选择框、组合框、对话框、工具提示和 toast。阴影和半透明背景会在页面之上混合。

```go
ui.WebView(c, app.page).Grow(1).Children(func() {
	// A badge over the page's corner.
	ui.Text(c, "Draft").Absolute().Top(8).Right(8).Padding(2, 8).Radius(8).Background(t.Accent)
})
```

指针只会在页面显示的地方落到页面上，画在它上面的内容不会把指针夺走：对话框的背景或下拉选择框的背景在打开时会挡住指针，不让它到达页面；浮层只在自己的面板上保留指针；带有 `PassThrough` 的元素则让指针穿过。拖到那里的文件会放到页面上。在页面上按下会关闭它之外的浮层（`PressedOutside`），并把键盘交给页面：原生 UI 的焦点转到 web 视图的元素，用户点击原生 UI 时，键盘会回到它那里。页面会设置它上面的光标。

在原生 UI 的控件之间，Tab 会停在 web 视图上：页面会在它的第一个元素处接住键盘，或用 Shift+Tab 在最后一个元素处接住；Tab 在其元素之间移动，越过最后一个元素后继续到 web 视图之后的控件。`Focus` 会把键盘交给页面，回到它之前拥有键盘的位置。

## 页面留下的按键

一个邮件应用在 web 视图中显示消息的 HTML，旁边是一个由键盘驱动的列表：J 和 K 在消息之间移动，Cmd+K（Windows 和 Linux 上为 Ctrl+K）打开命令面板。用户点击进入消息以选择文本或跟随链接，页面拥有键盘，但 J、K 和 Cmd+K 仍然有效：页面未处理的按键会像 web 视图的元素拥有焦点一样交给原生 UI。周围元素的快捷键（`Element.Shortcut`）和窗口的快捷键（`c.Shortcut`）能看到它们，用于滚动的按键会滚动周围的容器，Escape 会关闭原生 UI 的对话框或浮出层。键盘仍留在页面，页面仍然可以选择、复制、输入并跟随链接。

```go
func (app *mail) view(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		app.palette = true
	}
	reader := ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		app.list(c)
		ui.WebView(c, app.message).Grow(1) // an untrusted page
	})
	if reader.Shortcut(0, ui.KeyJ) {
		app.next()
	}
	if reader.Shortcut(0, ui.KeyK) {
		app.previous()
	}
}
```

页面在以下情况保留一个按键：

- 取消它（在 `keydown` 处理器中调用 `preventDefault`）；
- 在文本框、`textarea` 或可编辑内容中用它输入或编辑，或输入法用它组合文字，或在任意焦点位置用 AltGr 输入字符，如德语键盘上的 @；
- 用它执行操作：链接或按钮上的 Enter 或空格、滑块上的方向键、Ctrl+C 复制选区，或方向键、空格和 Page Down 在它能向该方向滚动时滚动页面；
- 或者它是 Tab（在页面中移动并移出页面），或单独的修饰键。

不受信任页面的按键，以及页面中各帧的按键，也会到来；页面无法伪造按键：原生 UI 只会收到用户按下的键。

- **macOS** 和 **Linux**：WebKit 决定页面处理了哪些按键，并把其他的发送给 web 视图的父级（AppKit 的响应者链，或 WebKit 的 Web 进程应答后的 GTK `key-press-event`），MyGo 在那里接收它们。在 Linux 上，WebKitGTK 保留方向键，而带角色的菜单项的快捷键（如 Edit › Copy 的）会继续传给菜单。
- **Windows**：WebView2 不告知这样的按键。每个文稿（页面及其各帧）的隔离世界中都有一个脚本，它在页面的处理器运行后看到页面的按键，并按上述规则把它留下的按键告知 MyGo。页面自己的脚本既接触不到这个脚本，也接触不到它通向 MyGo 的途径；而且这个脚本不需要桥接，所以不受信任的页面也会提供它们的按键。其他站点的帧由 WebView2 在其自己的进程中运行，会保留其按键。
- **浏览器**（`GOOS=js`）没有 web 视图。

辅助技术在 web 视图的元素内部找到页面，它就在原生 UI 之中：VoiceOver 和 Orca 像浏览其周围的控件一样浏览并穿过它。

## 滚动周围的原生 UI

一个阅读器在原生滚动容器中显示文稿：原生页头和它的评论之间的文章、邮件线程中的消息、原生卡片之间的渲染笔记。web 视图与页面一样高，页面自身没有可滚动的内容，页面上的滚轮应当滚动容器，就像在周围的原生元素上那样。`WebViewOptions.ScrollToContainer` 可以做到这一点：web 视图上的滚轮和触摸板会像在任何原生元素上一样滚动指针下的原生 UI，鼠标滚轮按行，触摸板按像素，方向相同，并带上按住的修饰键。页面永远不会随之滚动；它仍然接收指针按下、键盘、选择和触摸板的捏合。

```go
article, _ := win.NewWebView(mygo.WebViewOptions{URL: "/article", ScrollToContainer: true})

// In the view:
ui.Scroll(c).Fill().Children(func() {
	header(c)
	ui.WebView(c, article).Height(app.articleHeight) // the page's height, which it tells the app
	comments(c)
})
```

没有它时（默认情况），页面会在它显示的地方接收滚轮，这正是会自行滚动的页面所需要的：文档、浏览器的标签页、编辑器。

- **macOS**：web 视图的滚动事件（包括触摸板的惯性）会发送给原生 UI，而不是 WebKit。
- **Windows**：窗口保留 web 视图上的滚轮消息，而不转发给 WebView2。触摸板的捏合被 Windows 作为按住 Ctrl 的滚轮发送，仍然会缩放页面。
- **Linux**：web 视图的滚动事件会在 WebKitGTK 看到它们之前发送给原生 UI。
- **浏览器**（`GOOS=js`）没有 web 视图，所以该选项在那里不起作用。

## 它的页面

web 视图的页面工作方式与窗口的页面相同（参见[绑定](../bindings.md)）。`mygo.CallerPage(ctx)` 返回调用绑定方法的页面，`Page.WebView` 返回它的 web 视图，而 `mygo.CallerWindow(ctx)` 返回 web 视图所在的窗口，页面的窗口控件（`mygo.window.minimize()`、拖动区域）会作用于它。向窗口发送 `Event.Emit` 会到达其 web 视图的页面，`Event.EmitPage` 到达一个页面，`Broadcast` 则到达每个页面。

```go
func (s *Notes) Save(ctx context.Context, text string) error {
	page := mygo.CallerPage(ctx) // the web view's
	...
}
```

页面的 `window.close()` 会让窗口保持打开。内容规则会拦截 web 视图页面的加载，如同对窗口的页面一样（[阻止内容](../windows.md#blocking-content)），规则来自 `WebViewOptions.Page.ContentRules` 或 `Page.SetContentRules`。

### 类型安全的客户端

页面用 `mygo generate` 生成的客户端调用 Go，如同窗口的页面那样：客户端对 web 视图一无所知。原生 UI 项目没有前端，因此也没有客户端：可以给它配一个，比如 `frontend` 目录中的 Vite 应用，并在其 `mygo.json` 中填写[前端](../configuration.md#development-and-the-frontend)的字段：

```json
{
  "name": "Notes",
  "devUrl": "http://localhost:5173",
  "devCommand": "bun run --cwd frontend dev",
  "buildCommand": "bun run --cwd frontend build",
  "frontendDist": "frontend/dist"
}
```

客户端随后就是 `frontend/src/mygo.ts`（`bindings` 会改变位置），`mygo dev` 会在 Go 代码改变时重写它，`mygo build` 则会在构建前端之前重写它；前端依赖客户端所导入的 `mygo-runtime`。加载 `/` 的 web 视图会显示前端：在 `mygo dev` 期间来自开发服务器，在构建中则嵌入其中（参见[页面如何加载](../frontend.md#how-pages-load)）。它的页面会调用用 `mygo.Bind` 绑定的服务，并接收用 `mygo.NewEvent` 声明的事件：

```go
// Notes is bound for the pages.
type Notes struct{ likes atomic.Int64 }

// Liked tells pages how many likes there are.
var Liked = mygo.NewEvent[int]("liked")

// Like counts a like and tells every page.
func (n *Notes) Like() int {
	likes := int(n.likes.Add(1))
	Liked.Broadcast(likes)
	return likes
}

var page *mygo.WebView

func view(c *ui.Context) {
	if page != nil {
		ui.WebView(c, page).Fill()
	}
}

func main() {
	mygo.Bind(&Notes{})
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{Title: "Notes", Content: ui.View(view)})
		page, _ = win.NewWebView(mygo.WebViewOptions{URL: "/"})
		win.Invalidate()
	})
	// ...
}
```

```ts
import { Notes, events } from "./mygo";

likeButton.onclick = async () => {
  count.textContent = String(await Notes.like());
};
events.liked.on((likes) => (count.textContent = String(likes)));
```

带上下文的调用、错误、[通道](../bindings.md#channels)和事件都与窗口中的一样。没有前端构建的页面（例如应用用 `LoadHTML` 加载的 HTML），可以借助桥接注入每个页面的运行时按名称调用，`mygo.call("Notes.Like")` 和 `mygo.on("liked", …)`（参见[不使用生成的客户端](../bindings.md#without-the-generated-client)）。不是应用自己的页面（例如显示 `https://example.com` 的 web 视图）根本无法调用 Go：参见[谁可以调用](../bindings.md#who-may-call)。

## 限制

页面是系统的 web 视图，窗口将其与原生 UI 合成；MyGo 不绘制它。如同 SwiftUI 的 `WebView`：

- 读取其背后内容的效果（如 glass 插件的模糊）看不到页面的任何内容：它不是窗口帧的一部分。glass 插件的 `Glass{Native: true}` 是例外：系统的玻璃视图位于页面和内容之间，因此在 macOS 26 及更高版本上，web 视图上方的工具栏能获得真正的 Liquid Glass。
- 窗口的 `Window.CapturePage` 会在页面所在处显示一个洞；`WebView.CapturePage` 截取页面。

在 Windows 上，WebView2 会把以这种方式显示的页面元素放在它自己的一个窗口下：Narrator 会在原处找到 web 视图，并在那个窗口中找到页面的元素，而不是在 web 视图里面。

## 测试

在 `Tester` 中，`WebViewAt` 返回在窗口某点接收指针的 web 视图，以最后一帧显示的状态为准：即该点下方的那个，除非画在它上面的内容在该处接收指针。在 web 视图上使用 `ClickAt` 和 `Press` 会按下页面，视图按平台告知的方式得知这一点。
