# 富文本编辑器

<!-- preview rich-text-editor 300 -->

`ui/richtext` 包编辑带属性的文本，就像 macOS 上的 NSTextView 和绑定了 `AttributedString` 的 SwiftUI `TextEditor`。它支持：

- **格式：** 粗体、斜体、下划线、删除线、行内代码、字体、字号和颜色。
- **链接：** 单击打开，可从应用或按 ⌘K 设置。
- **段落：** 标题、项目符号列表、编号列表和任务列表、引用、代码块和对齐。
- **附件：** 行内的图像。
- **编辑：** 文字处理器的快捷键和“格式”菜单，以及撤销时让格式随文本一起恢复的功能。
- **交换：** 以富格式复制和粘贴、在输入属性中组合文字的输入法、Markdown 导出，以及能读出格式的屏幕阅读器。

它只布置和绘制视口中的段落，因此长文稿编辑起来像短文稿。

```go
import "github.com/egoist/mygo/ui/richtext"

// Kept by the app, as its other state.
app.doc = richtext.NewDocument(richtext.NewText("Hello"))

// In the view:
if richtext.Editor(c, app.doc).Fill().Changed() {
	app.save(app.doc.Text())
}
```

[笔记示例](../../examples/notes) 是一个带工具栏、链接浮层和“格式”菜单的笔记应用，它把笔记保存为 JSON。

## 文本

`Text` 是带属性的文本：段落，每个都有 `ParagraphStyle`，由文本段组成，每个文本段都有自己的 `Attributes`。它和字符串一样是值类型。它的方法返回新文本，原文本保持不变，新文本与它共享所有未改动的内容。保留一个版本（用于撤销、稍后保存或比较）只花一个指针；其他 goroutine 创建新版本时，仍可从任意 goroutine 读取某个版本。

```go
note := richtext.TextOf(
	richtext.Paragraph{Style: richtext.ParagraphStyle{Heading: 1}, Runs: []richtext.Run{{Text: "Groceries"}}},
	richtext.Paragraph{Style: richtext.ParagraphStyle{List: richtext.Tasks}, Runs: []richtext.Run{
		{Text: "Milk, "},
		{Text: "oat", Attributes: richtext.Attributes{Weight: 700}},
	}},
)
note = note.Format(0, 9, func(a *richtext.Attributes) { a.Color = ui.RGB(37, 99, 235) })
note = note.ReplaceString(note.Len(), note.Len(), "\nBread", richtext.Attributes{})
```

偏移量按 rune 计数，换行符也算在内，和 `Element.TextSelection` 一样。读取一个文本：

- `String` 和 `PlainText` 给出它的字符；
- `Paragraphs` 和 `Runs` 给出它的结构；
- `AttributesAt` 和 `ParagraphStyleAt` 描述一个 rune。

修改一个文本：

- `Replace` 和 `ReplaceString` 改变它的文本；
- `Format` 改变一个区段的属性；
- `FormatParagraphs` 改变区段所触及段落的样式。

留作零值的属性采用编辑器的样式：字体、字号和颜色。`Code`、`Link` 和 `Tag`（应用自己在文本段上的标记，例如提及或评论）只承载各自的内容：在它们边缘输入的文本不会并入其中。

段落的样式及其换行符的属性属于段落本身，文字处理器中也是如此：空段落会记住在那里输入会得到什么样式。段落合并或拆分时，每段都采用其第一个 rune 原来所在段落的样式，AppKit 修正段落样式时也是这样：

- 删除标题后的换行符会保留标题。
- 整个删除标题（包括它的换行符）会留下它后面的段落。
- 粘贴在段落开头的文本会带来它自己的样式。

列表是扁平的，和 Quill、Automerge 一样：段落的 `Indent` 就是它的层级，相邻的项组成一个列表，每个层级都从 1 开始编号（第二级起用字母和罗马数字）。

段落的 `ID` 是应用给它起的名字，可以作为应用保存该段落相关数据的键，例如笔记或时间线。编辑器不显示它；编辑时它会跟着段落文本走：

- 拆分段落时，ID 留在前半段；如果在段落开头按 Enter 把段落往下移，则留在后半段。另一半没有 ID。
- 合并两个段落时保留第一个的 ID，除非第一个是空的。
- 粘贴或插入的段落没有 ID：`Replace` 从不采用它插入文本的 ID，因此不会有两个段落得到同一个 ID。

