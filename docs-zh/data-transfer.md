# 剪贴板与拖放数据

包 `transfer` 描述系统剪贴板与原生[拖放](ui/drag-and-drop.md)共享的序列化内容。一个 `Data` 值包含有序的项。每一项为**同一**内容提供多种备选格式；不同的项描述不同的值。自定义格式使用 MIME 名称和显式编码。原生数据从不包含 Go 指针或进程本地的 Go 值。

```go
const noteFormat transfer.Format = "application/vnd.example.note+json"
frozen := *note
data := transfer.New(transfer.NewItem(
    transfer.Bytes(transfer.Text, []byte(frozen.Title)),
    transfer.Bytes(transfer.HTML, []byte("<b>"+html.EscapeString(frozen.Title)+"</b>")),
    transfer.Lazy(noteFormat, func() ([]byte, error) {
        return json.Marshal(frozen)
    }),
))

err := mygo.Clipboard.Write(data, mygo.ClipboardOptions{
    OnRelease: func() { /* release resources captured by these providers */ },
})
row.DragData(data) // the same model, with an independent provider cache
```

`ClipboardOptions.Concealed` 把一次写入标记为机密（例如密码），供读取平台标记的剪贴板管理器使用；它们如何处理这个标记，比如把它排除在历史记录之外或按密码显示，由它们自己决定。在 macOS 上，每个项都会带上 nspasteboard.org 的 `org.nspasteboard.ConcealedType`，并且只写入当前主机，使内容不会进入通用剪贴板；Windows 上会设置 `ExcludeClipboardContentFromMonitorProcessing`，让内容不进入剪贴板历史，也不会出现在用户的其他设备上；Linux 提供 KDE 的 `x-kde-passwordManagerHint` 目标，值为 `secret`，Klipper、CopyQ 和 GPaste 会读取它。浏览器没有这样的标记。`Flush` 会保留这些标记。内容会一直留在剪贴板上，直到应用替换或清除它：过一段时间后清除密码，是应用自己的策略。

`Bytes` 会复制传入的内容。`Data.Items` 和格式列表返回独立的切片，读取返回独立的字节。空字节是有效的。`Text`、`HTML`、`PNG`、`URIList` 和 `FileList` 是常见表示形式的名称。`TextData`、`URLData` 和 `FileData` 用于构造常见的提供内容。文件是编码为转义文件 URL 的绝对路径；`Data.Files` 只接受本地路径，忽略远程主机部分和非文件 URL。不会读取文件，也不会删除文件。每个路径使用一个项（`FileData` 会自动这样做）。

## 剪贴板发现与读取

`Clipboard.Formats` 返回可移植的名称，而不请求表示形式。`transfer.PreferredFormat` 和 `Offer.Preferred` 按接收方的偏好顺序协商。较旧的 `AvailableFormats` 保留各平台的惯例：macOS 上是原生 UTI，Linux 上是 GTK 目标，Windows 上是规范名称（现在也包括自定义格式和文件列表）。

只请求接收方可以使用的格式：

```go
formats := mygo.Clipboard.Formats()
if format, ok := transfer.PreferredFormat(formats, noteFormat, transfer.HTML, transfer.Text); ok {
    bytes, err := mygo.Clipboard.ReadFormat(format)
    // Decode the explicitly agreed encoding; handle err.
}

// Read accepted alternatives together; other providers are not requested.
data, err := mygo.Clipboard.Read(noteFormat, transfer.HTML, transfer.Text)
if err == nil {
    if format, ok := data.Preferred(noteFormat, transfer.HTML, transfer.Text); ok {
        bytes, err := data.Read(format)
        // Use the receiver's preferred representation.
    }
}
```

不带参数的 `Read()` 复制所有公布的表示形式。空剪贴板返回空数据。没有匹配格式的显式请求返回 `transfer.ErrFormat`。`ReadFormat` 像 `Data.Read` 那样合并项：文本用换行连接，URI/文件列表用 CRLF 连接，其他格式使用第一个匹配的项。使用 `Data.Items()` 读取单独的项。

读取返回的是已经生成好的数据，不再依赖原生提供方。在所有者改变、窗口关闭或源应用退出之后，它仍可在任何 goroutine 中使用，也可以在之后的拖放中提供。原生操作在主线程上运行。观察到所有权改变的读取返回 `mygo.ErrClipboardChanged`；Windows 在读取其他应用的内容期间持有剪贴板锁。从剪贴板提供方访问剪贴板返回 `mygo.ErrClipboardReentrant`，防止递归读取和替换。

