# 菜单按钮

<!-- preview menu-button 120 -->

`ui.MenuButton` 创建一个带标签和箭头的按钮，按下它在下方打开系统菜单，如同系统的弹出按钮：指针在它上面按下时，或它在持有键盘焦点时按 Enter、Space、Up 或 Down 也打开。函数在菜单打开时构建菜单项，并在某项被选中后的那一帧再次运行，此时它的 `Chosen` 报告所选项：

```go
ui.MenuButton(c, "Sort by", func(m *ui.Menu) {
	for _, by := range []string{"Name", "Date", "Size"} {
		if m.Item(by).Checked(app.sort == by).Chosen() {
			app.sort = by
		}
	}
})
```

## 菜单项

菜单接受与[上下文菜单](context-menu.md#菜单项)相同的菜单项：`Item`（带 `Checked`、`Radio`、`Disabled` 和 `Shortcut`）、`Separator` 和 `Submenu`。

## 任意按钮

`Element.Menu` 为任意按钮提供同样的菜单，例如只显示图标的按钮：

```go
ui.Button(c, "").Label("More").Children(func() { ui.Icon(c, dots) }).Menu(func(m *ui.Menu) {
	if m.Item("Duplicate").Chosen() {
		app.duplicate()
	}
	if m.Item("Delete").Chosen() {
		app.delete()
	}
})
```

在[工具栏](toolbar.md)中，菜单按钮在悬停前没有自己的外观。若按钮除了相关菜单外还有自己的操作，例如 Save 旁边的 Save As…，请使用[拆分按钮](split-button.md)。

## 在窗口中绘制

`ui.PopoverMenuButton` 接受相同的函数，MyGo 会在系统显示自身菜单的位置，以主题外观把菜单绘制在窗口中，如同 [`PopoverMenu`](context-menu.md#在窗口中绘制) 绘制上下文菜单：在应用自己设计的浮层和按钮旁边，系统的菜单反而显得格格不入；右键打开的绘制菜单与 ⋯ 按钮的系统菜单并排时，看起来就像两种菜单。`Element.PopoverMenuButton` 像 `Element.Menu` 一样为任意按钮提供这样的菜单，并接受与 `PopoverMenu` 相同的函数，因此一个函数可以两处共用。要这样绘制窗口的每个菜单，包括 `Menu` 的，见[窗口的每个菜单](context-menu.md#窗口的每个菜单)：

```go
menu := func(m *ui.Menu) {
	if m.Item("Rename").Shortcut(0, ui.KeyF2).Chosen() {
		app.renaming = i
	}
	if m.Item("Pinned").Checked(note.pinned).Chosen() {
		note.pinned = !note.pinned
	}
	m.Separator()
	if m.Item("Delete").Disabled(note.locked).Chosen() {
		app.delete(i)
	}
}
row.PopoverMenu(menu)
row.Children(func() {
	ui.Text(c, note.title).Grow(1)
	ui.Button(c, "").Label("More").Children(func() { ui.Icon(c, dots) }).PopoverMenuButton(menu)
})
```

它像系统菜单那样打开：指针在按钮上按下时，或按钮持有焦点时按 Enter、Space、Up 或 Down；位置在按钮下方，与起始端对齐，窗口没有空间时则在按钮上方。再次按下按钮则会关闭它。它具备 `PopoverMenu` 的按键、焦点和外观：打开时取得焦点，由键盘打开时焦点落在第一项上，关闭时把焦点还给按钮；Up、Down、Home、End 和键入在各项之间移动，Right 和 Left 打开和关闭子菜单，Enter 和 Space 选中，Escape、Tab 和在外部按下会关闭它。选中会像 `PopoverMenu` 那样报告 `Chosen`。

`ui.PopoverMenuButtonOptions` 放在函数之后，用来把菜单放到别处，并让应用打开和关闭它。`At` 和 `Self` 像 [`AttachTo`](overlays.md#你自己的浮层) 那样附着菜单，把 `Self` 点放在按钮的 `At` 点上：`AnchorBottomEnd` 和 `AnchorTopEnd` 让它与按钮的末端对齐，适用于窗口末端的 ⋯ 按钮，`AnchorTopStart` 和 `AnchorBottomStart` 把它放在上方，`AnchorTopEnd` 和 `AnchorTopStart` 放在按钮之后。窗口没有空间时，它仍会翻到另一侧。`Open` 是一个 `*bool`，表示菜单是否显示，如同 `MenuBase` 的：用户打开和关闭菜单时由按钮设置它，应用也可以设置它来打开或关闭菜单，例如它作用的对象在后台消失时：

```go
more.PopoverMenuButton(menu, ui.PopoverMenuButtonOptions{
	At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd, Open: &app.moreOpen,
})

// Later, on an event from elsewhere, before the window updates:
app.moreOpen = false
```

由应用打开的菜单会取得焦点，但没有高亮的菜单项，关闭时把焦点还给原先持有焦点的元素。一个窗口一次只显示一个自绘菜单：另一个打开时会关闭当前的菜单，把 `Open` 设为 false。

`Panel` 在主题外观之后设置菜单面板及每个子菜单的面板，用于使用应用自身表面、而非主题的 `Background`、`Border` 和阴影的菜单。它的背景、边框、圆角和内边距取代主题中的对应设置；`Shadow` 添加阴影，因此要先用 `NoShadow` 去掉主题的阴影。菜单保留它的位置、与按钮的间距以及菜单项的外观：

```go
more.PopoverMenuButton(menu, ui.PopoverMenuButtonOptions{
	Panel: func(panel ui.Element) {
		panel.Background(app.menuColor).Border(1, app.line).Radius(12).
			NoShadow().Shadow(0, 18, 50, 0, ui.RGBA(0, 0, 0, 0.45))
	},
})
```

辅助技术看到菜单按钮，在菜单显示时处于展开状态，还看到菜单及其菜单项、复选框项和单选按钮项，如同看到系统菜单。在工具栏的溢出菜单中，选中该按钮会在溢出按钮下方打开它自绘的菜单。

自绘菜单的菜单项是元素：在测试中，对按钮 `tt.Click` 会打开它，`tt.HasText` 找到它的菜单项，`tt.Click` 选中一项。`tt.Menu`、`tt.ChooseMenuItem` 和 `tt.CloseMenu` 在它上面的工作方式与系统菜单相同，因此测试在两种菜单下都能通过。

## 没有外观

`ui.MenuBase` 是无外观的菜单按钮，用于菜单项采用你自己布局的菜单，例如头像和名字，或色块：它的 `Trigger` 是按钮，接受子元素；`*bool` 为 true 时菜单显示。`Popup` 构建菜单，如同 [`PopoverBase`](popover.md#无外观) 构建面板：给面板设置样式，并在其中构建菜单项——`Item`、`CheckItem`、`RadioItem` 和 `SubmenuItem` 生成的每一项都是一行，接受子元素——以及用 `Submenu` 构建子菜单。指针或按键在某一项上时它是 `Highlighted`，它的 `Clicked` 报告选择结果，并关闭菜单：

```go
m := ui.MenuBase(c, &app.accountMenu)
m.Trigger.Gap(6).Padding(4, 8).Radius(999).Children(func() {
	ui.Avatar(c, app.user.Name, app.user.Photo).Size(24, 24)
	ui.Icon(c, chevron)
})
m.Popup(func(panel ui.Element) {
	panel.Margin(4, 0, 0, 0).Padding(4).Radius(10).Background(t.Background).Border(1, t.Border)
	for _, acct := range app.accounts {
		item := m.RadioItem(acct == app.user).Gap(8).Padding(6, 10).Radius(6)
		if item.Highlighted() {
			item.Background(t.SurfaceHover)
		}
		item.Children(func() {
			ui.Avatar(c, acct.Name, acct.Photo).Size(20, 20)
			ui.Text(c, acct.Name)
		})
		if item.Clicked() {
			app.switchTo(acct)
		}
	}
	more := m.SubmenuItem().Padding(6, 10).Children(func() { ui.Text(c, "Settings") })
	m.Submenu(more, func(panel ui.Element) {
		panel.Padding(4).Radius(10).Background(t.Background).Border(1, t.Border)
		if m.Item().Padding(6, 10).Children(func() { ui.Text(c, "Profile") }).Clicked() {
			app.openProfile()
		}
	})
})
```

它具备 `PopoverMenuButton` 的按键和焦点，后者基于相同的部件并采用主题外观：指针在按钮上按下时打开，按钮持有焦点时按 Enter、Space、Up 或 Down 也会打开，此时焦点落在第一项上；Up、Down、Home、End 和键入菜单项的前几个字母会移动高亮，并跳过 `Disabled` 的项；Right 和 Left 打开和关闭子菜单，在从右到左的布局中方向相反；Enter 和 Space 选中，Escape 关闭最上层的子菜单，Tab 和在外部按下会关闭菜单，并把焦点还给按钮。设置 `*bool` 也能打开和关闭它。辅助技术看到菜单按钮，在菜单显示时处于展开状态；看到菜单及其菜单项、复选框项和单选按钮项，选中状态由 `CheckItem` 和 `RadioItem` 决定；每个子菜单以它的菜单项命名。分隔符和标题是你在菜单项之间自行添加的元素。见[自定义控件](custom-widgets.md)，上下文菜单见 [`ContextMenuBase`](context-menu.md#不带外观)。

带 `KeepOpen` 的菜单项在被指针、Enter 或 Space 选中时会让菜单保持打开：它的 `Clicked` 报告选择，它保持高亮，如同 Radix 的菜单在 `onSelect` 阻止默认行为时那样。`Group` 生成一行菜单项，按键将它们当作一个整体：Up 和 Down 只停在它上面一次，停在它选中的项上，否则停在第一项；Left 和 Right 在它的各项之间循环移动，在从右到左的布局中方向相反。账户菜单中的主题切换器就是这样一行单选按钮项：

```go
m.Group(func(row ui.Element) {
	row.Gap(2).Padding(4).Label("Theme")
	for _, th := range []string{"Light", "Dark", "System"} {
		item := m.RadioItem(app.theme == th).KeepOpen().Grow(1).Padding(4, 8).Radius(6)
		if item.Highlighted() {
			item.Background(t.SurfaceHover)
		}
		item.Children(func() { ui.Text(c, th) })
		if item.Clicked() {
			app.theme = th
		}
	}
})
```

辅助技术看到一个组，由它的 `Label` 命名，其中包含单独计数的单选按钮项（组外的项一起计数）。

## 辅助功能

辅助技术看到一个菜单按钮：macOS 上是 `AXMenuButton`，Linux 上是带菜单的推送按钮，Windows 上是一个可展开的按钮。
