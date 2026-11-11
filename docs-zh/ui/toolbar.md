# 工具栏

<!-- preview toolbar 190 -->

`ui.Toolbar` 将控件横向排成工具栏，通常放在窗口顶部，可包含按钮、[切换按钮](toggle.md)、切换按钮组、[分段控件](segmented.md)、[拆分按钮](split-button.md)和[菜单按钮](menu-button.md)；这些控件在指针悬停前不显示独立的按钮外观。`ui.Spacer` 可将后面的控件推到工具栏末端。

```go
ui.Toolbar(c, func() {
	ui.BackButton(c, app.router)
	ui.ForwardButton(c, app.router)
	ui.ToggleGroup(c, func() {
		ui.Toggle(c, &app.bold, "Bold")
		ui.Toggle(c, &app.italic, "Italic")
	})
	ui.Spacer(c)
	if ui.Button(c, "Share").Clicked() {
		app.share()
	}
}).Label("Format")
```

## 键盘

工具栏是一个 Tab 焦点停留点：Left 和 Right 在控件之间移动焦点，Home 和 End 跳到第一个和最后一个，Tab 则继续到它之后。

## 溢出

放不下的控件会从最后一个开始进入工具栏末尾的菜单，在菜单中选中一个会单击该控件、切换一个切换按钮，或打开菜单按钮的菜单。

## 辅助功能

辅助技术看到一个工具栏，由 `Label` 命名，其中包含它的控件。
