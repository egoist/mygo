# 分段控件

<!-- preview segmented 200 -->

`ui.Segmented` 创建一个显示标签的分段控件，`*int` 是选中分段的索引，如同视图之间的切换：单击选择一个分段，持有键盘焦点时方向键也可以，`Changed` 报告新选择。

```go
ui.Segmented(c, &app.view, "List", "Grid").Label("View")
switch app.view {
case 0:
	app.list(c)
case 1:
	app.grid(c)
}
```

它是一个 Tab 焦点停留点，Tab 会跳到选中的分段。

## 全宽

每个分段都和它的标签一样宽。`FillEqually` 让控件与父元素一样宽，并在分段之间平分宽度，如同视图切换器或横跨面板的设置项那样：

```go
ui.Segmented(c, &app.appearance, "Light", "Dark", "System").FillEqually().Label("Appearance")
```

选中的分段占据自己的一份，标签居中，比份额更长的标签以省略号结尾。分段会保持外观、键、辅助技术读取的内容以及禁用状态。在 `FillEqually` 之后设置的宽度（如 `Width(320)`）是控件的宽度，分段仍会平分它。[切换按钮组](toggle.md)也支持它。

## 图标和其他内容

若要使用图标或自定义外观的分段，用 `ui.SegmentedBase` 构建它们，即一个由 `Segment` 组成的无外观 `Track`：

```go
seg := ui.SegmentedBase(c, &app.view, 2)
seg.Track.Padding(3).Radius(8).Background(t.Surface).Children(func() {
	for i, icon := range []*ui.SVG{listIcon, gridIcon} {
		s := seg.Segment(i).Padding(4, 10).Radius(6).Label([]string{"List", "Grid"}[i])
		if i == app.view {
			s.Background(t.Background)
		}
		s.Children(func() { ui.Icon(c, icon) })
	}
})
```

在构建分段之后，对 `Track` 调用 `FillEqually`，即可在它们之间平分宽度。见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一个由 `Label` 命名的单选按钮组，其中的单选按钮由各自的标签命名。
