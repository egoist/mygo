# 标签页

<!-- preview tabs 160 -->

`ui.Tabs` 创建一行显示标题的标签页，`*int` 保存所选标签页的索引。单击可选择标签页；标签页获得键盘焦点后，也可用方向键、Home 和 End 选择，焦点随选择移动。`Changed` 报告选择变化。在标签栏下方构建所选页面：

```go
ui.Tabs(c, &app.tab, "General", "Appearance", "Advanced")
switch app.tab {
case 0:
	app.general(c)
case 1:
	app.appearance(c)
case 2:
	app.advanced(c)
}
```

整行标签页只占一个 Tab 焦点停留点，按 Tab 会聚焦当前选中的标签页。如果页面需要历史记录并支持后退，用[路由器](navigation.md)；如果要在工具栏中切换视图，用[分段控件](segmented.md)。

## 无外观

`ui.TabsBase` 是无外观的标签页列表：一个容纳标签页的 `List`，其中的 `Tab` 选择索引，方向键移动选择和焦点。带 `Disabled(true)` 的标签页无法单击，方向键、Home 和 End 会跳过它：

```go
tabs := ui.TabsBase(c, &app.tab, len(names))
tabs.List.Gap(4).Children(func() {
	for i, name := range names {
		tab := tabs.Tab(i).Padding(6, 12).Radius(6).Disabled(!app.enabled[i])
		if i == app.tab {
			tab.Background(t.Surface)
		}
		tab.Children(func() { ui.Text(c, name) })
	}
})
```

### 关闭和重新排序

文档类的标签页可以关闭，也可以拖动换位置。`Reorder` 让用户把标签页拖到列表中的其他位置，拖动时列表会显示它将落在哪里，由你把它移过去。被拖动的标签页在放下时被选中，和系统标签栏按下即选中一致。`OnClose` 用于关闭标签页：有焦点的标签页按 Delete 或 Backspace 会关闭，点击它的 `CloseButton` 也会关闭。`CloseButton` 是无外观的按钮，不占 Tab 焦点停留点，点击它也不会选中该标签页。关闭的是当前选中的标签页时，选中它后面的那个；它是最后一个时，选中前面的那个。无论哪种情况，索引都跟着选中的标签页走，换成另一个标签页时 `Changed` 会报告。两者都要在构建标签页之前调用，并给标签页设置 key，这样各标签页保存的状态会跟着它移动：

```go
tabs := ui.TabsBase(c, &app.tab, len(app.files))
tabs.Reorder(func(from, to int) {
	f := app.files[from]
	app.files = slices.Delete(app.files, from, from+1)
	if to > from {
		to--
	}
	app.files = slices.Insert(app.files, to, f)
})
tabs.OnClose(func(i int) { app.files = slices.Delete(app.files, i, i+1) })
tabs.List.Gap(4).Children(func() {
	for i, f := range app.files {
		c.Key(f.Path)
		tabs.Tab(i).Padding(6, 12).Gap(6).Children(func() {
			ui.Text(c, f.Name)
			tabs.CloseButton(i).Padding(0, 4).Radius(4).Children(func() { ui.Text(c, "×") })
		})
	}
})
```

只有本列表自己的标签页能放到它上面。标签页在其他地方松开时（例如拖出窗口边缘，想在新窗口中打开它），它的 `Tab` 上的 `DragMissed` 会报告松开的位置。

见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术将其识别为标签页列表，按各标签页的标题命名，并能读取选中和禁用状态。关闭按钮对辅助技术隐藏，因为标签页的名称取自其中的全部文字；与 WAI-ARIA 的标签页一致，按 Delete 关闭有焦点的标签页。
