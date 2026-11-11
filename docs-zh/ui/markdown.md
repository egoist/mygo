# Markdown

<!-- preview markdown 440 -->

包 `ui/markdown` 在原生 UI 中显示 Markdown：CommonMark、GitHub 的表格、任务列表、删除线和自动链接、脚注和数学公式。它是为语言模型的回复而构建的，这些回复逐 token 流式传入：追加的文本单独解析，写了一半的语法会显示成它将变成的样子，文本则平滑地淡入。

```go
import "github.com/egoist/mygo/ui/markdown"

// Kept in the app's state, shared by every view.
app.md = markdown.Options{}

// In the view:
markdown.View(c, app.readme, &app.md)
```

`View` 显示已完成的文稿。每一帧都给它源文本，就像给 `Text` 一个字符串：它只重新解析自上一帧以来改变的部分，并保留对其余部分的处理结果，包括其段落的文本排版。

## 流式回复

`Stream` 显示仍在写入的 Markdown。把陆续到达的回复 token 追加到字符串中，完成后在同一个位置改用 `View` 显示它，让视图接着走下去：

```go
func (app *chat) message(c *ui.Context, m *Message) {
	if m.Done {
		markdown.View(c, m.Text, &app.md)
	} else {
		markdown.Stream(c, m.Text, &app.md)
	}
}
```

追加文本时，只解析追加位置之前仍未闭合的最外层块；如果没有这样的块，就只解析新增的行：一个 token 在长回复末尾的开销与在开头相同，已显示的块既不会重新解析，也不会重新排版。

回复的末尾会显示其语法最可能变成的样子，如聊天应用所显示的那样，而不改变文本：`**bol` 会显示为粗体，未闭合的行内代码段会按代码显示，`$$x_i`（以及启用 `ExtDollarMath` 时的 `$x_i`）会按数学公式显示，其中的 `_` 和 `*` 属于 TeX，而不是强调；`[the docs](https://ex` 是一个目标地址还没传完的链接（尚不可点击），未闭合的代码围栏则按代码显示。还可能变成其他内容的行会等待后面的内容：一行几个标记，如 `-` 或 `==`，会让上面的段落变成标题；表格的表头则会等待分隔行，以免短暂显示成一段竖线。已完成的块永远不会改变，完成后用 `View` 显示的回复，与 CommonMark 读取的完全一致。

### 节奏与淡入

token 会一阵一阵地到达。`Stream` 则以均匀的节奏显示文本，一次一个词，在四分之一秒内跟上已经到达的内容，每个词都淡入。`Options.Reveal` 可以改变方式：

```go
app.md.Reveal = &markdown.Reveal{
	Unit:    markdown.ByLine,         // a line at a time, once complete
	CatchUp: 400 * time.Millisecond,  // text waits at most this long
	Fade:    500 * time.Millisecond,  // and fades in over this
}
```

`ByWord`（默认）、`ByCharacter`、`BySentence`、`ByLine` 和 `ByParagraph` 一次显示一个单位，后三个只在单位完成时显示：句子在句号、问号或感叹号后跟一个空格处结束，或在表意句号（。）或行尾处结束。`AtOnce` 则随到随显示。负的 `Fade` 会关闭淡入，桌面的减弱动态效果也一样。文本通过颜色淡入，块的其他部分（列表标记、代码块、表格的行）则通过不透明度淡入，这只影响绘制：不会为此重新排版任何内容。消息切换到 `View` 时仍待显示的内容会继续到来。

### 回复显示完成之后

回复的最后几个字节会在最后几个词显示之前到达：节奏仍在追赶，它们也仍在淡入。`Revealed` 接受 `View` 或 `Stream` 返回的元素，报告它是否已显示全部文本并完成淡入，这样聊天会等到此时才显示回复的时间和操作，而不是在文字还没到齐时就显示：

```go
var md ui.Element
if m.Done {
	md = markdown.View(c, m.Text, &app.md)
} else {
	md = markdown.Stream(c, m.Text, &app.md)
}
if m.Done && markdown.Revealed(md) {
	ui.Text(c, m.Time).TextColor(c.Theme().TextMuted)
}
```

它说明的是这一帧显示的内容：要在视图之后读取它，并在它下面构建它决定的内容。视图在显示和淡入期间会请求帧，因此淡入结束后时间会自行显示。立即显示的文本（`AtOnce`，或没有 `Stream` 留下待显示内容的 `View` 文本）在第一帧就已显示；没有淡入时（负的 `Fade`、减弱动态效果），则在最后一个词显示时就已显示。更多文本会让它再次变为 false。对于 `Stream`，它只说明到目前为止到达的内容已经显示，因此还要检查回复是否完整，如上面的 `m.Done`。

