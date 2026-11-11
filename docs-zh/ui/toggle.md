# 切换按钮

<!-- preview toggle 150 -->

`ui.Toggle` 创建一个在 `*bool` 为 true 时保持按下状态的按钮，如同编辑器工具栏中的 Bold：单击会按下或松开它，持有键盘焦点时按 Space 也一样，`Changed` 报告变化。

```go
ui.Toggle(c, &app.inspector, "Inspector")
```

用 `Children` 和空标签为它提供其他内容，例如图标，并用 `Label` 命名：

```go
ui.Toggle(c, &app.bold, "").Label("Bold").Children(func() { ui.Icon(c, boldIcon) })
```

## 切换按钮组

`ui.ToggleGroup` 把其中的切换按钮和按钮绘制成连在一起，如同一个控件的各个分段，例如 Bold、Italic 和 Underline。它们是一个 Tab 焦点停留点，方向键在它们之间移动焦点，如同在[工具栏](toolbar.md)中：

```go
ui.ToggleGroup(c, func() {
	ui.Toggle(c, &app.bold, "B")
	ui.Toggle(c, &app.italic, "I")
	ui.Toggle(c, &app.underline, "U")
}).Label("Style")
```

`FillEqually` 让组与父元素一样宽，其中的切换按钮平分宽度，如同它对[分段控件](segmented.md#全宽)所做的那样。

若要在几个选项中选一个，例如视图模式，使用[分段控件](segmented.md)。

## 没有外观

`ui.ToggleBase` 是无外观的切换按钮：单击时切换其 `*bool` 的行：见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一个切换按钮，开启时处于按下状态，并把切换按钮组看作一个由 `Label` 命名的组。
