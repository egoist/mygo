# 列表

<!-- preview list 290 -->

`ui.List` 显示任意长度集合的行：它只构建视口中的行，以及视口外的几行，所以数百万行的列表和十行的列表一样快。行的高度由内容决定，各行可以不同：列表会在行显示时测量它，并据此估算其他行。

```go
ui.List(c, nil, len(app.files), func(i int) {
	ui.Text(c, app.files[i].Name).Padding(6, 12)
}).Grow(1)
```

给列表一个大小，或在父元素中让它增长。没有大小的列表，或只有 `MaxHeight` 的列表，和它的行一样高。

## 布局

`Gap` 设置行与行之间的间距，`Padding` 给内容加内边距并随内容滚动，`Justify(ui.End)` 把没有填满列表的行放在底部，就像聊天刚开始时的那几条消息。在列表里构建的其他东西会在它没有行时显示，例如一条表示它为空的消息：

```go
ui.List(c, nil, len(results), func(i int) {
	ui.Text(c, results[i].Title).Padding(6, 12)
}).Grow(1).Children(func() {
	if len(results) == 0 {
		ui.Text(c, "No results").TextColor(c.Theme().TextMuted).Padding(12)
	}
})
```

## 比列表宽的行

`RowWidth(w)` 让行至少 `w` DIP 宽，如表格的列所要求：行比列表宽时，列表也会横向滚动；行不比列表宽时，则增长到列表的宽度。行上方的表头通过 `ScrollWith` 跟它们一起水平滚动；把它裁剪起来，免得滚出它的内容露出来。`ui.Table` 对它的列就是这样做的。

```go
head := ui.Row(c).Clip().Children(app.titles)
list := ui.List(c, &app.files, len(app.files)).Grow(1).RowWidth(app.columnsWidth())
list.Rows(app.row)
head.ScrollWith(list)
```

## 列表状态

列表按行而不是按偏移保存位置，所以视口中的行在周围的行被测量、增长、出现和消失时留在原处，它的偏移能精确到一个 DIP 的几分之一，即使跨越数百万行。在你的状态里给它一个 `ui.ListState` 代替 `nil`，用来指定它的行为并移动它：

- `Key` 返回行对应条目的身份，例如它的 ID：行的状态（焦点、正在编辑的文本等）跟随它的条目，当行在上方被添加或移除时，列表保留它的位置和它的选择，例如加载更早的消息时。
- `KeepOffset` 让带 `Key` 的列表在行移动时保持偏移，而不是保持顶部的条目，适用于随变化重新排序的行，比如进程监视器中的行：行的状态和选择仍然跟随它们的条目。
- `FollowEnd` 让列表从末尾开始，并在行出现或增长时保持末尾在视图中，直到用户滚离它，就像聊天或日志那样；滚回末尾时会再次跟随。`FollowAnimation` 让它滑过去，用弹簧（`&ui.Smooth`）或曲线，而不是立刻显示末尾：新增的内容从下方升上来，就像聊天应用滚动时那样，用户滚动则会停止它。每次布局都会读取当时的 `FollowEnd`，`HoldPlace` 只让列表在接下来的一次布局中留在原处：在一次点击展开最后一条消息中的折叠内容的那一帧调用它，被点击的那行就会留在指针下，而不是在行增长时跟随末尾。
- `Header` 为充当分区标题的行命名：顶部分区的标题会固定在那里，它的行在下面滚动，直到下一个标题把它推开。给标题一个背景。
- `Label` 返回行的文本：列表有焦点时输入它的前几个字母会选择它，如 Finder 或 Explorer 中那样，辅助技术则把它读作行的名称。

给每个列表一个自己的 `ListState`，并在构建列表之前设置它的字段。

## 选择行

