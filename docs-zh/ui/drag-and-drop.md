# 拖放

<!-- preview drag-and-drop 230 -->

`Drag(value)` 让元素成为在窗口内拖动的值的起点：按住它的指针一旦移动了几个 DIP，元素的一个半透明副本就跟随指针，而这次按下不算单击。`ui.Drop[T](e)` 让 `e` 接受类型 `T` 的值，并返回落在它上面的那个；`ui.DragOver[T](e)` 返回位于它上面的那个，用来显示它将会接受：

```go
for _, task := range app.todo {
	ui.Row(c).Key(task.ID).Drag(task).Children(func() {
		ui.Text(c, task.Name)
	})
}

done := ui.Column(c).Grow(1).Padding(12).Radius(8).Border(1, t.Border)
if _, over := ui.DragOver[*Task](done); over {
	done.Border(2, t.Accent)
}
if task, ok := ui.Drop[*Task](done); ok {
	task.Done = true
}
```

- **目标。** 指针下最内层接受该值类型的元素会得到它，因此另一个接受其他类型的目标内部的目标不会挡住它。
- **内部的控件。** 在可拖动元素内部，按下只接受单击的控件（如按钮）后，一旦指针移动，就会拖动该元素，而这次按下不算单击：由按钮组成的列表行可以重新排序。没有移动的按下则会单击该控件。接受指针移动的控件（如文本输入和滑块）会保留这些移动。
- **取消。** Escape 放弃拖动。
- **滚动。** 拖动接近滚动容器的顶部或底部时，容器会滚动，越靠近边缘越快。
- **拖动多个。** 副本会在指针旁显示这次拖动包含多少个值，例如列表中选中的多行被一起拖动时。

`Dragging` 报告元素正在拖动它的值，便于在原处将它变暗。

`DragMissed` 在下一帧报告值被放开时没有落在任何接受它的元素上，以及放开的位置（窗口坐标）：放开在窗口边缘之外时，位置也在窗口之外，例如把一个标签页拖出，成为独立的窗口。用 Escape 放弃的拖动不算未命中。

```go
tab := ui.Row(c).Drag(t.id)
w, h := c.Size()
if at, ok := tab.DragMissed(); ok && (at.X < 0 || at.Y < 0 || at.X > w || at.Y > h) {
	app.tearOut(t, at)
}
```

## 重新排序

列表、表格和网格视图通过拖动，用 `ListState.Reorder` 和 `GridState.Reorder` 重新排序它们的行：若被拖动的行是选中的行之一，选中的行会一起移动，列表用一条线显示它们将移到哪里，松开指针后这些行就落在那里。

```go
app.list.Reorder = func(rows []int, to int) {
	app.items = move(app.items, rows, to) // before row to, len for the end
}
```