撤销和重做会让 ID 随文本一起恢复，JSON 会保存它们。剪贴板、HTML、Markdown 和 RTF 会略去它们。可以在创建文本时给出 ID（`TextOf`、`UnmarshalJSON`），也可以让文稿在产生这些段落的那个撤销步骤里，为编辑新产生的段落命名：

```go
app.doc = richtext.NewDocument(chapter)
app.doc.IdentifyParagraphs(func() string { return rand.Text() })

// On save, every paragraph has an ID:
for start, p := range app.doc.Text().Paragraphs() {
	app.store.Save(p.Style.ID, start, p)
}
```

`MarshalJSON` 和 `UnmarshalJSON` 保存文本。`Markdown` 和 `HTML` 导出它，`ParseMarkdown`、`ParseHTML` 和 `ParseRTF` 分别读取 Markdown、HTML 和 RTF，和粘贴时一样。

## 文稿

`Document` 是编辑器所编辑的内容：文本、选区（以及在该处输入会得到的属性），还有撤销历史。应用逐帧保留它。它的方法可以从任何 goroutine 安全调用，所以编辑器显示文稿时，工具栏、菜单或定时器也能格式化、编辑它。编辑器会在下一帧跟上变化。从其他 goroutine 修改时用 `Window.Update` 安排，这样才会有新的一帧。

命令像编辑器的快捷键那样作用于选区。作用于区段时，命令会修改其中的文本段或段落，作为一步撤销。作用于插入符时，字符格式只改变输入属性：接下来输入的文本会继承它，插入符一移动就会忘记，NSTextView 中的 ⌘B 也是如此。

```go
f := app.doc.Formatting() // the selection's formats, for a toolbar
bold := ui.Button(c, "B")
if f.Bold == richtext.FormatOn {
	bold.Background(t.Accent.Alpha(0.18))
}
if bold.KeepFocus().Clicked() {
	app.doc.ToggleBold()
}
```

`Formatting` 把每种字符格式表示为 `FormatOn`、`FormatOff` 或 `FormatMixed`，同时给出选区所在的链接和它第一个段落的样式。这些方法改变文稿：

| 方法 | 作用 |
|---|---|
| `ToggleBold`, `ToggleItalic`, `ToggleUnderline`, `ToggleStrikethrough`, `ToggleCode` | 开启一种格式；选区全部都有该格式时关闭 |
| `Format(fn)` | 改变任何属性，如字体、字号或颜色 |
| `SetLink(url)` | 为选区添加链接、为插入符处的链接设置地址，或插入 URL；`""` 移除链接 |
| `SetHeading(level)`, `ToggleList(kind)`, `ToggleQuote`, `ToggleCodeBlock`, `Indent(levels)`, `SetAlign(a)` | 改变选中的段落 |
| `FormatParagraphs(fn)` | 改变任何段落样式 |
| `ToggleChecked(i)` | 勾选包含 rune `i` 的任务 |
| `InsertText`, `Insert`, `InsertAttachment` | 替换选区 |
| `Edit(fn)` | 以任意方式修改文稿，作为一步撤销 |
| `Undo`, `Redo`, `BreakUndoCoalescing` | 回退、重做步骤 |
| `SetText` | 替换文本，如同打开另一条笔记，并清除历史 |
| `IdentifyParagraphs(newID)` | 为没有 `ID` 的段落以及编辑新产生的段落命名 |

`Edit` 在一个 `Edit` 上运行函数，其中的 `Replace`、`ReplaceString`、`Format`、`FormatParagraphs` 和 `Select` 会作为一步撤销修改文稿。选区会跟随变化，AttributedString 跟踪索引也是如此：在插入文本处，插入符移到文本之后；区段则不会在末端增长。

```go
app.doc.Edit(func(e *richtext.Edit) {
	e.ReplaceString(0, 0, "Dear Ann,\n", richtext.Attributes{})
	e.Format(0, 8, func(a *richtext.Attributes) { a.Italic = true })
})
```

## 编辑器

`Editor` 具有文本区域的外观：内边距、背景、边框和圆角。`EditorBase` 没有这些，适合应用自定义外观的编辑器。

`Changed` 报告用户在编辑器中所做的编辑，而不是应用通过文稿所做的编辑。`EditorOptions` 设置：

