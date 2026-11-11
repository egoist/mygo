# 警告对话框

<!-- preview alert-dialog 240 -->

`ui.AlertDialog` 在 `*bool` 为 true 时，像 AppKit 的警告框那样浮在窗口之上询问重要的事：一个标题、一条消息和几个按钮，最后一个按钮是默认按钮，带强调色并持有焦点，按 Enter 会单击它。Escape 关闭警告框，不选择任何按钮，效果同取消，无论按钮上写的是什么、用的是哪种语言；单击警告框外面则什么也不做。它返回被单击按钮的索引，并在单击发生的那一帧关闭警告框；在 Escape 关闭它的那一帧返回 `ui.AlertDismissed`，其他情况返回 -1。

```go
if ui.Button(c, "Delete").Clicked() {
	app.asking = true
}
switch ui.AlertDialog(c, &app.asking, "Delete “Notes”?", "You can't undo this.", "Cancel", "Delete") {
case 1:
	app.delete()
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

## 辅助功能

辅助技术看到一个由其标题命名、由其消息描述的警告对话框，焦点在它的默认按钮上。
