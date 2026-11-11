# 标签页

<!-- preview tabs 160 -->

`ui.Tabs` 创建一行带标签的标签页，`*int` 是所选标签页的索引。单击选中一个标签页，方向键、Home 和 End 在某个标签页持有键盘焦点时也一样，焦点会跟随选择；`Changed` 报告新选择。在它们下方构建所选标签页的页面：

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

这行标签页是一个 Tab 焦点停留点，Tab 会跳到选中的标签页。若页面需要历史、可由 Back 返回，用[路由器](navigation.md)；若在工具栏中选择视图，用[分段控件](segmented.md)。

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

见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一个标签页列表，其中的标签页由各自的标签命名，选中的处于选中状态，禁用的处于禁用状态。