- `Label` 和 `Placeholder`；
- `Font`，编辑器的字体和字号；
- `ReadOnly`，用户可以选择、复制文本并单击其中链接；
- `NoMarkdown`，关闭 Markdown 快捷键；
- `OnLink`，改为自行打开链接，而不是由系统打开；
- `OnLinkCommand`，按 ⌘K 时由它询问链接的 URL，例如在浮层中；
- `TagStyle`，设置带 `Tag` 的文本段的样式；
- `Menu`，用它代替编辑器自带的菜单构建上下文菜单，适合有自己菜单的应用；
- `OnKey`，在编辑器处理按键之前被询问，例如文本上方的应用弹出层（补全列表，放在 `Document.CaretRect`）要接管方向键、Enter 和 Escape：返回 true 即取走该按键；
- `Spans`，收到段落绘制成的 span，可以返回拆分、着色或间距不同的 span，文本保持不变，例如应用按语言给代码块着色，或给字母加间距；它的行为变化时修改 `SpansVersion`，让段落重新布置；
- `Theme`，文本的外观（见[主题](#主题)）。

按键与文字处理器相同，macOS 之外用 Control 代替 Command：

| 按键 | macOS | Windows 和 Linux |
|---|---|---|
| 粗体、斜体、下划线 | ⌘B, ⌘I, ⌘U | Ctrl+B, Ctrl+I, Ctrl+U |
| 删除线 | ⇧⌘X | Ctrl+Shift+X |
| 代码 | ⌘E | Ctrl+Shift+E |
| 链接 | ⌘K | Ctrl+K |
| 编号列表、项目符号列表、任务列表 | ⇧⌘7, ⇧⌘8, ⇧⌘L | Ctrl+Shift+7, 8, L |
| 增加缩进、减少缩进 | ⌘], ⌘[（列表中为 Tab 和 ⇧Tab） | Ctrl+], Ctrl+[ |
| 左对齐、居中、右对齐 | ⌘{, ⌘\|, ⌘} | Ctrl+L, Ctrl+E, Ctrl+R |
| 标题 1 到 6、正文 | ⌥⌘1 到 ⌥⌘6, ⌥⌘0 | Ctrl+Alt+1 到 6, Ctrl+Alt+0 |
| 上一个、下一个段落 | ⌥↑, ⌥↓ | Ctrl+↑, Ctrl+↓ |
| 文本开头、结尾 | ⌘↑, ⌘↓ | Ctrl+Home, Ctrl+End |

段落的行为和 Notes、Google Docs 一样：

- Enter 延续列表，在空项上则离开列表。
- 在标题末尾按 Enter 会开始一个正文段落。
- 在列表项、标题、引用或代码块的开头按 Backspace 会先去掉样式，再把段落与上面一段合并。
- 单击任务的复选框会勾选它，单击链接会打开它：仅限 `http`、`https` 或 `mailto` 地址（`ui.SafeToOpen`），和 Markdown 视图打开链接的方式一样，因为在 Windows 上其他协议和路径会启动程序。`OnLink` 则改为自行打开链接。

Markdown 快捷键会格式化正在输入的内容：在段落开头，`# ` 到 `###### ` 生成标题，`- `、`* ` 和 `+ ` 生成项目符号列表，`1. ` 生成编号列表，`[ ] ` 和 `[x] ` 生成任务，`> ` 生成引用，```` ``` ```` 后再按 Enter 生成代码块。在单词两侧，输入结束标记时，`**bold**`、`*italic*`、`_italic_`、`` `code` `` 和 `~~strikethrough~~` 会格式化它们包住的内容；单词内的下划线，例如 `snake_case` 里的，仍只是普通下划线。撤销会把快捷键还原成实际输入的内容。

[“格式”菜单的角色](../menus.md#roles)（`RoleFormatMenu`，或 `RoleBold`、`RoleAlignCenter` 等）会作用于拥有键盘焦点的编辑器。“粘贴并匹配样式”会按输入属性粘贴纯文本。

## 主题

`EditorOptions.Theme` 设置文本的外观：文本、弱化文本、链接、插入符和选区的颜色；代码（行内和代码块中）的字体、字号和颜色，以及代码块的圆角和留白；各级标题的字号、字重、颜色和上下留白；正文段落的首行缩进和段间距；列表的缩进、其标记和复选框的颜色；引用的竖条和文字；以及 `Editor` 的内边距，它会取代窗口主题中的内边距。留作零值的项采用窗口主题下 `DefaultTheme` 的值，因此主题只需设置自己改动的部分，也能跟随窗口进入深色模式：

```go
violet := ui.Hex("#7c3aed")
richtext.Editor(c, doc, richtext.EditorOptions{Theme: &richtext.Theme{
	Link:     violet,
	Checkbox: violet,
	Headings: [6]richtext.HeadingStyle{{Size: 2.2, Weight: 800}},
}})
```

长度以 em 为单位、按编辑器字号换算，因此主题会随 `Font` 缩放；但内边距和引用竖条的宽度以 DIP 计。改变主题会重新布置文本。编辑器显示的颜色随其文本段一起传给屏幕阅读器。

正文段落（标题、列表项和代码除外）默认连排，和笔记中一样。如果是散文，可以用以下两种方式之一把它们区分开：

- `FirstLineIndent` 缩进正文段落的第一行，和印刷小说一样（约 1.2 em）。标题、列表、代码块、空段落或采用其他对齐方式的段落（例如居中的场景分隔符 “* * *”）之后的第一段不缩进；引用的第一段同样不缩进，因为它们都不需要靠缩进来区分。只有开头对齐的段落会缩进。
- `ParagraphSpacing` 在正文段落之间留出间距，屏幕上排版的散文常这样做。标题保留自己的上下间距。

```go
richtext.EditorBase(c, app.doc, richtext.EditorOptions{Theme: &richtext.Theme{
	FirstLineIndent: 1.2,
}})
```

缩进不是字符：插入符、选区、单击和方向键都会跳过它，文字处理器中也是如此。

## 屏幕上的段落

`Document.VisibleParagraphs` 返回显示该文稿的编辑器中视口内的段落：每个段落的索引、起始 rune，以及它各行相对于编辑器元素顶部的位置和高度（单位为 DIP，已计入滚动）。在构建视图时读取它，就能让装订线与段落对齐，例如做时间线、页边批注或段落编号。把装订线放在编辑器旁边，顶部对齐，绘制位置取装订线顶部加上每个段落的顶部：

```go
boxes := app.doc.VisibleParagraphs()
ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
	ui.Box(c).Width(40).Clip().Draw(func(p *ui.Painter, r ui.Rect) {
		for _, b := range boxes {
			p.Fill(ui.Rect{X: r.X + 16, Y: r.Y + b.Top, W: 4, H: b.Height}, t.Accent, 2)
		}
	})
	richtext.EditorBase(c, app.doc).Grow(1)
})
```

这些方框就是编辑器在这一帧绘制段落的位置，因为编辑器在读取段落的同时就完成了布置。如果某一帧中它们的位置与读取时不同——例如编辑器滚动了、宽度变了，或显示了插入符——编辑器会再构建一帧，因此装订线会自动跟随，应用无需监视滚动。段落上下的空间（标题的边距、段落之间的间距）不计入它的方框，因此其顶部的标记与第一行对齐。和绘制一样，`VisibleParagraphs` 只布置视口中的内容，因此长文稿中的开销和短文稿中一样小。在编辑器显示文稿之前，它返回 nil。

## 屏幕上的插入符

`Document.CaretRect` 返回编辑器显示插入符的位置，不论编辑器有没有焦点：一条与插入符高度相同的线，从编辑器元素左上角算起（以 DIP 为单位，已计入滚动），以及它是否在视口内。在构建视图时读取它，就能把弹出层放在插入符处，例如 `/` 或 `@` 之后的补全，其方向键、Enter 和 Escape 由 `OnKey` 接管。把弹出层和编辑器放在同一个盒子里，位置放在插入符底部：

```go
caret, ok := app.doc.CaretRect()
ui.Box(c).Fill().Children(func() {
	richtext.EditorBase(c, app.doc, richtext.EditorOptions{OnKey: app.completionKey}).Fill()
	if app.completing && ok {
		ui.Column(c).Absolute().Left(caret.X).Top(caret.Y + caret.H + 4).Children(func() {
			for _, s := range app.completions {
				ui.Text(c, s)
			}
		})
	}
})
```

和段落一样，插入符的位置就是编辑器在这一帧绘制它的位置：如果某一帧中位置与读取时不同——例如用户输入、编辑器滚动或宽度变化——编辑器会再构建一帧，因此弹出层会跟着插入符。在编辑器显示文稿之前，以及插入符滚出视口（此时弹出层隐藏）时，`ok` 为 false。

## 在格式边缘输入

输入的文本继承插入符前一个字符的属性。在段落开头，它继承后一个字符的属性；替换选区时，继承选区第一个字符的属性。这些是 NSTextView 的规则（在 macOS 27 上实测），ProseMirror、Lexical 和 Quill 也遵循：

- 在粗体末尾输入会延续粗体；在它开头则不会。
- 在链接、代码段或标签末尾输入不会延续它。在它们内部输入则会。
- 删除插入符前的文本后，在插入符移动之前，输入的文本会继承它的属性：删掉一个粗体单词再输入，新文本也是粗体。
- 按 Enter 后，继续输入仍保持按 Enter 前输入的属性。

## 附件

附件是一个对象替换字符（U+FFFC），其 `Attributes.Attachment` 持有 `Attachment`：图像，或应用自己绘制的内容；它按行内盒子的形式布置，坐在基线上。它像一个字符一样被选择、复制和删除，换行会绕开它。比编辑器还宽的盒子会缩放以适应。

```go
photo, _ := ui.DecodeBitmap(data)
app.doc.InsertAttachment(&richtext.Attachment{Image: photo, Name: "Beach", Width: 240})
```

没有图像或 `Draw` 函数时，附件会在盒子里显示 `Name`，就像尚未加载的图像。HTML 和 Markdown 会写出附件的 `URL`。

## 复制与粘贴

复制把选区以三种格式放入剪贴板：

- 编辑器自己的格式（`richtext.Format`，JSON），MyGo 应用会原样粘贴，在应用内连附件一起；
- HTML；
- 纯文本。

粘贴时会读取剪贴板中内容最丰富的格式：编辑器自己的格式，然后是 HTML，然后是 RTF，然后是 Markdown（`transfer.Markdown`，Markdown 视图和应用复制出的就是这种），最后是纯文本。[Markdown 视图](markdown.md)的选区会带着其格式粘贴。

- HTML，即浏览器、邮件、Word 和 Google Docs 复制的那种，会保留标题、列表、任务列表、引用、代码块、对齐、粗体、斜体、下划线、删除线、代码和链接。
- RTF，即 TextEdit、Pages 和 WordPad 在没有 HTML 时复制的那种，会保留段落、对齐、列表、粗体、斜体、下划线、删除线和链接。字体、字号和颜色会被略去，Notes 也会略去它们，因此粘贴的内容采用编辑器的样式。“粘贴并匹配样式”（⇧⌥⌘V）会按输入属性粘贴纯文本。

## 输入法

输入法在插入符处按输入属性组合，组合过程中带下划线，AppKit 和 Qt 都是这样显示的。整个组合与它替换的内容一起构成一步撤销。

## 辅助功能

屏幕阅读器把编辑器读作文本框，带其文本段的格式：

- **VoiceOver** 读取 `AXAttributedStringForRange`：字体、颜色、下划线、删除线和链接。
- **Orca** 读取 ATK 的文本段属性。
- **讲述人**读取 UI Automation 的 Text 模式，可按字符、格式、单词和行读取，给出每个文本段的字体、字重、斜体、行、颜色和标题级别，以及它们不一致时的混合值。

屏幕阅读器也会移动选区。

## 大型文稿

文本是分块的 rope，旁边是持久化的段落序列，因此一次编辑只复制它触及的内容以及通往该处的路径，代价为 O(log n)。编辑器只布置视口中的段落，保留附近段落的布局，并估计其他段落的高度。在 100,000 个带格式段落的文稿中输入一个字符，并在 CPU 上绘制这一帧，所需时间和在 1,000 个段落的文稿中差不多（`ui/richtext` 中的 `BenchmarkTypeFrame`）。

## Markdown

`Text.Markdown` 写出带 GitHub 扩展的 CommonMark：

- 标题、列表、任务列表、引用和围栏代码块；
- `**bold**`、`*italic*`、`~~strikethrough~~`、`` `code` ``、链接和图像；
- 下划线写成 `<u>`；Markdown 的分隔符无法在所在位置打开或关闭的标记，则写成 `<strong>`、`<em>` 和 `<del>`，例如紧挨着 “bar” 之前的粗体 “foo.”。

对齐、字体和颜色没有对应的 Markdown，会被略去。

`ParseMarkdown` 把它读回来，也读取模型或其他应用写的内容：带 GitHub 扩展和脚注的 CommonMark，使用 [Markdown 视图](markdown.md)的解析器（`ui/markdown/parser`）。列表项第一个之后的段落缩进在它下面，表格的行是由制表符分隔其单元格的段落。数学公式保持原样，因为编辑器不显示它。`Markdown` 写出的内容读回来是一样的。