## 一个聊天

把每条消息显示为[列表](list.md)中跟随末尾的一行：列表在回复增长时保持末尾可见，用 `FollowAnimation` 滑向它，直到用户向上滚动；用户滚回末尾后，它会再次跟随。`AtEnd` 和 `ScrollToEnd` 可以用来做一个回到最新消息的按钮。

```go
app.list.Key = func(i int) any { return app.messages[i].ID }
app.list.FollowEnd = true
app.list.FollowAnimation = &ui.Smooth
ui.List(c, &app.list, len(app.messages), func(i int) {
	app.message(c, app.messages[i])
}).Grow(1).Gap(16).Justify(ui.End)
```

[聊天示例](../../examples/markdown-chat)就是这样流式显示预设回复的。

## 选择和复制

视图中的文本是一个跨所有块的选区，如在浏览器中那样。复制会把选中的文本放到剪贴板，段落之间有空行，列表项各占一行，表格单元格以制表符分隔；它会以 HTML 格式复制，以便连同格式粘贴到[富文本编辑器](rich-text-editor.md)、邮件或文字处理软件中；也会以 Markdown 格式复制（`transfer.Markdown`）。上下文菜单中的 `Copy as Markdown`，以及 Shift+Cmd+C（Linux 和 Windows 上为 Shift+Ctrl+C），会复制选区的源文本，补全它截断的强调、代码和链接；没有选区时则复制整个文稿。回复流入时，文本会保持选区。

`ShareSelection` 把选区交给视图周围的 `Selectable` 容器，例如跨越聊天各条消息的一个选区。

## 链接和图像

指向 `http`、`https` 和 `mailto` 地址的链接（`ui.SafeToOpen`，如富文本编辑器打开它们那样）会像[链接](link.md)那样打开，并在工具提示中显示其地址；不带协议前缀的路径会在视图周围的路由器中前往该处；`#a-heading` 会滚动到该锚点的标题（`Slug`）；其他协议的链接，如 `javascript:`，什么也不做。`OnLink` 则处理每次点击：

```go
app.md.OnLink = func(url string) { app.open(url) }
```

`LinkMenu` 给每个链接一个自己的[上下文菜单](context-menu.md)，传入它的目标地址，例如一个在回复中链接文件的聊天，会为右键点击的文件提供 `Copy Path` 和 `Reveal in Finder`。链接的菜单会替换视图周围的菜单，如同其 `Selectable` 容器的菜单，而 `EditItems` 会把文本的 `Copy` 和 `Select All` 放回去；指向 `#a-heading` 的链接、脚注引用，以及目标地址还没传完的链接，会保留它们周围的菜单：

```go
app.md.LinkMenu = func(m *ui.Menu, url string) {
	if m.Item("Copy Path").Chosen() {
		app.copyPath(url)
	}
	if m.Item("Reveal in Finder").Chosen() {
		app.reveal(url)
	}
	m.Separator()
	m.EditItems()
}
```

`Anchor` 在某个标题处打开文稿，传入它的 slug（锚点名），可带或不带 `#`：比如一个文档阅读器从搜索结果打开 `guide.md#install`，或回到用户上次所在的位置。视图会在文稿显示时滚动到它，在另一个文稿替换它时以及 `Anchor` 改变时也是如此；`AnchorVersion` 会再次请求同一个标题。处理 `#a-heading` 链接的 `OnLink` 会设置它：

```go
app.md.OnLink = func(url string) {
	if strings.HasPrefix(url, "#") {
		app.md.Anchor = url
		app.md.AnchorVersion++
		return
	}
	app.open(url)
}
```

`LinkIcon` 在链接文本的开头放一个图标，由目标地址和周围文本的大小决定：一个像 `InlineMath` 那样生成的盒子，点击效果与该链接相同，并停留在它第一个词所在的行上。作为文本、HTML 和 Markdown 复制时，它都不产生内容。当它返回 false 时，链接没有图标；仍在流式传入的链接也暂时没有。链接从 `Theme.Link` 获取颜色：

