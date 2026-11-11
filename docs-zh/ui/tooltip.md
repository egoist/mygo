# 工具提示

<!-- preview tooltip 160 -->

`Tooltip` 在指针停留在元素上，或键盘焦点移到元素上之后显示提示，并向辅助技术描述这个元素：

```go
ui.Button(c, "").Label("Share").Tooltip("Share with others").Children(func() {
	ui.Icon(c, share)
})
```

指针在元素上停留 0.6 秒后，提示就会显示；键盘焦点通过键盘移到它上面时则立即显示。它会显示在元素上方并居中，上方没有空间时显示在下方，无论指针位于元素上的哪个位置，如同 Base UI 的提示。一个提示消失后的 0.4 秒内，下一个提示会立即显示，例如指针沿工具栏移动时。指针离开时它会消失；在元素上按下或单击，或按 Escape，会让它隐藏，直到指针离开后再次回来，或焦点再次到来：单击之后它不会再显示，菜单按钮打开的菜单旁也不会显示。对于彼此嵌套的元素，显示最内层元素的提示，且一次只显示一个提示。给只显示图标的按钮，以及仅靠标签无法说明的内容配上提示。

焦点归属于元素把焦点交给的那一方，即它的 `ActiveDescendant`，如同窗口中绘制的菜单把焦点交给按键高亮的项：提示显示在该项上。

提示采用主题的 `Inverse` 和 `InverseText` 颜色，除非主题另行设置，否则它的文本和背景会互换（见[主题](styling.md#主题)）。

## 延迟

主题的 `TooltipDelay` 设置指针在元素上停留多久后显示提示，`Tooltip` 和 `TooltipBase` 都一样：为 0 时表示 0.6 秒，延迟为负时立即显示提示。下一个提示立即显示的时限随之变化，是它的三分之二（默认 0.4 秒）。内容密集的设置页会更早显示提示，而有许多小按钮的工具栏，或同时还内联显示帮助的页面，会更晚：

```go
t := *c.Theme()
t.TooltipDelay = 300 * time.Millisecond
c.SetTheme(&t)
```

若只用于窗口的一部分，例如工具栏，就在 `ui.Themed` 中用那个主题构建它（见[窗口一部分的主题](styling.md#窗口一部分的主题)）；每个元素的提示会按构建它时所用的主题等待。

## 无外观

`TooltipBase` 在元素旁显示自定义提示，如同 Base UI 的 Tooltip：它按上文所述决定提示何时显示和消失，`fn` 设置提示的样式并构建其内容。提示显示在元素上方并居中，上方没有空间时显示在下方；用 `AttachTo` 把它放在别处，其外边距使它和元素保持距离。`AttachedAnchors` 说明它实际落到了哪一侧，`AttachedOrigin` 让它从最靠近元素的边缘缩放（见[浮层](overlays.md#你自己的浮层)）：

```go
b := ui.Button(c, "").Label("Share").Description("Share with others")
b.Children(func() { ui.Icon(c, share) })
ui.TooltipBase(c, b, func(tip ui.Element) {
	tip.AttachTo(b, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 6)
	tip.Padding(4, 8).Radius(6).Background(ink).TextColor(paper)
	tip.Children(func() {
		ui.Text(c, "Share with others")
		ui.Text(c, "⌘S").TextColor(muted)
	})
})
```

它返回提示元素，不显示时返回 nil。指针可以穿过提示。

## 辅助功能

辅助技术把 `Tooltip` 的提示作为帮助文本，在名称之后读作元素的描述，除非 `Description` 给出了其他描述。它把 `TooltipBase` 的提示看作工具提示，但不会朗读：应给元素一个 `Description`，说明提示的内容。
