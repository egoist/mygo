# 自动补全

<!-- preview autocomplete 260 -->

`ui.Autocomplete` 创建一个编辑 `*string` 的文本输入框，在它下方从字符串列表中推荐包含所输入内容的字符串，以输入内容开头的排在前面：Up 和 Down 在建议之间移动，Enter 取用一项，单击也可以；没有选中任何建议时按 Enter，则为 `Submitted`。`Changed` 报告变化，无论是键入引起的还是取用建议引起的。

```go
ui.Autocomplete(c, &app.city, cities).Label("City")
```

与[组合框](combobox.md)不同——后者的值是它的某个选项——自动补全保留用户输入的内容。

## 辅助功能

辅助技术看到的是一个由 `Label` 命名的组合框，可以像文本框一样编辑，其建议补全所输入的内容（`aria-autocomplete="list"`）；对它而言，方向键停留的那条建议持有焦点。
