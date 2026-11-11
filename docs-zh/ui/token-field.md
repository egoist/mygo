# 标记字段

<!-- preview token-field 260 -->

`ui.TokenField` 创建一个标记字段，例如标签或邮件收件人，编辑一个 `*[]string`：标记显示为小标签（chip），每个都带一个移除按钮，排在一个输入框前面，在输入框中按 Enter 或输入逗号会添加已键入的内容，输入框为空时按 Backspace 会移除最后一项。包含已键入内容的建议显示在它下方，已经作为标记的不再显示：Up 和 Down 在建议之间移动，Enter 或单击会添加一个。`Changed` 报告变化。

```go
ui.TokenField(c, &app.tags, allTags).Label("Tags")
```

小标签填满字段时会换到更多行。`Placeholder` 在输入框为空时显示一段文本，例如“Add a tag…”；未设置时则不显示。

## 辅助功能

辅助技术看到由 `Label` 命名的字段，它的输入框是一个组合框，其建议补全已键入的内容（`aria-autocomplete="list"`），没有建议时则是一个文本框，以及每个小标签上的按钮，名称由“Remove”和它的标记组成。
