# 警告对话框

<!-- preview alert-dialog 240 -->

`ui.AlertDialog` 在 `*bool` 为 true 时，在窗口上方显示警告框，用于确认重要操作，类似 AppKit 的警告框：包含标题、消息和多个按钮。最后一个是默认按钮，使用强调色并获得键盘焦点，按 Enter 即可触发。无论按钮的文字和语言是什么，按 Escape 都会关闭警告框，效果相当于取消，不选择任何按钮；单击警告框外部则无效。单击按钮的那一帧会关闭警告框，并返回该按钮的索引；按 Escape 关闭的那一帧返回 `ui.AlertDismissed`，其余情况返回 -1。

```go
if ui.Button(c, "Move to Trash").Clicked() {
	app.asking = true
}
switch ui.AlertDialog(c, &app.asking, "Move “Notes” to the Trash?", "You can put it back from the Trash.", "Cancel", "Move to Trash") {
case 1:
	app.trash()
}
```

必须得到回答的警告框，可以在 Escape 将其关闭时把 `*bool` 设回 true，从而保持打开：

```go
if ui.AlertDialog(c, &app.updating, "Update required", "Restart to finish updating.", "Restart") == ui.AlertDismissed {
	app.updating = true
}
```

警告框会在窗口允许的范围内超出通常的宽度，让按钮留在同一行，标题和消息也按这个宽度换行。在窄到无法做到这一点的窗口里，按钮会换到更多行；长过警告框的标签则以省略号结尾。

要展示你自己内容的对话框，用[对话框](dialog.md)；对于用户可以撤销的选择，立即执行，并在 [toast](toast.md) 中提供撤销。

## 破坏性操作

`ui.AlertDialogButtons` 接收 `ui.AlertButton`，其中标为 `Destructive` 的按钮用于删除数据或无法撤销的操作，类似 AppKit 设置了 `hasDestructiveAction` 的警告框：它们的标签使用主题的 `Danger` 颜色，并且永远不会成为默认按钮。默认按钮是最后一个非破坏性按钮，例如“取消”，这样按 Enter 会保留数据。所有按钮都是破坏性按钮的警告框没有默认按钮：焦点停在警告框本身，按 Enter 不会触发任何按钮。与其他按钮一样，只有当用户用 Tab 把焦点移到破坏性按钮上之后，Enter 才会触发它。颜色本身不会告诉使用辅助技术的用户任何信息：让标签说明按钮做什么，例如用“Delete”而不是“OK”。

```go
switch ui.AlertDialogButtons(c, &app.asking, "Delete “Notes”?", "You can't undo this.",
	ui.AlertButton{Label: "Cancel"}, ui.AlertButton{Label: "Delete", Destructive: true}) {
case 1:
	app.delete()
}
```

## 辅助功能

辅助技术看到一个由其标题命名、由其消息描述的警告对话框，焦点在它的默认按钮上；没有默认按钮时，焦点在警告框本身。
