# 文本输入

<!-- preview text-input 250 -->

`ui.TextInput` 创建一个编辑 `*string` 的单行文本输入框，`ui.TextArea` 则创建多行输入框。它们会随着用户输入修改字符串：直接读取即可；需要跟进处理时用 `Changed`，单行输入框中按下 Enter 时用 `Submitted`。

```go
if ui.TextInput(c, &app.query).Placeholder("Search").Label("Search").Changed() {
	app.results = search(app.query)
}

if ui.TextInput(c, &app.name).AutoFocus().Submitted() {
	app.rename()
}

ui.TextArea(c, &app.notes).Height(160)
```

- `Placeholder` 在输入为空时显示一段文本，在 `TextInput` 中只占一行，在文本区域中会换行。在包裹输入框的字段上，如 `SearchField`、`TokenField` 或 `Combobox`，它会显示在该输入框中。`PlaceholderColor` 用不同于主题 `TextMuted` 的颜色绘制它，例如表单里的警告提示。
- `CaretWidth` 把光标画得比 1 DIP 宽，例如终端的方块光标，或大字号主题所用的宽度；设为 0 则隐藏光标。除此之外它不改变任何东西。
- `Password` 隐藏其中的内容，并且在调用它的那些帧里，让内容不会进入剪贴板和输入法：显示文本的眼睛按钮只要不再调用它即可。只有 `TextInput` 接受它：和所有平台一样，文本区域没有密码模式。
- `AutoFocus` 让输入框一出现就获得键盘焦点，例如对话框的第一个字段。
- `ReadOnly(true)` 显示文本而不让用户修改：它仍能获得焦点，但没有光标，文本可以被选择和复制。
- `MaxLength(n)` 限制用户输入的文本长度不超过 n，计数方式与 HTML 的 `maxlength` 相同，按 UTF-16 代码单元计算：键入、粘贴或由输入法提交的内容都会被裁到放得下的长度。应用设置的值不会被裁剪；n <= 0 表示不限制长度。
- `Disabled(true)` 使它变灰。
- `OnPaste(fn)` 在粘贴内容（Cmd+V 或 Ctrl+V，或编辑菜单、上下文菜单中的“粘贴”）插入之前看到它，并收到剪贴板中的文本；剪贴板里没有文本时为空字符串，例如只有图像时。返回 true 表示接管这次粘贴，之后什么也不会改变，如同 Web 上的 `preventDefault`：

  ```go
  ui.TextArea(c, &app.draft).Lines(1, 8).OnPaste(func(text string) bool {
  	if len(text) > 10_000 {
  		app.attach(text) // a large block becomes an attachment
  		return true
  	}
  	if text == "" {
  		if png := mygo.Clipboard.ReadImage(); png != nil {
  			app.attachImage(png)
  			return true
  		}
  	}
  	return false
  })
  ```

- `OnTextKey(fn)` 在输入框接收不产生文本的按键之前看到它：Enter、Tab、Escape、Backspace 和 Delete、方向键等移动光标的按键，以及带 Cmd 或 Ctrl 的组合键，就像 AppKit 的 `doCommandBySelector` 和网页的 `keydown`。fn 在按键到来时运行，运行于此前键入的按键和文本之后；返回 true 即接管该按键：文本区域不插入换行，`TextInput` 不提交，Tab 不移动焦点。没有接管的按键则照常处理，就像没有 fn 一样。焦点周围的 `Shortcut` 能处理的按键会先交给它，和 `HandleInput` 一样，除非输入框本身要用这些键编辑，例如 Enter 和方向键。fn 可能在帧与帧之间运行，因此要通过传入的 `ui.TextKey` 读写文本，而不是使用从视图中留存的 `Element`（fn 返回后它们会 panic），例如聊天输入框用 Enter 发送、Shift+Enter 换行、Up 把上一条消息放回：

  ```go
  ui.TextArea(c, &app.draft).Lines(1, 8).OnTextKey(func(k ui.TextKey) bool {
  	switch {
  	case k.Key == ui.KeyEnter && k.Mods == 0:
  		app.send(k.Text())
  		k.SetText("")
  		return true
  	case k.Key == ui.KeyUp && k.Mods == 0 && k.Text() == "":
  		k.SetText(app.lastSent())
  		return true
  	}
  	return false // Shift+Enter breaks the line
  })
  ```

