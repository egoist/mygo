# 可折叠

<!-- preview collapsible 220 -->

`ui.Collapsible` 创建一个展开控件，如同 SwiftUI 的 `DisclosureGroup`：标签旁有一个箭头，单击即可打开或关闭，按 Enter 和 Space 也一样，打开时在下方显示它的函数在 `*bool` 为 true 时构建的内容。打开时箭头旋转，内容展开进入视野。`Changed` 报告单击。

```go
ui.Collapsible(c, "Advanced", &app.advanced, func() {
	ui.Checkbox(c, &app.verbose, "Verbose logging")
	ui.Checkbox(c, &app.experimental, "Experimental features")
})
```

系统设置了减少动效时，内容会立即显示。

若要在盒子里放多个节，用[手风琴](accordion.md)。

## 无外观

`ui.CollapsibleBase` 是无外观的可折叠控件：`Trigger` 显示和隐藏 `Panel` 构建的内容，并用 `Progress` 让它展开和收起，后者从 0（关闭）到 1（打开），供你实现自己的外观和动效：

```go
p := ui.CollapsibleBase(c, &app.open)
p.Trigger.Gap(6).Children(func() {
	ui.Icon(c, chevron).Rotate(90 * p.Progress())
	ui.Text(c, "Details")
})
p.Panel(func() {
	ui.Text(c, app.details)
})
```

见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一个由标签命名的展开控件，处于展开或折叠状态，它像单击一样打开和关闭它。