见[列表](list.md#重新排序)和[网格视图](grid-view.md)。

列表的行也可以拖出列表，如同把文件拖进文件夹：使用 `ListState.DragRows` 时，它们拖动的是应用的一个值，可由任意位置的 `ui.Drop` 接受。见[列表](list.md#把行拖出去)。

从其他应用拖来的文件是[拖放的文件](input.md#拖入的文件)。

## 在窗口和应用之间

`DragData` 用来自 `transfer` 包的显式表示形式启动系统拖动。文件、文本、URL、HTML、PNG 和自定义序列化格式都可以离开应用，或来自另一个应用。`DropData` 和 `DataDragOver` 声明元素接受的格式和操作：

```go
data := transfer.TextData("Hello from MyGo")
ui.Text(c, "Drag me").Padding(12).DragData(data)

zone := ui.Box(c).Size(260, 140).Border(1, c.Theme().Border)
options := transfer.DropOptions{Formats: []transfer.Format{transfer.Text}}
if _, over := ui.DataDragOver(zone, options); over {
	zone.Border(2, c.Theme().Accent)
}
if drop, ok := ui.DropData(zone, options); ok {
	text, err := drop.Data.Read(transfer.Text)
	if err == nil {
		app.message = string(text)
	}
}
```

最内层匹配且已启用的目标会在下一帧接收一次放下。至少有一个声明的格式必须匹配，且源和目标必须共享一种操作。悬停时只声明格式，不读取字节。被接受的表示会在放下时复制；得到的数据可以安全保留，并在原生会话结束后从 goroutine 读取。

### 表示和惰性提供者

一个条目可以为同一内容提供多种替代表示。自定义格式使用 MIME 名称和显式编码，如 JSON：

```go
const taskFormat transfer.Format = "application/vnd.example.task+json"
frozen := *task // snapshot the value that all representations describe
data := transfer.New(transfer.NewItem(
	transfer.Bytes(transfer.Text, []byte(frozen.Title)),
	transfer.Lazy(taskFormat, func() ([]byte, error) {
		return json.Marshal(frozen)
	}),
))
row.Drag(task).DragData(data)
```

`Bytes` 复制它的输入。`Lazy` 声明一种格式，并在每次拖动会话中，于接收方请求时缓存其结果或错误一次。提供者在主线程上运行，必须迅速返回。在提供之前，先在 goroutine 上准备好开销大的数据；提供者绝不能等待需要主线程的工作。外部应用可以在释放前请求数据。

使用 `DragDataFrom(func() transfer.Data { ... })` 在手势开始时对仍在变化的选择做快照。用 `transfer.URLData(url)` 和 `transfer.FileData(absolutePaths...)` 表示 URL 和文件列表。文件路径会转义为文件 URL；接收方的 `Data.Files()` 返回本地路径，而 `Data.URLs()` 返回 URI 列表条目。`FileData` 传输已存在的路径；它不会创建承诺的文件，也不会流式传输文件内容。

`Drag(value)` 单独使用时留在它的窗口内。与 `DragData` 结合时，它还会把 Go 值提供给同一进程其他窗口中已有的 `Drop[T]` 和 `DragOver[T]` 目标。这些带类型的目标接受复制操作。一个临时的随机会话令牌用于标识该值；原生表示中不含 Go 指针，源结束时令牌就不再解析。其他进程使用序列化的表示，并显式解码。

### 复制、移动、完成和取消

源和目标默认使用复制。要允许移动，双方都要设置 `Operations: transfer.Copy | transfer.Move`。两端都允许时，以修饰键的建议为准；否则优先复制，其次移动。不兼容的提议会被拒绝。Option 在 macOS 上请求复制，在其他平台请求 Ctrl；Cmd 在 macOS 上请求移动，在其他平台请求 Shift。

```go
row.DragData(data, transfer.DragOptions{
	Operations: transfer.Copy | transfer.Move,
	Done: func(result transfer.Result) {
		if result.Err != nil {
			app.report(result.Err)
		} else if result.Operation == transfer.Move {
			app.removeOriginal(task.ID)
		}
	},
})
```

`Done` 在主线程上恰好运行一次，无论取消、原生启动失败、源移除还是窗口销毁，都会运行。Escape 通过系统的拖动跟踪器取消。`Window.CancelDataDrag()` 可从任意 goroutine 取消该窗口拥有的源。`Window.StartDataDrag` 直接从当前的原生指针手势开始传输；UI 元素通常使用 `DragData`，以便应用移动阈值。

`Result.Operation` 是目标的最终效果。`Canceled` 表示没有发生放下；`Escaped` 区分用户主动放弃的拖动（Escape、`CancelDataDrag` 或窗口关闭）与在没有目标处松开的拖动，`Point` 是拖动结束的位置，使用 `Screen.CursorScreenPoint` 的屏幕坐标。例如，一个标签页在每个窗口之外被放开，就是 `Point` 处的 `Canceled && !Escaped`。`Err` 报告原生或提供者的失败。MyGo 从不删除文件。只有在移动成功后，才移除应用中的记录，并要考虑会自行移动文件的文件管理器。已有的 `DroppedFiles` 和 `OnFileDrop` 监听器继续以复制语义接受文件。

默认预览是源可见元素的快照，最大为 512 DIP。把 `DragOptions.Preview` 设为 `image.Image`，把 `Hotspot` 设为它的指针偏移，以使用自定义预览（每个维度最多 4096 像素）。图像在拖动开始时复制；一个图像像素代表一个 DIP。

### 原生格式映射

| 平台 | 原生传输 | 表示 |
|---|---|---|
| macOS | AppKit 拖动会话和粘贴板条目提供者 | 文本、HTML、PNG 和文件 URL 使用系统的 UTI。MIME 名称通过 `UTType` 映射，并带有可逆的标识符回退。条目边界会保留。 |
| Linux | GTK 3 拖动上下文和异步选择 | MIME 目标，文本用 UTF8_STRING，URL 和文件用 text/uri-list。被接受的格式在放下后请求。 |
| Windows | OLE IDataObject / IDropSource / IDropTarget | Unicode 文本、CF_HDROP、URL、CF_HTML、PNG 和注册的自定义名称；延迟渲染和 Shell 拖动图像。流保留精确的二进制长度；HGLOBAL 源可能包含填充。 |
| 浏览器 | 画布上放下的文件，放下时读取（见[浏览器](browsers.md)） | 只有文件，位于后端提供的、可由 `os` 读取的路径。其他拖动，以及向窗口外的拖动，都没有传输。 |

GTK 和 OLE 只有一个选择/数据对象：多个文本条目用换行连接，URI/文件列表用 CRLF 连接，其他格式则使用第一个提供该格式的条目。同一 MyGo 进程的窗口之间的传输通过核心的本地注册表保留所有条目。传入的 GTK/OLE 表示每个限制为 64 MiB；原生提供者必须提供内存或流表示（不支持虚拟文件承诺）。

见 [`examples/drag-drop`](../../examples/drag-drop)，两个窗口交换笔记、文件、文本和 URL，并演示惰性 JSON 表示和移动完成。不依赖原生 UI 也可以复用传输模型；现有剪贴板便捷方法的签名保持不变。`Clipboard.Write` 和 `Clipboard.Read` 交换同一个 `transfer.Data`；每次剪贴板写入和拖动都会获得独立的提供者缓存。关于剪贴板协商、持久化和提供者生命周期，见[剪贴板与拖动数据](../data-transfer.md)。