- `Selected` 让点击，或列表拥有键盘焦点时的 Up、Down、Home 和 End（Linux 和 Windows 上还有 Page Up 和 Page Down）选择一行，选中的行用强调色显示；列表的 `Changed` 报告新的选择，`Submitted` 报告双击或 Enter。`-1` 表示没有选择行。有 `Key` 时，选择会在行被添加、移除或排序时跟随它的条目，条目离开列表时则变成 `-1`。列表会在构建时、同一次构建过程中写入它，所以你在列表之后构建的东西（读取所选行的详情面板）必须把 `-1` 当作没有选择。没有 `Key` 时，选择留在它的索引上。
- `Selection` 让用户选择多行，保存在一个 `ui.Selection[K]` 中，有 `Key` 时按条目的键，没有时按索引：点击选择一行，Cmd 点击（Linux 和 Windows 上 Ctrl 点击）添加一行或把它移出选择，Shift 点击，或按住 Shift 配合方向键，会选择从最后选中的那一行到当前行的范围；Cmd+A 全选。在 Linux 和 Windows 上，Ctrl 加方向键只移动而不选择，Ctrl+Space 添加或移除当前行。同时设置的 `Selected` 是最后选择的那一行。

一个表格和它下方所选行的编辑器：

```go
app.list.Key = func(i int) any { return app.cols[i].Name }
app.list.Selected = &app.colRow
ui.List(c, &app.list, len(app.cols), func(i int) {
	ui.Text(c, app.cols[i].Name).Padding(6, 12)
}).Grow(1)
if app.colRow >= 0 { // -1 once another table's columns replaced the chosen one
	columnEditor(c, &app.cols[app.colRow])
}
```

当同一个列表显示另一组行时，比如换成另一个表格的行，就重新初始化它的状态（`app.list = ui.ListState{}`），让它忘记之前选择的条目和滚到的位置。

按路径选择多个文件：

```go
type fileList struct {
	files  []File
	chosen ui.Selection[string]
	list   ui.ListState
}

func (app *fileList) view(c *ui.Context) {
	app.list.Key = func(i int) any { return app.files[i].Path }
	app.list.Label = func(i int) string { return app.files[i].Name }
	app.list.Selection = &app.chosen
	ui.List(c, &app.list, len(app.files), func(i int) {
		ui.Text(c, app.files[i].Name).Padding(6, 12)
	}).Grow(1)
	if ui.Button(c, fmt.Sprintf("Delete %d", app.chosen.Len())).Clicked() {
		for path := range app.chosen.All() {
			app.delete(path)
		}
		app.chosen.Clear()
	}
}
```

已离开列表的条目的键会留在选择中，直到你把它们移除。`Element.ClickModifiers` 会给出任意点击的修饰键，供你自己做同样事情的控件使用。

在带 `Selection` 的列表中，右键点击选择之外的行，会在它的上下文菜单打开之前先选中它，如 Finder 和 Qt 那样，所以菜单作用于打开它的那一行；在已选择的行上，选择则保持不变。`MenuKeepsSelection` 关闭这一点：菜单在点击的行上打开，其构建器知道自己的行，选择保持不变。只用 `Selected` 的列表从不在右键点击时选择。

列表有焦点时，菜单键和 Shift+F10 会在所选行的下方打开它的上下文菜单：行中带 `ContextMenu` 的最外层元素；没有选中行，或该行没有这样的元素时，则用列表自己的。

```go
app.files.Selection = &app.chosen
app.files.MenuKeepsSelection = true // the menu acts on the row clicked alone
```

`DragSelect` 与 `Selection` 一起使用时，指针从按下的那一行开始拖过哪些行，就选择哪些行，效果相当于对指针下的行做 Shift 点击（如果按下的那一刻按住 Cmd，或 Linux 和 Windows 上的 Ctrl，还会在已选行之外一并选上）；指针接近或越过列表顶部或底部时，列表会滚动。在未选择的行上按下，会在指针按下时就选中它，如电子表格那样，而不是等到松开；在已选择的行上按下则会等到松开再处理，这样拖动所选内容时能保留这一选择。`DragSelecting` 报告指针按住期间正在进行的选行拖动，供跟随选择的工作使用，比如计算所选行之和，好等拖动结束再做。列表不能同时用 `DragSelect` 和 `Reorder`（或 `DragRows`）。

