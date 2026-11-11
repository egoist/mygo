# 步进器

<!-- preview stepper 100 -->

`ui.Stepper` 创建一对箭头，用于在两个边界之间按步长改变 `*float64`，如同字段旁的 AppKit 步进器：单击箭头会向上或向下步进，步进器持有键盘焦点时 Up 和 Down 也一样，按住箭头会持续步进，按得越久速度越快。`Changed` 报告新值。

```go
ui.Row(c).Gap(6).Children(func() {
	ui.Textf(c, "%.0f copies", app.copies)
	ui.Stepper(c, &app.copies, 1, 99, 1).Label("Copies")
})
```

若用户还会键入数字，用[数字输入](number-input.md)，它自带步进箭头。

## 辅助功能

辅助技术看到一个表示该值的微调按钮，由 `Label` 命名，可递增和递减。
