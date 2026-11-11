# 按钮

<!-- preview button 140 -->

`ui.Button` 创建一个显示标签的按钮，`ui.PrimaryButton` 则创建一个强调色的按钮，用于窗口或对话框的主要操作。`Clicked` 报告单击，或按钮持有键盘焦点时按下的 Enter 或 Space：

```go
ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
	if ui.Button(c, "Cancel").Clicked() {
		app.editing = false
	}
	if ui.PrimaryButton(c, "Save").Clicked() {
		app.save()
	}
})
```

## 内容

按钮把子元素排成一行，并把自身的文本颜色给它们。用 `Children` 配合空标签给它其他内容，如标签前的图标：

```go
ui.PrimaryButton(c, "").Children(func() {
	ui.Icon(c, save)
	ui.Text(c, "Save").SingleLine()
})
```

只显示图标的按钮需要 `Label`（供辅助技术命名）和 `Tooltip`：

```go
if ui.Button(c, "").Label("Delete").Tooltip("Delete").Children(func() { ui.Icon(c, trash) }).Clicked() {
	app.delete()
}
```

## 状态

`Disabled(true)` 把按钮变灰：它既不接受单击，也不接受焦点。`AccessDisabled(true)` 只告诉屏幕阅读器它不可用，同时保留焦点和外观，用于用户仍必须到达的按钮，如表单无效时的提交按钮：见[辅助功能](accessibility.md#不可用但仍可到达)。在[工具栏](toolbar.md)中，按钮在悬停前没有自己的外观。若要让某个默认按钮在焦点位于任何位置时都由 Enter 按下，就用[快捷键](input.md#快捷键)接管该键：

```go
if ui.PrimaryButton(c, "Sign in").Clicked() || c.Shortcut(0, ui.KeyEnter) {
	app.signIn()
}
```

持有焦点的按钮会独占 Enter 和 Space，因此快捷键只在焦点在别处时触发。

## 样式

按钮返回自身的元素：其后的调用会改变它的外观，如 `ui.Button(c, "Save").Padding(10, 20).Radius(999)`。`ui.ButtonBase` 是无外观的按钮，一个可获取焦点并报告 `Clicked` 的行，用于你自己的设计：见[自定义控件](custom-widgets.md)。

## 辅助功能

辅助技术看到一个由文本或 `Label` 命名的按钮，按下它的方式与单击一样。

## 点击操作

在构建过程中需要分支时用 `Clicked()`，要在构建后执行操作则用 `OnClick`：

```go
ui.Button(c, "Save").Disabled(app.saving).OnClick(app.save)
```

回调看到的是最终配置，并在绑定值处理输入之后运行。它可以在不中断正在构建控件的循环的情况下改变模型。已处理的输入会在视图重建前被消耗。关于如何在查询和回调之间选择，见[事件与动作](views.md#事件与动作)。