`ExtendTo(row)` 选择从 Shift 扩展的起点到 `row` 之间的行，如同对 `row` Shift 点击那样，供你自己的快捷键使用，例如把选择扩展到最后一行的：

```go
if app.extendToEnd {
	app.list.ExtendTo(len(app.files) - 1) // after the list is built
}
```

可以用行里的元素判断双击落在行的哪一部分，而不从行那里拿走点击，行会继续负责选择：`RowDoubleClicked` 报告指针在元素上时对行的双击。在元素上使用 `DoubleClicked` 则会拿走点击，行就不会选择。

```go
ui.List(c, &app.list, len(app.files), func(i int) {
	name := ui.Text(c, app.files[i].Name).Padding(6, 12)
	if name.RowDoubleClicked() {
		app.rename(i)
	}
})
```

`RowClicked` 以同样的方式报告单击，用于对单击采取行动的行，例如文件列表打开文件，或侧边栏打开它对应的页面，且行内没有按钮拿走点击：行会拿走它，所以它的 `Label` 指出拿走点击的是什么，供输入前几个字母、屏幕阅读器和 `Tester.Click` 使用。它报告主按钮不带修饰键的每次点击，对已经选中的行也一样，而 `Changed` 不会报告新的内容。移动选择的方向键不会报告，Shift 或 Cmd 点击扩展选择也不会。双击会用 `RowClicked` 报告第一次点击，用 `RowDoubleClicked` 报告第二次，如各系统的列表那样。辅助技术按下行时，会报告给行中第一个询问此事的元素；而在不选择行的列表中，一问就会让行拿走点击。

```go
ui.List(c, &app.sidebar, len(app.pages), func(i int) {
	if ui.Text(c, app.pages[i].Title).Padding(6, 12).FillWidth().RowClicked() {
		app.show(i)
	}
})
```

自己绘制悬停和按下的行，例如聊天列表的行，要询问用 `Rows` 构建的行，而不是自己的元素：`ListRow` 的 `Hovered`、`Pressed` 和 `PointerPosition` 报告行的状态，就像行自己的元素会报告的那样。行内元素上的 `Pressed` 或 `Hovered` 会让那个元素拿走按下，行就不会选择，对行的 `Label` 的 `Tester.Click` 也不会到达它。按下会在指针按下的地方开始一个波纹：

```go
ui.List(c, &app.chats, len(app.items)).Grow(1).Rows(func(row ui.ListRow) {
	bg := ui.Transparent
	switch {
	case row.Pressed():
		bg = row.Context.Theme().SurfacePressed
		if app.ripple.Row != row.Index {
			x, y, _ := row.PointerPosition()
			app.ripple = Ripple{Row: row.Index, X: x, Y: y, At: time.Now()}
		}
	case row.Hovered():
		bg = row.Context.Theme().SurfaceHover
	}
	ui.Box(row.Context).Background(bg).Padding(8, 12).Children(func() {
		ui.Text(row.Context, app.items[row.Index].Title)
	})
})
```

在不选择行的列表中，`Pressed` 让行拿走按下。

## 滚动

`ScrollTo(row, align)` 把一行显示在列表的 `ui.Start`、`ui.Center` 或 `ui.End` 处，`ScrollIntoView(row)` 以最少的滚动显示它，`ScrollToEnd` 则滚到末尾，包括尚未构建的行：列表会在布局这一帧时滚动到行测量后所在的位置。`Visible` 返回视口中的第一行和最后一行，`AtEnd` 说明末尾是否显示，用于在列表接近已加载内容的末尾时加载更多。

一个聊天：

```go
app.chat.Key = func(i int) any { return app.messages[i].ID }
app.chat.FollowEnd = true
app.chat.FollowAnimation = &ui.Smooth
if first, _ := app.chat.Visible(); first < 5 && app.more {
	app.loadOlder() // above the messages in view, which stay put
}
if !app.chat.AtEnd() && ui.Button(c, "Jump to latest").Clicked() {
	app.chat.ScrollToEnd()
}
ui.List(c, &app.chat, len(app.messages), func(i int) {
	message(c, app.messages[i])
}).Grow(1).Justify(ui.End)
```