```go
app.md.LinkIcon = func(dest string, size float32) (markdown.InlineObject, bool) {
	if !strings.HasPrefix(dest, "http") {
		return markdown.InlineObject{}, false
	}
	return markdown.InlineObject{
		Width: size * 1.2, Height: size * 0.86, Depth: size * 0.14,
		Draw: func(p *ui.Painter, r ui.Rect) {
			p.Icon(globe, ui.Rect{X: r.X, Y: r.Y, W: size, H: size}, app.md.Theme.Link)
		},
	}, true
}
```

图像在 `LoadImage` 加载它们之前会显示描述文字，加载在各自的 goroutine 上进行；如果没有它，图像就一直只是描述文字，因为视图自身不会从网络加载任何内容。`FileImages(dir)` 加载一个目录中的文件。仅由图像组成的段落按它们原本的大小显示，最宽不超过视图。

```go
app.md.LoadImage = markdown.FileImages(filepath.Dir(app.path))
```

## 代码

围栏代码用[代码编辑器](code-editor.md)的分词器和配色按语言高亮，逐行基于前一行的状态，因此流式传入的块只高亮新增的行；它的各行按块排版，因此增长时只重新排版最后一个块。每个块会在代码上方显示语言和复制按钮，代码可横向滚动（`WrapCode` 则让它换行）。正在写入的块还没闭合时，按钮会等待。

`CodeBlock` 代替 `DefaultCodeBlock` 构建代码块，`Code` 则按它列出的语言构建这些块，例如图表。每个都会获得该块的语言、代码，以及围栏是否仍未闭合，并可以使用默认实现的各部分：

```go
app.md.Code = map[string]func(*ui.Context, *markdown.CodeBlockView){
	"mermaid": func(c *ui.Context, b *markdown.CodeBlockView) {
		if b.Open { // still streaming: show the code until it closes
			markdown.DefaultCodeBlock(c, b)
			return
		}
		ui.Image(c, app.diagram(b.Code()))
	},
}
app.md.CodeBlock = func(c *ui.Context, b *markdown.CodeBlockView) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, b.Language).Bold()
		b.Body(c)       // the highlighted, selectable code
		b.CopyButton(c) // the copy button
	})
}
```

## 数学和 HTML

`$$` 与 `$$`、`\[` 与 `\]` 或 `\(` 与 `\)` 之间的数学公式以代码字体显示其 TeX，正如语言模型所写的那样；启用 `ExtDollarMath` 扩展时 `$…$` 也是如此，但美元符号读作货币的地方除外。`Math` 则构建展示公式的块，例如使用你自己的渲染器；仍在流式传入的块对 `Block` 是 `Open` 的，它可以显示 TeX 直到闭合。

`InlineMath` 把文本中的数学公式变成段落里的一个对象，由它的 TeX 和周围文本的大小决定：一个宽 `Width`、基线上方 `Height`、下方 `Depth` 的盒子，它随词一起换行，由 `Draw` 绘制。作为文本或 Markdown 复制时，它是其源文本，如 `$x_i$`。当它返回 false 时，数学以 TeX 显示。每当其段落改变时都会询问它，例如流式传入时每一帧，因此要保留它生成的内容：

```go
app.md.InlineMath = func(tex string, size float32) (markdown.InlineObject, bool) {
	f, err := app.formula(tex, size) // laid out once, kept by TeX and size
	if err != nil {
		return markdown.InlineObject{}, false
	}
	return markdown.InlineObject{Width: f.W, Height: f.Ascent, Depth: f.Descent, Draw: f.Draw}, true
}
```

原始 HTML 显示为文本，这样回复就无法隐藏任何内容。在文本中，模型所写的格式标签（`<b>`、`<i>`、`<u>`、`<s>`、`<code>`、`<kbd>`、`<mark>`、`<sub>`、`<sup>` 和 `<br>`）会生效；`HTML` 构建 HTML 块，例如用于 `<details>` 或 `<think>`。

## 样式

视图的外观与窗口一致，使用窗口的字体和颜色。`Theme` 用来设置要改的部分，其余部分采用 `DefaultTheme` 针对窗口主题的值，因此只设置一个字段的主题只会改变一件事：

```go
app.md.Theme = &markdown.Theme{
	FontSize:     15,
	Link:         ui.Hex("#7c3aed"),
	CodeFont:     "JetBrains Mono",
	BlockSpacing: 1,
	Headings: [6]markdown.HeadingStyle{
		{Size: 1.8, Weight: 800, Rule: true},
		{Size: 1.4, Rule: true},
	},
}
```

