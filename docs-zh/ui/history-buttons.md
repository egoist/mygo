# 后退和前进按钮

<!-- preview history-buttons 240 -->

`ui.BackButton` 和 `ui.ForwardButton` 创建在[路由器](navigation.md)的历史中
后退和前进的按钮，如同 Finder 或浏览器的工具栏中那样：在历史的两端禁用，并
显示一个尖角符号。给它们路由器，即你状态中的一个 `*ui.Router`：

```go
ui.Toolbar(c, func() {
	ui.BackButton(c, app.router)
	ui.ForwardButton(c, app.router)
	ui.Text(c, app.router.Title()).Bold()
})
```

右键单击会按页面标题（路由的 `Title`）列出可后退或前进的页面，最近的在前；没有
标题的则按路径列出。选中一项会立即转到那里。

这些按钮是已有按键操作的快捷键：macOS 上是 Cmd+[ 和 Cmd+]，其他平台上是
Alt+Left 和 Alt+Right，以及鼠标的侧键。

## 辅助功能

辅助技术看到名为 "Back" 和 "Forward" 的按钮，它们在历史的两端不可用。