## 重新排序

`Reorder` 让用户把行拖到列表中的另一个位置：选择的行（如果拖动的行在其中，否则就是被拖动的那一行；列表会按顺序把这些行传给你），以及它们要插到哪一行之前（插到末尾时则是行数）。把它们移到那里：列表在拖动时会显示它们会去哪里，并在接近边缘时滚动。

```go
app.list.Reorder = func(rows []int, to int) {
	app.items = move(app.items, rows, to)
}
```

本身就是按钮的行（点击时会打开它所显示的内容）也能重新排序：在行的按钮上按下，指针一移动就拖动行，不移动则点击按钮。

## 把行拖出去

`DragRows` 让用户把行拖出列表，放到别处：选择的行（当拖动的行是其中之一时），否则是那一行，拖动 `DragRows` 返回的值，`ui.Drop` 和 `ui.DragOver` 像接受任何被拖动的值一样接受它。列表本身不接受任何东西，也不在行之间显示线，所以它内部的目标（例如文件列表中的文件夹）或它周围的目标，在指针所在的地方得到这个值：

```go
app.files.DragRows = func(rows []int) any {
	return app.pathsOf(rows) // []string
}
// In the row of a folder:
if paths, ok := ui.Drop[[]string](row); ok {
	app.move(paths, folder)
}
```

对不能拖动的行返回 nil。列表不能同时用 `Reorder` 和 `DragRows`。

见[拖放](drag-and-drop.md)。

## 焦点

列表把它的控件身份保存在 `ListState.Handle` 中。焦点查询在构建列表之前或之后都有效，焦点请求在列表隐藏时等待：

```go
app.list.Handle.OnShortcut(c, ui.Cmd, ui.KeyK, app.openCurrentFile)
ui.List(c.Key("files"), &app.list, len(app.files)).Grow(1).
    Rows(func(row ui.ListRow) {
        text := ui.Text(row.Context, app.files[row.Index].Name).Padding(6, 12)
        if row.Selected() && row.ListFocused() {
            text.TextColor(c.Theme().Accent)
        }
    })
```

在 UI 线程上调用 `app.list.Handle.Focus()` 请求焦点，或用 `Focus(c)` 显式选择一个窗口。`Focused(c)` 报告焦点在列表上，`FocusWithin(c)` 则把它的后代也算在内。只有该句柄对应的控件存在且启用时，句柄的快捷键动作才会在构建之后运行。

`ListRow` 提供当前的 Context、行索引、选择和列表焦点，以及行的悬停和按下（见[选择行](#选择行)）。`ListState.Selected` 或 `Selection` 会启用选择。`ItemKey` 给每个条目一个稳定的键。要在调用 `Rows` 之前配置它们。

对于使用 `List(c, state, n, func(i int))` 的自定义行构建器，`ListState` 的轮询辅助函数会在构建之后或在其行回调内部解析本次构建的列表。持久的焦点和命令处理使用 `Handle`。`Table` 通过它的 `ListState` 共享这个身份，`Outline` 则通过它的 `List` 状态共享。

持有键盘焦点的行滚出视图后仍保持构建，所以编辑可以继续，Tab 也能把下一个聚焦的行滚入视野。

## 辅助功能

辅助技术会看到一个列表，由它的 `Label` 命名，其中的列表项由它们内部的文本或状态的 `Label` 命名，每一项都说明自己在所有行中是第几行，“10,000 行中的第 5 行”，尽管列表只构建视口中的那些。选择行的列表会把焦点交给所选行，供辅助技术使用：上下方向键移动选择时会被读出，聚焦一行则会选中它。屏幕阅读器移出视图时会请求列表滚到那里，这会构建它到达的行。由单元格组成的行所构成的列表，比如电子表格，是带 `Role(ui.RoleGrid)` 的[网格](accessibility.md#网格)：它的行就是表格的行。

列中的行见 [Table](table.md)；树见 [Outline](outline.md)。
