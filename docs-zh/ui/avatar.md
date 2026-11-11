# 头像

<!-- preview avatar 150 -->

`ui.Avatar` 创建一个人或一件东西的头像：有图像时把它裁成圆形；没有图像时显示名字的首字母，颜色由名字决定，因此每个人的颜色保持不变。它的高度略大于主题字号的两倍。

```go
ui.Row(c).Gap(8).Children(func() {
	ui.Avatar(c, user.Name, user.Photo) // a *ui.Bitmap, or nil
	ui.Text(c, user.Name)
})
```

## 辅助功能

辅助技术看到一张以名字命名的图像。
