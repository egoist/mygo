# 大纲

<!-- preview outline 320 -->

`ui.Outline` 显示一棵项目树，如同 AppKit 的大纲视图和 Finder 的列表视图：顶层是各个项目，每个项目下方缩进显示它展开的子项目，并带一个打开和关闭它的箭头。应用提供顶层项目和一个 `children` 函数，后者返回某个项目的子项目，没有子项目时返回 nil；大纲只构建可见的行，与[列表](list.md)一样，因此任意大小的树都和小树一样快。

```go
type fileTree struct {
	children map[string][]string // the paths in each directory
	files    ui.OutlineState[string]
	row      int
}

func (app *fileTree) view(c *ui.Context) {
	app.files.List.Selected = &app.row
	ui.Outline(c, &app.files, []string{"/"}, func(dir string) []string {
		return app.children[dir] // nil for a file
	}, func(path string) {
		ui.Text(c, filepath.Base(path))
	}).Grow(1)
	chosen := app.files.Item(app.row)
	// …
}
```

## 大纲状态

它的状态 `OutlineState` 包含各行的 `ListState`（它像列表的 `ListState` 一样选择行：`Selected`、`Selection`），以及 `Open`，即一个 `Selection`，包含正在显示子项目的项目。行的键就是项目，因此当上方的项目打开或关闭时，行的选择和状态会跟随项目。`Rows`、`Item` 和 `Depth` 读取显示出来的行。`Indent` 是每一层在行箭头前增加的宽度，为零时是主题的 `Space(4)`；它不能为负。`Prefix` 改为构建行的开头（[自己的行](#自己的行)）。

## 打开和关闭

- 单击项目的箭头会打开或关闭它；按住 Option 单击（按住 Alt 单击）也会打开或关闭其中的所有项目。
- 大纲持有焦点时，Right 打开选中的项目或转到它的第一个子项目，Left 关闭它或转到它的父项目；在 macOS 上按住 Option、在其他平台上按住 Shift（如同 GTK），它们会打开或关闭其中的所有项目。
- 若关闭的项目有子项目被选中，则改为选中该项目。

## 自己的行

`Prefix` 是 `OutlineState` 的函数，代替缩进和箭头构建每一行的起始部分，用于自定义外观的行，例如 JSON 查看器或沿层级画线的文件树：它的 `OutlineRow` 说明该行的项目有多深（`Depth`）、是否有子项目（`Branch`）以及它们是否显示（`Open`），`Arrow` 创建用于打开和关闭它的盒子，与大纲的箭头一样，由你设置尺寸并绘制。此时行既没有内边距也没有间距，并居中其中的内容；按键、选择和辅助技术仍由大纲负责。

```go
app.files.Prefix = func(r ui.OutlineRow) {
	ui.Box(c).Width(16 * float32(r.Depth)).Shrink(0)
	arrow := r.Arrow().Size(20, 28).Center()
	if r.Branch {
		arrow.Children(func() {
			icon := ui.Icon(c, chevron)
			if r.Open {
				icon.Rotate(90)
			}
		})
	}
}
```

## 在列中

`ui.OutlineTable` 在[表格](table.md)的列中提供同样的功能，第一列显示箭头，按项目的深度缩进：这些列就是表格的列，用户可以排序、调整大小和移动。

```go
ui.OutlineTable(c, &app.files, cols, roots, children, func(path string, col int) {
	switch col {
	case 0:
		ui.Text(c, filepath.Base(path)).SingleLine()
	case 1:
		ui.Text(c, app.size(path))
	}
})
```

## 辅助功能

辅助技术看到一棵树，或一个行可打开和关闭的表格，其中的行会说明自己的层级、能否打开、是否已打开，以及在所有行中的位置；它像箭头一样打开和关闭它们。
