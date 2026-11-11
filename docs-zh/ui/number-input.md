# 数字输入

<!-- preview number-input 160 -->

`ui.NumberInput` 创建文本框，把 `*float64` 作为两个边界之间的数字来编辑，Up 和 Down 以及它旁边的按钮按步长改变它。输入的内容一旦成为范围内的数字就立即生效，失去焦点后，按步长的小数位数舍入显示。`Changed` 报告新值。`Width` 设置整个输入的尺寸，包括按钮：文本框会收窄，使按钮仍留在其中。

```go
ui.NumberInput(c, &app.copies, 1, 99, 1).Label("Copies")
ui.NumberInput(c, &app.opacity, 0, 1, 0.05).Label("Opacity")
```

若用户只会按步长调整值，用[步进器](stepper.md)；若要粗略选择的值，用[滑块](slider.md)；若是金额，`float64` 无法精确表示，用[小数输入](decimal-input.md)。

## 辅助功能

辅助技术看到一个表示该值的微调按钮，由 `Label` 或它所在的[字段](form.md)命名，可按步长增减它。