主题设置字体、代码字体及其行高；文本、次要文本、链接、粗体文本和代码的颜色；代码的大小；块与列表项之间的间距；每级标题的大小、字重、颜色、间距和分隔线；列表缩进；块引用的竖条和文本；代码块的背景、边框、圆角、内边距和 token 颜色（一个 `editor.Theme`）；表格的线、表头、条纹、内边距和最宽列；标记文本；以及复选框。长度以 em 为单位，乘以字体大小，因此主题随字体缩放。`LinkWeight` 是链接文本的最小字重，用于仅靠颜色不足以让链接看起来像链接的场合，比如灰色打印主题，或对色盲读者。

`Block` 替换任何块的显示方式：每个块都会先询问它，当它返回 true 时由它构建，使用文稿的块和行内元素（参见[解析器](#解析器)）：

```go
app.md.Block = func(c *ui.Context, d *parser.Document, i int) bool {
	b := d.Block(i)
	if b.Kind != parser.ThematicBreak {
		return false
	}
	ui.Text(c, "⁂").TextAlign(ui.Center).AlignSelf(ui.Stretch)
	return true
}
```

## 辅助功能

标题是相应层级的标题，列表及其项是列表和列表项，链接是链接，表格是包含行、列标题和单元格的表格，任务项是复选框（无论是否勾选），图像是按描述命名的图像，代码块是按语言命名的组，并带有复制代码按钮。

## 解析器

解析器是一个独立的包 `ui/markdown/parser`，它不导入 ui 的任何东西，因此只读取 Markdown 的程序（导入它的富文本编辑器、渲染 HTML 的服务器）不会把视图带进来。`Parse` 读取字符串，`Append` 添加文本，`Edit` 修改一个范围，每种操作都只重新解析改动触及的部分。一次编辑会从它前面的顶层块开始解析，直到遇到一个块：这个块开始时没有任何未闭合内容，而之前某个块也在同一位置开始；从这里起，它保留原有的块：在长文稿的预览中输入时，只会解析少数几个块。定义位置晚于使用位置的链接会让使用它的块重新解析其行内元素，而且只解析这些。

```go
d := parser.Parse(src)
d.Append(token)
for i := range d.Children(0) { // the top-level blocks
	b := d.Block(i)
	if b.Kind == parser.CodeBlock {
		fmt.Println(b.Language(), d.Lines(i))
	}
}
html := d.HTML()
```

块和行内元素是按文稿顺序排列的扁平切片，每个都带有它在源中的字节范围（`Start` 和 `End`），以及它子元素之后的索引（`Next`）。`Inlines` 在第一次被询问时解析一个块的行内元素。`New(ext)` 读取 `Default` 之外的其他扩展，`SetStreaming` 像 `Stream` 那样补全末尾。

它通过 CommonMark 0.31.2 规范的全部 652 个示例以及 GitHub 扩展的示例，对那些在其他解析器中很慢的输入也能保持线性时间，和 cmark 一样，并且不依赖标准库之外的任何东西。

对中文、日文和韩文放宽了一条规则，因为它们不在强调前后加空格：在 CJK 字符、全角标点或弯引号旁边，`*` 和 `~~` 即使在 CommonMark 中会被标点阻止的地方也能开启和闭合，因此 `**结论。**后文` 是粗体。`_` 和单个 `~` 保持 CommonMark 的规则。规范中那个位置没有包含 CJK 文本的示例，所以它们仍然全部通过。

## 性能

在 Apple M5 上以 `CGO_ENABLED=0 GOMAXPROCS=1` 测量一条 50 KB 的回复，它包含标题、嵌套列表、代码、表格和链接：

| | 时间 | 分配 |
|---|---:|---:|
| 完整解析它，包括行内元素 | 0.3 ms | 0.8 MB，1,300 次分配 |
| 追加一个 4 字节的 token，解析尾部的行内元素 | 2 µs | 600 B，2 次分配 |
| 完整显示它的一帧，没有变化 | 1.3 ms | 16 KB，350 次分配 |
| 追加一个 16 字节 token 的一帧 | 0.8 ms | 36 KB，350 次分配 |

帧会像视图那样构建每个块的元素，同时保留解析结果、段落的区间及其文本排版：比聊天回复长得多的文稿，最好一次只显示一节。

<!-- repository-only:start -->

## 运行基准测试和测试

```sh
CGO_ENABLED=0 GOMAXPROCS=1 go test ./ui/markdown -run '^$' -bench . -benchmem
go test ./ui/markdown -run 'TestCommonMark|TestGFM|TestAppend|TestEdits|TestPathological' -v
```

<!-- repository-only:end -->
