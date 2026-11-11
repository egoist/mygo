# 复选框

<!-- preview checkbox 220 -->

`ui.Checkbox` 创建一个切换 `*bool` 的复选框，带一个标签：单击框或它的标签会勾选或取消勾选，持有键盘焦点时按 Space 也一样，`Changed` 报告单击。

```go
ui.Checkbox(c, &app.settings.Sync, "Sync across devices")
if ui.Checkbox(c, &app.showHidden, "Show hidden files").Changed() {
	app.reload()
}
```

复选框在用户单击的瞬间就改变值：直接读取它；之后要做的工作则问 `Changed`。

## 组

`ui.CheckboxGroup` 在内部构建的复选框上方创建一个复选框，这些复选框缩进在它下方，如一组权限：全部勾选时它为勾选状态，部分勾选时为混合状态；单击它，或它在持有焦点时按 Space，会全部勾选，若已全部勾选则全部取消。`Changed` 报告单击。

```go
ui.CheckboxGroup(c, "Notifications", func() {
	ui.Checkbox(c, &app.mail, "Mail")
	ui.Checkbox(c, &app.calendar, "Calendar")
	ui.Checkbox(c, &app.messages, "Messages")
})
```

## 混合

`ui.CheckboxState` 是可以混合的复选框，如表格标题中的“全选”框：每一行都勾选时它勾选，部分勾选时混合。它设置 `*ui.CheckState`，即 `ui.CheckOff`、`ui.CheckOn` 或 `ui.CheckMixed`，混合时显示一个短横线；单击它，或它在持有焦点时按 Space，会勾选它，已经勾选时则取消勾选，`Changed` 报告单击：

```go
all := ui.CheckOff
switch n := app.selected(); {
case n == len(app.rows):
	all = ui.CheckOn
case n > 0:
	all = ui.CheckMixed
}
if ui.CheckboxState(c, &all, "").Label("Select all").Changed() {
	app.selectAll(all == ui.CheckOn)
}
```

`CheckboxGroup` 就是这样一个复选框，位于它所容纳的复选框之上。

## 无外观

`ui.CheckboxBase` 是无外观的复选框：单击一行即可切换它的 `*bool`，外观由你根据值绘制；`ui.CheckboxStateBase` 则设置 `*ui.CheckState`，如 `CheckboxState` 所做：见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一个由标签命名的复选框，处于勾选或未勾选状态，它像单击一样切换它；还看到一个可混合的复选框，已勾选、未勾选或混合。复选框组是一个复选框（部分勾选时为混合状态），其余复选框则组成一个由该标签命名的组。
