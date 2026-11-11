# 浮层

<!-- preview popover 240 -->

`ui.Popover` 在 `*bool` 为 true 时，在元素（即它的锚点）下方显示一个由它的函数构建的面板；在它外面按下或按 Escape 会将它设为 false。这次按下会继续传给指针下的内容，如同 Web 的浮层那样：单击另一个按钮会关闭浮层并按下该按钮，单击锚点则会关闭它，因为锚点会切换它。锚点下方没有空间时，面板显示在它的上方，并随窗口滚动跟随锚点。

```go
more := ui.Button(c, "More ▾")
if more.Clicked() {
	app.menu = !app.menu
}
ui.Popover(c, more, &app.menu, func() {
	if ui.Button(c, "Rename").Clicked() {
		app.menu, app.renaming = false, true
	}
	if ui.Button(c, "Duplicate").Clicked() {
		app.menu = false
		app.duplicate()
	}
})
```

`Dismissed` 在外部按下或 Escape 关闭浮层的那一帧报告；必须保持打开的浮层，此时把 `*bool` 设回 true 即可。

浮层的元素随 Tab 移动而跟随锚点；带焦点关闭时，它会把焦点还给原先持有焦点的元素。见[浮层](overlays.md)。

若要用系统菜单，用[菜单按钮](menu-button.md)；若要用在窗口中绘制的元素菜单，并具备菜单的按键，用 [`PopoverMenu`](context-menu.md#在窗口中绘制)，或在按钮下方用 [`PopoverMenuButton`](menu-button.md#在窗口中绘制)；若要提示，用[工具提示](tooltip.md)。浮层显示在所属窗口中：必须延伸到窗口边缘之外的浮层，会显示在[浮出窗口](../windows.md#flyouts)中，即紧挨锚点放置的独立窗口。

## 无外观

`ui.PopoverBase` 是无外观的浮层：它的函数为面板设置样式并构建内容。上外边距让面板与锚点保持距离，无论显示在哪一侧：

```go
ui.PopoverBase(c, anchor, &app.open, func(panel ui.Element) {
	panel.Margin(6, 0, 0, 0).Padding(8).Radius(12).Background(t.Background).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.2))
	app.filters(c)
})
```

面板可以用 `AttachTo` 放到别处：放在锚点右侧，中线对齐；右侧没有空间时放到左侧：

```go
ui.PopoverBase(c, anchor, &app.open, func(panel ui.Element) {
	panel.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 6)
	app.details(c)
})
```

自定义浮层（如悬停卡片）在浮层中用 `AttachTo` 和 `PressedOutside` 构建：见[浮层](overlays.md#你自己的浮层)。

见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术把面板看作锚点之后的弹出层。
