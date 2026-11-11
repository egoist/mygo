# 徽标

<!-- preview badge 180 -->

`ui.Badge` 创建一个药丸状的短文本，例如[侧边栏](sidebar.md)条目旁的未读消息数：

```go
ui.SidebarItem(c, "inbox", inboxIcon, "Inbox").Children(func() {
	ui.Badge(c, fmt.Sprint(app.unread))
})
```

它返回自身的元素：用 `Background` 和 `TextColor` 为它上色，例如用强调色的徽标标示需要注意的内容。

```go
ui.Badge(c, "New").Background(t.Accent).TextColor(t.AccentText)
```

## 辅助功能

辅助技术在读取它所在的条目时一并读出它的文本。
