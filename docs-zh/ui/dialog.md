# 对话框

<!-- preview dialog 260 -->

`ui.Modal` 在 `*bool` 为 true 时，在变暗的窗口上方显示一个由它的函数构建的对话框；单击它外面或按 Escape 会把它设为 false。

```go
if ui.Button(c, "Rename…").Clicked() {
	app.renaming = true
}
ui.Modal(c, &app.renaming, func() {
	ui.Text(c, "Rename").Bold()
	if ui.TextInput(c, &app.name).AutoFocus().Submitted() {
		app.renaming = false
	}
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, "Cancel").Clicked() {
			app.renaming = false
		}
		if ui.PrimaryButton(c, "Rename").Clicked() {
			app.rename()
			app.renaming = false
		}
	})
})
```

## 键盘

对话框会独占键盘：打开时取得焦点，焦点落在其中第一个可取得焦点的元素上，除非其中有一个请求了焦点（`AutoFocus`）；Tab 在它的元素之间循环，构建在它之外的窗口快捷键会等待，因为它背后的内容是惰性的。关闭时，它把焦点还给原本持有焦点的元素，例如打开它的按钮。见[浮层](overlays.md)。

## 让它保持打开

`Dismissed` 在 Escape 或单击外部关闭对话框的那一帧报告；必须保持打开的对话框（例如有更改要保存）此时把 `*bool` 设回 true：

```go
if ui.Modal(c, &app.editing, app.editor).Dismissed() && app.unsaved {
	app.editing = true
}
```

要询问重要的事，需要按钮且单击外部不会关闭，用[警告对话框](alert-dialog.md)。

## 窗口之外

对话框绘制在它所属的窗口中，并被裁剪到窗口边缘：由浮出窗口请求的对话框（例如托盘的浮出窗口）不会比该浮出窗口更宽。改为打开一个对该浮出窗口模态的窗口，它显示在浮出窗口之上并随之关闭（见[叠在浮出窗口上的对话框](../windows.md#a-dialog-over-a-flyout)）。

## 无外观

`ui.DialogBase` 是无外观的对话框：由它的函数为覆盖窗口的背板（背板使面板居中）和面板设置样式，并构建面板的内容：

```go
ui.DialogBase(c, &app.open, func(backdrop, panel ui.Element) {
	backdrop.Background(ui.RGBA(0, 0, 0, 0.5))
	panel.Width(360).Padding(24).Radius(16).Background(t.Background)
	ui.Text(c, "Welcome").FontSize(20).Bold()
})
```

见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一个对话框，并在它显示时看不到它背后的任何内容。
