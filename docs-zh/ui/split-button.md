# 拆分按钮

<!-- preview split-button 120 -->

`ui.SplitButton` 创建一个与箭头连在一起的按钮，箭头用于打开相关操作的菜单，如“保存”旁边的“另存为…”、“运行”旁边的“运行方式…”，或“新建”旁边的“从模板新建…”。它是一个控件，绘制方式与[按钮](button.md)相同：两个区段共用一个表面和一条边框，两者之间有一条分隔线，指针悬停或按下时，两个区段各自独立亮起。`ui.PrimarySplitButton` 是使用强调色的拆分按钮，如同 `ui.PrimaryButton`。

它返回自身的各个部分：`Button` 是前导区段，显示标签并执行操作，它的 `Clicked` 报告单击，或它持有键盘焦点时按下的 Enter 或 Space，与按钮一样；`Arrow` 是尾部区段，打开其 `Menu` 提供的菜单，即[系统菜单](menu-button.md#任意按钮)，如同菜单按钮的菜单在它下方打开：

```go
save := ui.SplitButton(c, "Save")
save.Arrow.Menu(func(m *ui.Menu) {
	if m.Item("Save As…").Chosen() {
		app.saveAs()
	}
	if m.Item("Save a Copy…").Chosen() {
		app.saveCopy()
	}
})
if save.Button.Clicked() {
	app.save()
}
```

菜单接受与[上下文菜单](context-menu.md#菜单项)相同的菜单项；它的函数会在某项被选中后的那一帧再次运行，此时该项的 `Chosen` 报告结果。

## 在窗口中绘制

`Arrow.PopoverMenuButton` 为它提供一个由 MyGo 以主题外观在窗口中绘制的菜单，如同 [`ui.PopoverMenuButton`](menu-button.md#在窗口中绘制) 所做的那样，函数、按键和选项都相同：`ui.PopoverMenuButtonOptions` 可以把菜单放到箭头旁的其他位置，例如与箭头的末端对齐，并让应用打开和关闭菜单：

```go
run := ui.PrimarySplitButton(c, "Run")
run.Arrow.PopoverMenuButton(func(m *ui.Menu) {
	for _, target := range app.targets {
		if m.Item("Run " + target).Chosen() {
			app.run(target)
		}
	}
}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
if run.Button.Clicked() {
	app.run(app.target)
}
```

会绘制其所有菜单的窗口（[窗口的每个菜单](context-menu.md#窗口的每个菜单)）也会绘制 `Arrow.Menu` 的菜单。

## 内容与状态

用 `Button.Children` 和空标签给按钮添加其他内容，例如标签前的图标，并用 `Button.Label` 为它命名，与[按钮](button.md#内容)一样。容纳两个区段的 `Group` 在与同级元素之间的布局上与按钮一样：外边距、对齐方式或尺寸都加在它上面。

`Group.Disabled(true)` 禁用整个控件：两个区段都不接受单击或焦点，并且都显示为灰色。`Button.Disabled(true)` 只禁用操作，箭头仍能打开菜单，如同没有新内容可保存、但“另存为…”仍可用时的“保存”：

```go
save := ui.SplitButton(c, "Save")
save.Group.Disabled(app.saving)
save.Button.Disabled(!app.dirty)
```

在[工具栏](toolbar.md)中，它在指针悬停之前不显示自身表面，与工具栏的按钮一样；悬停时，它的表面、区段之间的分隔线，以及指针下的区段会以深一档的颜色显示。工具栏没有空间容纳它时，其溢出菜单会列出它的两个区段：选中按钮的区段会执行操作，选中箭头的区段则打开菜单。

在从右到左的布局中，箭头位于左侧，其菜单在箭头的起始侧打开，与[菜单按钮](menu-button.md)一样。

## 键盘

拆分按钮是一个 Tab 焦点停留点，Tab 会跳到上次持有焦点的区段，最开始是按钮。Left 和 Right 在区段之间移动焦点，在从右到左的布局中方向相反；在工具栏中，它们会继续前往其他控件。按钮持有焦点时，Enter 和 Space 单击它；箭头持有焦点时，它们打开菜单，Up 和 Down 也一样；Alt+Down 也会从按钮打开菜单。

## 辅助功能

辅助技术看到一个包含按钮和菜单按钮的组，这是拆分按钮的常见模式：按钮由文本或 `Label` 命名，菜单按钮由 `Arrow.Label` 命名，在此之前名为 "More options"，使用应用的语言；菜单显示时处于展开状态。`Group.Label` 为这个组命名。

## 在测试中

区段可以通过名称找到：`tt.Click("Save")` 单击按钮，`tt.Click("More options")` 或箭头自己的 `Label` 打开菜单；`tt.Menu` 列出菜单，`tt.ChooseMenuItem` 从中选择，若菜单绘制在窗口中，则由 `tt.Click` 选择它的菜单项。见[测试](testing.md)。