`TextInput` 若窄到放不下文本，有焦点时会让光标保持在视图内，没有焦点时则显示文本开头。

`TextRanges` 在调用它的帧中为文本中的连续片段着色并设置字重，例如消息输入框在输入内容中显示提及：

```go
in := ui.TextAreaBase(c, &app.draft).Lines(1, 8)
for _, m := range mentions(app.draft) {
	in.TextRanges(ui.TextRange{Start: m.start, End: m.end, Color: m.color, Weight: 600})
}
```

范围以文本的 rune 为单位，每帧在文本中查找。它们可以重叠和嵌套，就像属性字符串（`NSAttributedString`）的属性：范围会设置 `Color`（alpha 不为零时）和 `Weight`（字重不为零时），范围重叠时，颜色和字重分别取本帧各次调用中最后一个设置它的范围的值。粗体范围中若有带颜色的提及，该提及会同时显示为粗体和彩色：

```go
in.TextRanges(
	ui.TextRange{Start: 0, End: 40, Weight: 700},
	ui.TextRange{Start: 10, End: 16, Color: t.Accent},
)
```

密码输入框不显示任何范围。输入法组合时，组合文本以无样式显示在光标处，范围在它周围保持原有样式。

文本区域至少有数行高，并随文本增长；指定高度后，它就在其中滚动，可用滚轮和滚动条，光标移动时保持在视图内。`Lines(min, max)` 让它按文本在当前宽度下换行后的高度伸展，从 `min` 行到 `max` 行，超过后就滚动，例如消息输入框随输入内容增长：

```go
ui.TextArea(c, &app.draft).Lines(1, 8)
```

文本区域只布置视口中的段落，并保留它们的布局直到它们改变，因此它可以容纳数十万行的文本，如日志或源文件，输入速度依然很快。

## 编辑

文本输入框的编辑方式和平台的文本框一样：用指针选择（双击选择一个词，三击选择一行），用 Shift 和方向键选择，用平台按键按词和行选择；撤销和重做；剪切、复制和粘贴，也可以由“编辑”菜单中的角色执行；还有编辑命令的上下文菜单。它们接受输入法组合的文本，输入法能看到光标周围的文本，因此长按输入、日语转换和预测都像在其他应用中一样工作。输入法组合期间按下的键归它所有，例如 Enter 选择候选、Escape 放弃组合：这些键不会提交任何内容，不会触发快捷键，也不会关闭对话框。`Composing` 报告它是否正在组合，此时组合文本尚未进入字符串。

输入框会在帧之间保留正在编辑的文本、选区和撤销历史；从别处设置字符串会替换文本。它会保留最后一个撤销步骤，因此应用可以重新格式化用户键入的内容而不丢失那一步。把字符串设为 `""` 不会丢弃历史：Undo 可以恢复清除之前的文本。

如果要用应用的文本替换用户的部分或全部文本，应改为在输入框上调用 `ReplaceText(start, end, s)`。它替换从 `start` 到 `end` 的 rune，作为单独的一步撤销，因此撤销能恢复用户写过的内容，重做则再次换成应用的文本。偏移量会被限制在文本范围内，因此 `0, math.MaxInt` 会替换全部内容，包括同一帧中键入的文本：

```go
msg := ui.TextArea(c, &app.message).Label("Message")
if text, ok := app.generated(); ok {
	msg.ReplaceText(0, math.MaxInt, text) // Undo restores what the user wrote
}

in := ui.TextInput(c, &app.draft)
if name, ok := app.mentionPicked(); ok {
	_, caret := in.TextSelection()
	in.ReplaceText(app.mentionStart, caret, "@"+name+" ")
}
```

`ReplaceText` 把光标放到文本末尾，并像键入一样通过 `Changed` 报告这次编辑。单行输入框会把换行当作空格，`MaxLength` 也不会裁剪它。它同样适用于 `TextInputBuffer` 和 `TextAreaBuffer`。

