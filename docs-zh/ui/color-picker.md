# 颜色选择器

<!-- preview color-picker 430 -->

`ui.ColorWell` 创建一个绑定到 `*ui.Color` 的颜色控件，以色块显示当前颜色，单击色块或按 Enter、Space 可在下方打开颜色选择器，类似 AppKit 的颜色控件；按 Escape 或单击外部可关闭选择器。通过 `Changed` 可检测颜色变化。

```go
ui.Field(c, "Tint", func() {
	ui.ColorWell(c, &app.tint)
})
```

`ui.ColorPicker` 是选择器本身，用于显示在你自己的面板中，如同 AppKit 的颜色面板：一个方块，横向选择饱和度、纵向选择亮度，色相由它下方的滑块选择；一个不透明度滑块；用户可编辑的十六进制颜色；以及常见颜色的色块。它持有焦点时，方向键在方块中移动，Up 和 Down 改变亮度，Left 和 Right 改变饱和度。

```go
ui.ColorPicker(c, &app.background)
```

## 辅助功能

辅助技术看到一个颜色块，其值是十六进制颜色，由它的 `Label` 或它所在的[字段](form.md)命名。在选择器中，它看到饱和度和亮度的滑块、色相和不透明度的滑块、名为“Hex”的文本框，以及以各色块颜色命名的按钮，名称都用窗口的语言（[MyGo 自己的字符串](../i18n.md#mygos-own-strings)）。
