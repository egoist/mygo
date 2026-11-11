# 从右到左

阿拉伯语、希伯来语、波斯语和乌尔都语从右到左阅读，这些语言的应用界面也如此：一行从右边开始，侧边栏在右边，后退箭头指向右。当应用的语言从右到左阅读时，原生 UI 会这样排版窗口，如同 Flutter 的 `Directionality`、SwiftUI 的 `layoutDirection`、CSS 的 `direction` 和 GTK 的文本方向。坐标系不会镜像，如同在 Godot 中：X 在 `Rect` 中、在 `Draw` 绘制的内容中，以及指针位置中的坐标仍然向右增长。只有布局会镜像。

## 方向

`ui.LeftToRight` 和 `ui.RightToLeft` 是两个方向（`ui.Direction`）。窗口采用应用语言的方向，由包 `mygo` 设置；每个元素则采用它所在元素的方向。`Direction` 让元素及其中构建的内容拥有自己的方向，例如电话号码、代码示例或媒体播放器的控件，它们在从右到左的窗口中仍保持从左到右：

```go
ui.Row(c).Direction(ui.LeftToRight).Gap(8).Children(func() {
	ui.Button(c, "⏮")
	ui.Button(c, "⏯")
	ui.Button(c, "⏭")
})
```

要在创建元素之后、构建子元素之前设置它，如同 `Key`：控件在构建时读取方向。附着在元素上的弹出内容（浮层、组合框的选项、工具提示）以及 `Overlay` 构建的内容，采用请求它们处的方向。

`Context.Direction` 返回当前构建内容的方向，供自行绘制沿线指向内容的控件使用：

```go
forward := "M5 2 11 8 5 14"
if c.Direction() == ui.RightToLeft {
	forward = "M11 2 5 8 11 14"
}
```

## 什么会镜像

从右到左：

- `Row` 从右侧排布子元素，`Column` 从右侧横向对齐它们：`Justify`、`AlignItems` 和 `AlignSelf` 从方向中取得 `Start` 和 `End`。`Reverse` 则从左侧排布一行。网格从右侧放置列，`JustifyItems` 和 `JustifySelf` 从其单元格的右侧开始。
- `TextAlign` 的 `Start` 是元素的右边，无论文本本身如何阅读，如同 CSS、AppKit 和 GTK，`End` 是左边。段落的阅读方向由其中第一个强方向字符决定，这是 Unicode 的双向算法得出的；一个没有任何此类字符的段落（例如只有数字或空行）则从右到左。从左到右时，一切不变：阿拉伯语的段落仍然从右边开始，如同 Apple 的指南按语言对齐段落。
- 逻辑边缘的起始侧位于右边：`PaddingStart`、`PaddingEnd`、`MarginStart`、`MarginEnd`、`BorderStartWidth`、`BorderEndWidth`、定位元素的 `Start` 和 `End`（以及 `StartPercent`、`EndPercent`），以及锚点 `AnchorTopStart`、`AnchorStart`、`AnchorBottomStart`、`AnchorTopEnd`、`AnchorEnd` 和 `AnchorBottomEnd`，如同 CSS 的逻辑属性和 Flutter 的 `EdgeInsetsDirectional` 与 `AlignmentDirectional`。没有从任何一侧定位的绝对元素会去右边；同时给出 `Left` 和 `Right`、而宽度容不下两者时，`Right` 优先。
- 横向滚动从右边开始，`ScrollState.X` 从那里开始计数（0 显示起始，`MaxX` 显示末尾），如同 Flutter 的偏移；滚轮和触控板像从左到右时那样移动内容，Shift 加滚轮则朝末尾滚动。垂直滚动条在左边。
- 控件会镜像沿线指向的内容：开关的开启位置在左边；滑块、范围滑块、进度条和量度计从右边填充，方向键按它们指向的方向移动旋钮；标签页、分段控件、工具栏、评分、网格视图和日历用 Left 前往下一个项；展开箭头、面包屑的尖角符号和工具栏的溢出按钮指向左，后退按钮则指向右；树、大纲和展开控件从右侧缩进，并用 Left 展开；分栏的第一个窗格在右侧；表格的列从右侧开始，并从左边缘调整大小；路由器推送的页面从左侧滑入，Alt+Right 则后退。
- 浮层、选择器和组合框的选项，以及菜单按钮的菜单，在打开它们的元素的起始侧打开，上下文菜单按从右到左排版，并在指针左侧打开。
- 在单行文本输入中，文本从右边开始，占位符也如此，空输入的插入符也在那里。方向键按它们指向的方向移动插入符，穿过任一方向的文本；Option（其他平台上为 Control）和 Command 与方向键组合时，会按它们在从右到左段落中指向的方向按词移动、移到行尾，如同 AppKit 和 GTK 那样。

## 什么不会镜像

- 物理的东西保持物理，如同 CSS：`Left`、`Right`、`Padding(top, right, bottom, left)`、`BorderWidth`、`AnchorTopLeft`、`Radius` 的角、渐变的角度、变换。
- 文本、图像、图标和 `Draw` 绘制的内容不会镜像：对勾、放大镜、加载指示器和图表仍按从左到右的方式阅读。必须指向另一边的内容，要在读取 `Context.Direction` 之后再绘制。
- 代码从左到右阅读：代码编辑器和差异查看器（`ui/editor`）、Markdown 的代码块和终端保持自己的方向，时间输入也如此，因为时间就是这样书写的。
- 富文本编辑器（`ui/richtext`）按其文本阅读的方式对齐段落，如同文稿那样。
- 检查器从左到右阅读。

## 试用

`MYGO_DIRECTION=rtl` 把原生 UI 的每个窗口从右到左排版（`ltr` 则从左到右），无论应用的语言是什么，以便在界面翻译之前试用，如同 GTK 的 `GTK_DEBUG=invert-text-dir` 和 Qt 的 `-reverse`：

```sh
MYGO_DIRECTION=rtl mygo dev
```

[检查器](inspector.md)的 RTL 切换按钮在检查器运行时对其窗口做同样的事，`Tester` 在 `SetDirection(ui.RightToLeft)` 之后会把它的窗口排成从右到左，如同布局测试所做的那样：

```go
tt := ui.NewTester(app.view, 400, 300)
tt.SetDirection(ui.RightToLeft)
back, _ := tt.Find("Back")
if back.X < 200 {
	t.Error("the back button is not at the right")
}
```

[gallery](../../examples/gallery)可以通过其工具栏中的切换按钮，以从右到左的方式显示每个控件。
