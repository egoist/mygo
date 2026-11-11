# 树

<!-- preview tree 280 -->

`ui.Tree` 创建一棵树，其项目由 `ui.TreeItem` 构建，每个项目构建在它所属的项目内。项目的 `*bool` 为 true 时显示它的子项目；单击它的箭头会打开或关闭它，`Clicked` 报告单击它的其他位置或按 Enter（用于选中它），`Selected` 则显示选中状态。

```go
item := func(path, label string, open *bool, children func()) {
	if ui.TreeItem(c, label, open, children).Selected(app.chosen == path).Clicked() {
		app.chosen = path
	}
}
ui.Tree(c, func() {
	item("src", "src", &app.srcOpen, func() {
		item("src/main.go", "main.go", nil, nil)
	})
	item("go.mod", "go.mod", nil, nil)
})
```

子项目为 nil，或 open 为 nil，都会使该项目成为叶子。

## 键盘

树是一个 Tab 焦点停留点，Tab 会跳到上次持有焦点的项目，或 `Selected` 的项目。项目持有键盘焦点时，Up 和 Down 把它移到相邻的项目，Home 和 End 移到第一个和最后一个项目，Right 打开项目或移到它的第一个子项目，Left 关闭它或移到它的父项目；在从右到左的布局中，Left 打开，Right 关闭。键入项目的首字母会移到它，再次按同一个字母会移到下一个以该字母开头的项目。

树会构建它显示的每个项目。对于数千个项目的树，或带列的树，使用[大纲](outline.md)，它只构建可见的行。

## 无外观

`ui.TreeBase` 是无外观的树，用于自定义外观的行，例如 JSON 查看器的行：一个容纳项目的 `Root` 列，每个项目由 `Item` 创建，其 `Row` 由你设置样式并填充，其 `Group` 在行下方的列中构建其中的项目，前提是其 `*bool` 为 true（nil 会使其成为叶子）。树具有与 `Tree` 相同的按键和焦点，`Depth` 说明项目的深度，可用于缩进行的行。在 `Item` 之前用 `Context.Key` 设置的键，会在其上方的项目打开或关闭时把焦点保持在项目上。

```go
var node func(n *Node)
tree := ui.TreeBase(c)
tree.Root.Children(func() {
	node = func(n *Node) {
		c.Key(n.path)
		item := tree.Item(n.open) // nil for a leaf
		row := item.Row.Gap(4).Padding(3, 8).PaddingStart(8 + 16*float32(item.Depth())).Radius(6)
		if app.chosen == n.path {
			row.Background(t.Surface)
		}
		if row.Clicked() {
			app.chosen = n.path
		}
		row.Children(func() {
			if n.open != nil {
				arrow := ui.Icon(c, chevron).Size(16, 16)
				if *n.open {
					arrow.Rotate(90)
				}
				if arrow.Clicked() {
					*n.open = !*n.open
				}
			}
			ui.Text(c, n.key).FontWeight(500)
			ui.Text(c, n.value).TextColor(t.TextMuted)
		})
		item.Group(func() {
			for _, kid := range n.children {
				node(kid)
			}
		}) // nil while closed; style it, as with a line along it
	}
	for _, n := range app.roots {
		node(n)
	}
})
```

`Tree` 是带主题外观的 `TreeBase`。`TreeBase` 构建每个显示出来的项目；对于数千个项目的树，使用带有你自己 `Prefix` 的[大纲](outline.md#自己的行)。见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一棵由项目组成的树，每个项目都有层级，在有子项目时还有打开或关闭状态，并说明它在显示的项目中的位置，如同大纲的行；它像箭头一样打开和关闭它们。
