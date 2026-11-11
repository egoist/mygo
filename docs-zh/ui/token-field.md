# 标记字段

<!-- preview token-field 260 -->

`ui.TokenField` 创建多项输入框，用于编辑 `*[]string`，例如标签或邮件收件人：每项显示为带移除按钮的小标签（chip），末尾是输入框。按 Enter 或输入逗号会添加当前输入的内容，输入框为空时按 Backspace 会移除最后一项。输入框下方会显示包含当前输入内容的建议，排除已添加的项；用 Up 和 Down 选择建议，按 Enter 或单击即可添加。`Changed` 报告变化。

```go
ui.TokenField(c, &app.tags, allTags).Label("Tags")
```

小标签排满一行后会自动换行。`Placeholder` 在输入框为空时显示提示文本，例如“Add a tag…”；未设置时则不显示。

## 辅助功能

辅助技术将整个控件识别为由 `Label` 命名的输入区域：有建议时，内部输入框是提供列表补全的组合框（`aria-autocomplete="list"`），否则是普通文本框。每个小标签的移除按钮也可单独访问，名称由“Remove”和该项文本组成。
