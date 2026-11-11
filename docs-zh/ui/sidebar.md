# 侧边栏

<!-- preview sidebar 280 -->

`ui.Sidebar` 是源列表，如同 Finder 和 Mail 的侧边栏：项带图标，分在节中，节的标题可以隐藏和显示这些项；它把选中项的 ID 写入 `*string`。项放不下时它会滚动。要给它设置宽度：

```go
ui.Sidebar(c, &app.mailbox, func() {
	ui.SidebarSection(c, "Mailboxes", &app.mailboxesShown, func() {
		ui.SidebarItem(c, "inbox", inboxIcon, "Inbox").Children(func() {
			ui.Badge(c, fmt.Sprint(app.unread))
		})
		ui.SidebarItem(c, "sent", sentIcon, "Sent")
	})
	ui.SidebarSection(c, "Tags", nil, func() {
		ui.SidebarItem(c, "work", nil, "Work")
	})
}).Width(220)
```

- `ui.SidebarItem` 选中它自己的 ID，在图标之后显示标签，没有图标时传 nil；用 `Children` 向它添加内容，如[徽章](badge.md)。
- `ui.SidebarSection` 在 `*bool` 为 true 时，把它函数构建的项放在标题下：单击标题会显示或隐藏这些项，单击它的箭头也一样；指针停在标题上时才会显示箭头。`*bool` 为 nil 时，该节保持打开。

## 选择

单击选中一个项；侧边栏持有焦点时，Up、Down、Home、End 和项的首字母在项之间选择，Tab 会离开它，因为它只是一个焦点停留点。`Changed` 报告新选择。侧边栏持有焦点时，选中的项以强调色显示，否则显示为灰色，如同 AppKit 的侧边栏。

配合[路由器](navigation.md)使用时（即你状态中的一个 `*ui.Router`），项的 ID 就是路径，新的选择会推送对应的页面：

```go
page := app.router.Path()
if ui.Sidebar(c, &page, func() {
	ui.SidebarItem(c, "/notes", nil, "Notes")
	ui.SidebarItem(c, "/files", nil, "Files")
}).Width(220).Changed() {
	app.router.Push(page)
}
```

## 辅助功能

辅助技术看到一棵树：各节的标题是可打开和关闭的项，包含该节的项；侧边栏持有焦点时，选中的项持有焦点。
