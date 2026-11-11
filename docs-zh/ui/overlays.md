# 浮层

浮层显示在窗口内容上方：[对话框](dialog.md)在变暗的窗口之上，[警告对话框](alert-dialog.md)，[浮层](popover.md)在元素下方，[选择](select.md)和[组合框](combobox.md)的弹出内容，在窗口中绘制的菜单（[`PopoverMenu`](context-menu.md#在窗口中绘制) 和 [`PopoverMenuButton`](menu-button.md#在窗口中绘制)，以及[绘制其菜单](context-menu.md#窗口的每个菜单)的窗口的每个菜单），[工具提示](tooltip.md)和 [toast](toast.md)。它们对键盘和背后内容的处理方式相同。尽管显示在窗口之上，它们仍采用请求它们处的主题：在 [`Themed`](styling.md#窗口一部分的主题) 内打开时采用它的主题，如同在那里构建的 `ui.Overlay` 元素；toast 则属于窗口。

```go
more := ui.Button(c, "More ▾")
if more.Clicked() {
	app.menu = !app.menu
}
ui.Popover(c, more, &app.menu, func() {
	if ui.Button(c, "Rename").Clicked() {
		app.menu, app.renaming = false, true
	}
})
ui.Modal(c, &app.renaming, func() {
	ui.Text(c, "Rename").Bold()
	if ui.TextInput(c, &app.name).AutoFocus().Submitted() {
		app.renaming = false
	}
})
```

## 键盘

对话框独占键盘：它在打开时取得焦点，落在其中第一个可取得焦点的元素上，除非其中有元素请求了焦点（`AutoFocus`）；Tab 在它的元素之间循环，构建在它之外的窗口快捷键会等待，因为它背后的内容是惰性的，屏幕阅读器也看不到它。浮层的元素随 Tab 移动而跟随其锚点。

Escape 关闭最上面的浮层，选择器的弹出内容先于它所在的对话框关闭，除非持有焦点的元素接管它，如同终端那样；带焦点关闭的浮层会把焦点还给打开它时持有焦点的元素，例如它的按钮。

在浮层外按下会关闭它，并继续传给指针下的内容，如同 Web 的浮层：单击另一个按钮会关闭浮层并按下该按钮。选择器的弹出内容会接收这次按下，如同系统的弹出菜单；对话框的背板会关闭它。

## 你自己的浮层

`ui.Overlay` 在所有其他内容之上构建元素，用 `Absolute` 按窗口的 DIP 放置：

```go
ui.Overlay(c, func() {
	ui.Text(c, "Offline").Absolute().Top(12).Right(12).
		Padding(4, 10).Radius(999).Background(t.Danger).TextColor(t.AccentText)
})
```

`PopoverBase` 和 `DialogBase` 是无外观的浮层和对话框，用于你自己设计的浮层：见[自定义控件](custom-widgets.md)。两者都不适用的浮层，例如抽屉、悬停卡片或菜单栏的菜单，用构成它们的基础部件构建：

- `AttachTo(target, at, self)` 把一个元素放在另一个元素旁边，无论目标在哪里：把它的点 `self` 放在目标的点 `at` 上，例如 `AttachTo(button, ui.AnchorBottomLeft, ui.AnchorTopLeft)` 放在按钮下方。如果会超出窗口，它就翻到另一侧，或沿目标的另一个方向放置，然后移回窗口内；纵向滚动的元素不会高于目标较大一侧的空间，并会滚动。它的外边距使它和目标保持距离。它是目标的浮层：它的元素随 Tab 移动而跟随目标。
- `AttachEdgeMargin(v)` 让这样的元素与窗口边缘保持 `v` DIP 的距离，未设置时保持 4：`AttachEdgeMargin(12)` 用于不能碰到窄窗口边缘的宽面板，如同 CSS Anchor Positioning 中包含块的 inset。
- `AttachedAnchors` 返回它最终所在的锚点，如同 Floating UI 的 placement：对于请求放在按钮下方却去了它上方的面板，是 `ui.AnchorTop, ui.AnchorBottom`。视图读取上一帧的锚点，`Draw` 读取当前帧的；在面板首次显示的那一帧，布局之前构建的视图读取所请求的锚点。它第一帧显示的内容取决于布局实际翻到了哪一侧：`AttachedOrigin` 把它的变换原点放在它自己的锚点上，位于它实际落到的任一侧，使它从最靠近目标的边缘缩放进入；`Enter` 或 `Exit` [动效](transitions.md#出现与离开)的 `Flip` 会镜像它的偏移，因此一个在按钮下方下滑的菜单会在按钮上方上滑出现：

  ```go
  tip := ui.Column(c).AttachTo(b, ui.AnchorBottom, ui.AnchorTop).Margin(6, 0, 0, 0).
  	AttachedOrigin().Transition(ui.ElementTransition{
  		Duration: 150 * time.Millisecond, ExitDuration: 100 * time.Millisecond,
  		Enter: &ui.Motion{Scale: 0.96, Y: -4, Flip: true},
  		Exit:  &ui.Motion{Scale: 0.96, Y: -4, Flip: true},
  	})
  _, self := tip.AttachedAnchors()
  tip.AlignItems(ui.Center).Children(func() {
  	if self == ui.AnchorTop { // below the button, as asked
  		ui.Text(c, "▲")
  	}
  	ui.Text(c, "Copied")
  	if self == ui.AnchorBottom { // above it, without room below
  		ui.Text(c, "▼")
  	}
  })
  ```
- `PressedOutside` 报告在元素、它的目标以及其中内容所开的浮层之外发生的按下，浮层据此关闭；`PressedInside` 报告它们之内的一次按下，而不接收指针。元素显示之前的按下不算在它的外部：右键单击打开的浮层会保持打开，因为这次按下在同一帧打开了它。
- `OverlayShortcut` 接收持有焦点的元素及其周围元素未处理的按键，无论焦点在哪里，最后构建的浮层优先，如同 Escape 关闭最上面的那个。
- `Modal` 把元素变成对话框的背板：焦点移入并留在其中，构建在它之外的窗口快捷键会等待，屏幕阅读器只看到它及其上方的内容。

在 `ui.Overlay` 顶层构建的每个元素在带焦点消失时，会把焦点还给在它出现时持有焦点的元素。右侧的一个抽屉，可以用 Escape 或单击它的背板关闭：

```go
if app.drawer {
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Justify(ui.End).
			Background(ui.RGBA(0, 0, 0, 0.3)).Modal()
		back.Children(func() {
			panel := ui.Column(c).Width(320).FillHeight().Padding(16).Background(t.Background).
				Role(ui.RoleDialog).Label("Filters").Children(app.filters)
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				app.drawer = false
			}
		})
	})
}
```

原生[对话框](../native.md#dialogs)也可以使用：从 goroutine 调用它们，以便视图不必等待它们。