要开始新的编辑会话，清空字符串并更改输入框的[键](views.md#键)。新的身份会得到新的编辑器，不保留旧的选区、组合状态或撤销历史：

```go
// Keep secret and secretSession in app state.
ui.TextInput(c.Key(app.secretSession), &app.secret).Password().Label("Secret")

if ui.Button(c, "Cancel").Clicked() {
	app.secret = ""
	app.secretSession++ // the next build creates a fresh editor
}
```

如需按索引存储，可以用 `ui.TextInputBuffer` 和 `ui.TextAreaBuffer` 编辑 `*ui.TextBuffer`。从外部用 `buffer.Set("")` 替换其内容时，控件会在下次与缓冲区同步时丢弃撤销和重做历史以及组合状态；选区则被限制到新文本范围内。无论是更改键还是替换缓冲区，都不保证旧文本已从内存中抹去；已有的 `TextSnapshot` 值仍保留其内容。

单行输入框会把粘贴文本中的换行替换为空格。可以用 `OnPaste` 在多行内容被改写或插入之前拒绝它：

```go
ui.TextInput(c, &app.secret).Password().OnPaste(func(text string) bool {
	return strings.ContainsAny(text, "\r\n") // import "strings"
})
```

## 光标

`TextSelection` 以文本中按 rune 计的偏移返回选区，两者相等处即光标；`SetTextSelection` 可以移动选区，例如应用补全正在键入的词后，把光标放到词后：

```go
input := ui.TextArea(c, &app.draft)
if start, _ := input.TextSelection(); app.completed != "" {
	app.draft, start = complete(app.draft, start, app.completed)
	input.SetTextSelection(start, start)
}
```

`TextSelection` 按顺序返回选区。`TextAnchorCaret` 返回从选区起点到光标的范围，这样应用就能区分向前和向后做出的选区：编辑器自身用于扩展选区的按键（例如 Shift-Home 移到行首）会从锚点开始扩展选区：

```go
anchor, caret := input.TextAnchorCaret()
input.SetTextSelection(anchor, lineStart(app.draft, caret))
```

## 行旁

`TrackLines` 把文本区域布置其文本各行的位置记录到 `ui.TextLines` 中，供需要在行旁绘制的应用使用：例如在每行的第一显示行旁画行号，或在一行的某些片段上画标记。这里的一行指两个换行符之间的文本，如果文本区域对它换行，就会占多个显示行。`Top(i)` 是第 `i` 行在区域内容中的顶部位置，从内边距下缘算起，尚未计入滚动；`At(y)` 返回某个高度处的行；`Rows(i)` 是第 `i` 行每个显示行起始处的 rune；`Height` 是文本的高度。配合区域的 [`ScrollState`](scroll.md#保持偏移)，行号栏就能为视口中的行编号：

```go
const pad = 8 // the text area's top padding
ui.Row(c).Grow(1).Children(func() {
	ui.Box(c).Width(48).FillHeight().Draw(func(p *ui.Painter, r ui.Rect) {
		top := r.Y + pad - app.scroll.Y // the top of the area's content
		p.Clip(r, 0, func() {
			for i := app.lines.At(app.scroll.Y); i < app.lines.Count() && top+app.lines.Top(i) < r.Y+r.H; i++ {
				p.Text(r.X+8, top+app.lines.Top(i), strconv.Itoa(i+1), 12, p.Theme().TextMuted)
			}
		})
	})
	ui.TextArea(c, &app.source).Padding(pad, 12).Grow(1).FillHeight().
		TrackLines(&app.lines).TrackScroll(&app.scroll)
})
```

区域尚未布置的行，其高度只是估算值，滚动条也同样如此，直到它显示这些行。在 `Draw` 中，行是当前帧的行；构建视图时，则是上一帧的行。

## 错误

在[字段](form.md)中，`Error` 会把值标记为无效：输入框会用主题的 `Danger` 颜色绘制边框，辅助技术会把它和消息一起读出。

```go
ui.Field(c, "Email", func() {
	ui.TextInput(c, &app.email)
}).Error(app.emailError)
```

## 没有外观

`ui.TextInputBase` 和 `ui.TextAreaBase` 是没有内边距、背景、边框和圆角的文本输入框，适合自行设计的输入框：见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一个文本框或文本区域，其名称来自 `Label` 或所在字段，其值就是文本，并带有光标和选区；除非是只读的，否则可以像键入一样编辑。密码字段会隐藏其值。
