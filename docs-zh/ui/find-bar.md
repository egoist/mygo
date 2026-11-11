# 查找栏

<!-- preview find-bar 200 -->

`ui.FindBar` 在 `*bool` 为 true 时创建一条用于查找文本的栏，如同 Safari 和 Xcode 的查找栏：一个查询文本框（栏打开时取得焦点，并选中上次的查询）、匹配项数量、选择当前匹配项的上一个和下一个按钮，以及 Done。

```go
if c.Shortcut(ui.Cmd, ui.KeyF) {
	app.finding = true
}
matches := app.find(app.query) // the app counts them
ui.FindBar(c, &app.finding, &app.query, len(matches), &app.match)
if app.finding && len(matches) > 0 {
	app.show(matches[app.match])
}
```

应用负责查找：它统计查询的匹配项并显示当前匹配项，`Changed` 报告当前匹配项的移动。

## 键盘

文本框中的 Enter 跳到下一个匹配项，Shift+Enter 跳到上一个；macOS 上的 Cmd+G 和 Shift+Cmd+G、其他平台上的 F3 和 Shift+F3 也一样，到达两端后回绕；Escape 和 Done 关闭栏。

关闭时，它返回一个不显示任何内容的元素。

## 辅助功能

辅助技术看到一个名为 "Find" 的工具栏，其中包含一个搜索文本框，并在匹配项数量变化时听到计数。它的名称、计数（"3 of 12"、"No matches"）和 Done 都使用窗口的语言（[MyGo 自己的字符串](../i18n.md#mygos-own-strings)）。