现有的 `ReadText`、`WriteText`、`ReadHTML`、`WriteHTML`、`ReadImage`、`WriteImage` 和 `Clear` 保持原有签名。`WriteHTML` 在所有平台上都提供 HTML 和文本回退，修复了 Linux 以前只能处理文本的行为。`WriteImage` 在替换现有数据之前校验 PNG；原生图像读取器仍然接受 TIFF、GTK 图像目标和 Windows DIB。`WriteFiles` 和 `ReadFiles` 交换原生文件列表。`Write`/`WriteFiles` 传入空内容时会清除剪贴板。写入失败时不会安装它的释放钩子。

## 惰性提供方与所有权

每次写入和每次拖放都会获取一个新的 `Data.Snapshot`。惰性提供方被请求时，对那个快照最多运行一次；成功、错误和已恢复的 panic（`transfer.ErrProviderPanic`）都会被缓存。发现和偏好协商不会请求字节。原生接收方（包括剪贴板管理器）可能在发布后立刻请求字节。`Read` 只请求它选中的格式。提供方出错会导致该次读取被拒绝，其他表示形式仍然可用。

提供方在主线程上执行，必须迅速返回。提前在 goroutine 上准备代价高昂的内容，并冻结替代表示形式共享的值。绝不要等待另一个主线程操作、递归读取同一个提供方，或从剪贴板提供方访问剪贴板。独立的 `Data.Read` 在调用它的 goroutine 上执行其提供方。

剪贴板所有权属于**应用**，与任何源窗口无关。关闭窗口后，它复制的内容和提供方仍然可用。让提供方资源保持存活，直到 `ClipboardOptions.OnRelease` 运行；它在原生回调以及任何外层剪贴板操作退出之后（包括运行嵌套事件循环的存储操作）在主线程上运行一次。替换、`Clear`、成功的 `Flush` 和应用关闭都会释放提供方。macOS 也可能在履行完所有承诺后结束它们。该钩子可以在应用运行时写入新的剪贴板内容；应用关闭后的剪贴板调用会返回事件循环已停止的错误，或返回便捷方法的空结果。它不能依赖窗口仍然存在。拖放源则使用其会话的 `Done` 回调。

`Clipboard.Flush` 先把拥有的每一个表示形式解析出来，再交给原生存储。提供方失败时会保持所有权不变，并把这些失败返回；如果持久化很关键，请在退出前处理它们。应用退出时会尽力自动调用 `Flush`，然后释放剩余的提供方资源。在另一个应用拥有剪贴板时执行 `Flush`，不会动它的内容。在 Linux 上，退出后要持久化，需要有剪贴板管理器。没有的话，`Flush` 会返回 `mygo.ErrClipboardPersistence`，释放惰性资源，并在应用运行期间继续提供已经生成好的字节。

## 原生互操作

| 平台 | 剪贴板实现 | 项边界与格式 |
|---|---|---|
| macOS | NSPasteboard 项和应用拥有的数据提供方；执行 `Flush` 时提供已生成的项 | 项边界在原生读取和持久化后仍然保留。文本/HTML/PNG/文件 URL 使用系统 UTI；自定义 MIME 名称共享拖放的 UTType 映射。完整的 URI/文件列表有序列化类型，同时有单 URL 回退。UTF-16 文本和 TIFF 图像在读取时转换。 |
| Linux | GtkClipboard 选择目标、共享的拖放格式映射、get/clear 回调、所有者变更跟踪；GTK 剪贴板管理器存储 | 在拥有它的 MyGo 进程内，所有项都保留。原生选择会合并文本和 URI/文件列表；自定义格式使用第一个匹配的项。HTML 以 `text/html` 提供。持久化取决于桌面的剪贴板管理器。 |
| Windows | 与拖放所用相同的延迟渲染 IDataObject，配合 OleSetClipboard/OleFlushClipboard；加锁的 Win32 读取 | 在拥有它的进程内，执行 `Flush` 之前所有项都保留。原生读取会合并文本/URI/文件列表，并使用第一个自定义项。Unicode 文本、CF_HTML、PNG/DIB、CF_HDROP、URL 和注册的 MIME 格式可与系统应用互操作。可选的私有长度提示在 HGLOBAL 持久化后保留 MyGo 的空/二进制载荷；实际的原生格式仍是未包装的字节。 |

来自其他应用的剪贴板表示形式每个限制为 64 MiB。没有字节长度约定的其他 Windows HGLOBAL 自定义数据可能包含分配器填充；与这类源互操作时，请使用自带长度的编码。虚拟文件和文件内容承诺不在当前模型的范围内。`Drag(value)` 使用的 Go 值是进程本地的；它们的临时拖放注册表令牌会被剪贴板写入拒绝，也会从剪贴板发现中省略。

[剪贴板示例](../examples/clipboard)把一条笔记作为文本、HTML 和惰性 JSON 复制并拖放，显示提供方调用/释放的计数，粘贴文件列表，并执行 `Flush` 写出剪贴板数据。
