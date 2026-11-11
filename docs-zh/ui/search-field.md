# 搜索框

<!-- preview search-field 200 -->

`ui.SearchField` 创建一个用于搜索的文本框，编辑 `*string`：一个放大镜、文本，以及在有内容时清空文本的按钮；此时按 Escape 也会清空。为空时显示“Search”，或显示 `Placeholder` 设置的文字，用来说明搜索范围。`Changed` 报告变化，`Submitted` 报告 Enter。

```go
if ui.SearchField(c, &app.query).Label("Search mail").Changed() {
	app.filter()
}

ui.SearchField(c, &app.pageQuery).Placeholder("Search pages, or #tag to filter")
```

若要在用户输入时搜索，就在 `Changed` 时筛选；若搜索较耗时，则在 `Submitted` 时搜索，或用 `c.After` 在停顿之后搜索。

## 辅助功能

辅助技术看到一个搜索框（macOS 上的 `AXSearchField`，Linux 上带 `xml-roles:searchbox` 的 entry，Windows 上的 Edit “search box”），由 `Label` 命名。你自己绘制、带自身图标和清空按钮的文本输入，可以用 `Search` 声明为搜索框，如同 ARIA 的 `searchbox`：

```go
ui.TextInput(c, &app.query).Placeholder("Search").Label("Search mail").Search()
```
把它连同执行搜索的按钮（如果有）放进一个 `Role(ui.RoleSearch)` 的元素，即一个搜索地标，屏幕阅读器会列出它并跳转到它（[辅助功能](accessibility.md#地标实时区域和分隔线)）。
