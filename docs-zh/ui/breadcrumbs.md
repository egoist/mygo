# 面包屑导航

<!-- preview breadcrumbs 180 -->

`ui.Breadcrumbs` 创建一条条目路径，如同 Finder 的路径栏和 AppKit 的路径控件，条目之间用尖角符号分隔：除最后一项外，其余条目都是链接，单击即可选中它，按 Enter 也一样，并把 `*int` 设为它的索引，`Changed` 会报告这一变化。空间不足时，长条目以省略号缩短。

```go
path := []string{"Macintosh HD", "Users", "ada", "Documents"}
if ui.Breadcrumbs(c, path, &app.chosen).Label("Path").Changed() {
	app.open(path[:app.chosen+1])
}
```

最后一项是用户所在的位置，它不是链接。

## 辅助功能

辅助技术看到的是一个导航地标中的链接，`Label` 为这个地标命名，屏幕阅读器可以在窗口的各地标之间跳转到它；最后一项则被看作当前页（`ui.CurrentPage`），屏幕阅读器会这样朗读它。你自己的路径用 `Role(ui.RoleNavigation)` 和 [`Current`](accessibility.md#状态) 说明同样的事：

```go
ui.Row(c).Gap(6).Role(ui.RoleNavigation).Label("Breadcrumb").Children(func() {
	ui.Link(c, "Home", "/")
	ui.Text(c, "/").AccessHidden(true)
	ui.Text(c, "Settings").Current(ui.CurrentPage)
})
```
