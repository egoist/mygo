# 过渡

<!-- preview transitions 230 -->

`Transition` 让元素从上一帧的状态平滑过渡到当前布局指定的状态，包括在父元素中的位置、尺寸、背景色、边框色和阴影。视图只需描述目标状态，过渡负责生成中间的动画。

```go
var rowMotion = ui.ElementTransition{
	Enter: &ui.Motion{Collapse: true},
	Exit:  &ui.Motion{Collapse: true},
}

ui.Column(c).Children(func() {
	removed := -1
	for _, it := range app.items {
		ui.Row(c).Key(it.ID).Padding(8).Transition(rowMotion).Children(func() {
			ui.Text(c, it.Title).Grow(1)
			if ui.Button(c, "Remove").Clicked() {
				removed = it.ID // once the loop is done (see Views)
			}
		})
	}
	if removed >= 0 {
		app.remove(removed)
	}
})
```

新增的行展开出现，移除的行收起，其余的行滑到自己的位置，无论条目是新增、移除、排序还是打乱。

## 移动的是什么

一个 `ElementTransition` 在其 `Duration` 内做动画（为零时是 200 ms，为负时完全不花时间，见下文），沿其 `Ease`（为 nil 时用 `ui.EaseOut`，也可以是任意[缓动](drawing.md#动画)）；如果设置了 `Animation`，则按它的方式：用弹簧，例如 `ui.Snappy`，元素在途中被送往别处时会平滑转向，并保持速度（见 [Motion](motion.md)）。

- **位置**：元素在父元素中的位置。随父元素移动，例如滚动视图滚动时，不算变化。
- **大小**：它的宽度和高度。内容会在途中的每个大小上排版，就像面板打开时文字重新换行。在它的位置使用 `Width` 或 `Height`，只让其中一个做动画，如 CSS 的 `transition-property: height`：另一个会立刻跟随布局。
- **颜色**：它的背景色和边框色，例如悬停淡入淡出时。它们在 sRGB 中混合，或在 `Animation` 设置了 `Oklab` 时在 Oklab 中混合，后者混合出的中间色保持颜色的明度和饱和度，而不会经过两个饱和色之间一个暗淡、更深的中间色：`Animation: &ui.Animation{Duration: 200 * time.Millisecond, Oklab: true}`。
- **阴影**：它的 `Shadow` 和 `InsetShadow`，它们的偏移、模糊、扩散和颜色，如 CSS 过渡 `box-shadow`：每一个都从上一帧中就位的那一个过渡而来，新增的阴影从偏移为零的透明阴影过渡而来，移除的则过渡到它。内阴影取代外阴影的位置时，阴影会立刻改变。

这四类都会做动画，除非用 `Position`、`Size`（或 `Width`、`Height`）、`Colors` 或 `Shadows` 指定其中一部分：

```go
tile := ui.Box(c).Size(64, 48).Radius(8).Background(t.Surface)
if tile.Hovered() {
	tile.Background(t.SurfaceHover)
}
tile.Transition(ui.ElementTransition{Colors: true, Duration: 120 * time.Millisecond})

// A button whose resting shadow fades as the pointer comes.
b := ui.ButtonBase(c).Padding(8, 14).Radius(8).Background(t.Surface)
if !b.Hovered() {
	b.Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.2))
}
b.Transition(ui.ElementTransition{Shadows: true, Duration: 150 * time.Millisecond}).
	Children(func() { ui.Text(c, "Save") })
```

元素需要在帧与帧之间保持相同的 ID：一个 `Key`，或在一组兄弟中相同的位置。过渡只让元素自己做动画：因变化而移动的元素也需要自己的过渡才能平滑移动；要和上面的行同步移动，就要用同一个过渡；如果容纳它们的列大小也会变，列也需要过渡：

```go
open := app.sidebar
w := float32(56)
if open {
	w = 240
}
ui.Row(c).Fill().Children(func() {
	ui.Column(c).Width(w).ClipX().Transition(ui.ElementTransition{Size: true})
	ui.Column(c).Grow(1).Transition(ui.ElementTransition{})
})
```

高度跟随内容的面板，例如一个详情区域，在用户把窗格拖宽或拖窄时，只让高度做动画：它弹开，而宽度逐帧跟上拖动。

```go
ui.Row(c).Width(app.paneWidth).Children(func() { // follows a drag handle
	ui.Column(c).Key("details").Grow(1).
		Transition(ui.ElementTransition{Height: true, Animation: &ui.Snappy}).
		Children(func() {
			ui.Text(c, "Details")
			if app.open {
				ui.Text(c, app.body)
			}
		})
})
```

`Animation` 中的 `Delay` 会把变化推迟一段时间：按索引递增的延迟会让列表的行依次错开：

```go
for i, it := range app.items {
	a := ui.Snappy
	a.Delay = time.Duration(i) * 25 * time.Millisecond
	ui.Row(c.Key(it.ID)).Transition(ui.ElementTransition{Animation: &a, Enter: &ui.Motion{Y: 8}})
}
```

`Transitioning()` 报告元素的过渡在上一帧中是否在移动它。

## 出现与离开

`Enter` 是元素出现时的起点，`Exit` 是它离开时的去向，都从它在布局中的位置算起：一个 `Motion` 会按 `X` 和 `Y` 指定的 DIP 数，以及自身宽度和高度的 `XPercent`、`YPercent` 百分比移动，处于 `Opacity` 指定的不透明度（未设置时为 0，即透明），按 `Scale` 缩放（0 保持它的大小），绕 `TransformOrigin` 旋转 `Rotate` 度，按 `Blur` 指定的 DIP 模糊（与 [`Blur`](styling.md) 的模糊一样）；使用 `Collapse` 时，在列中没有高度、在行中没有宽度，内容会被裁剪。使用 `Flip` 时，附着在另一个元素上的元素（[`AttachTo`](overlays.md#你自己的浮层)）会在布局把它放在目标另一侧时，镜像 `X` 与 `XPercent`，或 `Y` 与 `YPercent`：一个从按钮下方 `Y: -8` 处降下的菜单，在只有上方有空间时，会从 8 处升起，从它显示的第一帧起就这样。

```go
toast := ui.ElementTransition{
	Enter: &ui.Motion{Y: 12},
	Exit:  &ui.Motion{Opacity: 1, X: 300},
}
popup := ui.ElementTransition{
	Animation: &ui.Snappy,
	Enter:     &ui.Motion{Scale: 0.95},
	Exit:      &ui.Motion{Scale: 0.95},
}
// Text or an icon crossing over to another through a short blur, and a
// sheet sliding in by its own height, however tall it is.
swap := ui.ElementTransition{
	Enter: &ui.Motion{Y: 4, Blur: 4},
	Exit:  &ui.Motion{Y: -4, Blur: 4},
}
sheet := ui.ElementTransition{
	Animation: &ui.Snappy,
	Enter:     &ui.Motion{Opacity: 1, YPercent: 100},
	Exit:      &ui.Motion{Opacity: 1, YPercent: 100},
}
```

元素只有在上一帧就已存在的父元素里，才会带着它的 `Enter` 出现，这样一次性显示的页面或对话框不会让它里面的每个元素都做动画。它出现期间，布局对它的移动（比如有行落在它上方）会在过渡不负责动画的方面（`Position`、`Size`、`Width`、`Height`）立即生效；改变它的大小会按比例改变它的 `Collapse`：只播放它的 `Enter`。父元素仍在、元素自己却不再被构建时，它的一个副本（如上一帧显示的那样）会在兄弟元素下方走向它的 `Exit`：它不占空间，所以兄弟元素立刻移到它的位置，如果它们有过渡就平滑移动；它既不接收指针，也不接收焦点，也不会被辅助技术读取，就像一个同时有 `IgnoreInput` 和 [`AccessHidden`](accessibility.md#不挡辅助功能的路) 的元素。副本保留元素的 [z-index](layout.md#绘制顺序)，并且只位于 z-index 相同的兄弟元素之下：用 `ZIndex` 抬起的对话框会在页面上方淡出，而不是在它下方。在退出结束前重新构建时，元素从副本所在的位置回来。

### 边走边定的离开

副本按元素最后一次构建时的 `Exit` 走。有时离开的方向要到元素走的时候才知道，比如日历、向导或轮播向前或向后翻一步：离开的页面要沿着送走它的那一步的方向离开。这时父元素的 `ChildExit` 会说明这一帧离开的子元素改往哪里走：有 `Exit` 的子元素会边走边被复制。已经在路上的副本继续按原来的方式走。

```go
dir := float32(app.dir) // 1 forward, -1 back, set as the step changes
page := ui.ElementTransition{
	Animation: &ui.Snappy,
	Enter:     &ui.Motion{X: 40 * dir},
	Exit:      &ui.Motion{X: -40 * dir},
}
ui.Box(c).Grow(1).ChildExit(ui.Motion{X: -40 * dir}).Children(func() {
	ui.Column(c.Key(app.step)).Absolute().Fill().Transition(page).Children(app.stepView)
})
```

没有它的话，在步骤掉头的那一帧离开的页面，虽然是按向前构建的，也会向前离开。

### 一次一个

离开的元素和接替它的元素会同时显示，一个穿过另一个。`AfterExits` 会让正在出现的元素等待兄弟元素的副本离开：它先在布局中不可见地占住自己的位置，等它们走完再带着 `Enter` 出现，这样它的父元素一次只显示一个，例如一个面板走完另一个才出现：

```go
panel := ui.ElementTransition{
	Enter:      &ui.Motion{Y: 8},
	Exit:       &ui.Motion{Y: -8},
	AfterExits: true,
}
ui.Box(c).Grow(1).Children(func() {
	ui.Column(c.Key(app.tab)).Absolute().Fill().Transition(panel).Children(app.tabView)
})
```

如果它在显示之前就消失了，这个被推迟的元素不会留下任何痕迹。

### 给退出定时

`Exit` 和其他变化一样进行，除非 `ExitDuration` 或 `ExitEase` 另有说明：一个弹层进入用 150 ms、离开用 100 ms，就像 CSS 的 `transition-duration` 在两个状态之间不同。单独设置时，`ExitDuration` 保留 `Ease`，`ExitEase` 保留 `Duration`；设置任何一个都会让弹簧的退出（`Animation`）变成曲线，`ExitDuration` 为零时长度大约和弹簧一样，在 `Animation` 的 `Delay` 之后开始：

```go
popup := ui.ElementTransition{
	Duration:     150 * time.Millisecond,
	ExitDuration: 100 * time.Millisecond,
	ExitEase:     ui.EaseIn,
	Enter:        &ui.Motion{Scale: 0.96},
	Exit:         &ui.Motion{Scale: 0.96},
}
```

`ExitDelay` 是 `Exit` 出发前等待多久，就像 CSS 的 `transition-delay` 在两个状态之间不同。为零保留 `Animation` 的 `Delay`，和其他变化一样等待；负值不等待，所以一个接一个进入、每个 `Delay` 更长的行会一起离开：

```go
row := ui.ElementTransition{
	Animation: &ui.Animation{Duration: 200 * time.Millisecond, Ease: ui.EaseOut, Delay: 80 * time.Millisecond},
	ExitDelay: -1,
	Enter:     &ui.Motion{Y: 8},
	Exit:      &ui.Motion{Y: 8},
}
```

负的 `Duration` 完全不花时间：每个变化立刻显示，`Enter` 和 `Exit` 也一样，元素离开时不留下副本，就像自带减少动效设置的应用那样；负的 `ExitDuration` 只对 `Exit` 这样做。为零的 `Duration` 是 200 ms：

```go
if app.settings.ReduceMotion {
	popup.Duration, popup.ExitDuration = -1, -1
}
```

## 共享元素

`Match` 给元素一个与另一个元素共享的身份，例如网格中照片的缩略图和详情视图中的照片。一个带 `Match` 的元素出现在上一帧中带有相同 `Match` 的元素所在的位置时，就从那里出现，采用那一个的大小和颜色，即使来自另一个父元素，就像 SwiftUI 的 `matchedGeometryEffect` 和 Motion 的 `layoutId`：

```go
type photoMatch struct{ id int }

if app.open == nil {
	for _, p := range app.photos {
		thumb := ui.Image(c.Key(p.ID), p.Thumb).Size(64, 64).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Match: photoMatch{p.ID}})
		if thumb.Clicked() {
			app.open = p
		}
	}
} else {
	ui.Image(c.Key("hero"), app.open.Full).Size(320, 240).
		Transition(ui.ElementTransition{Animation: &ui.Snappy, Match: photoMatch{app.open.ID}})
}
```

用你自己的类型作为身份，避免与其他元素的身份相同。出现的元素会从另一个的盒子走到自己的盒子，内容在途中的每个大小上排版（如 `Size` 排版那样），所以里面的东西不会拉伸，圆角也不例外。一次只给一个元素 `Match`；离开的那个按它自己的 `Exit` 走，如果有的话。

### 飞行

`Fly` 让出现的元素从另一个那里飞过来，就像 Keynote 的 Magic Move 和 Flutter 的 Hero：

```go
tr := ui.ElementTransition{Animation: &ui.Snappy, Match: photoMatch{p.ID}, Fly: true}
```

在途中，出现的元素：

- 从另一个的盒子走到自己的盒子，内容在每个大小上排版（或用下面的 `ScaleContent` 缩放）；
- 从另一个的外观变到自己的：它的背景色和边框色、圆角、不透明度、文字颜色，以及它的变换，沿较短的方向旋转，在 3D 和透视中也是如此，就像 CSS 插值变换那样；还会从另一个周围元素的变换（例如一个被缩放的容器）过渡到它周围元素的变换；
- 画在窗口其余部分之上，或它所在的浮层之上（例如对话框），在它之后的浮层之下，并在它绘制的地方接受指针：脱离它周围滚动视图和容器的裁剪，盖在它之后的元素上，和它自身一样不透明，无论周围元素的不透明度如何，并使用它周围 `Themed` 的主题；
- 把它的内容作为一个整体平滑移动，而不是让文本一行一行地逐像素移动，到达之后就像静止的文本那样；
- 如果它来自的那个元素也有 `Fly`，就在它上面淡入，那个元素随它一起走，也在每个盒子上排版，并淡出：来的那个走到一半时已经完全不透明，另一个在后半段淡出，如 Motion 的交叉淡入淡出。从视图中消失时，那个元素会作为自身的一个副本继续走，而不是按它自己的 `Exit` 说的走；仍在构建时，例如在一个正在离开的页面里，它自己走。

它在途中随周围的元素一起被移除时，例如它的视图带着 `Exit` 关闭，元素就在它们的副本中继续飞：从它显示的位置到那个副本让它到的位置，随副本一起淡出。

### 其余一切

Keynote 的 Magic Move 在共享的东西移动时，让幻灯片不共享的东西淡出。要在两个视图之间做到这一点：在一个保持存在的父元素里给每个视图一个 `Key`，并配上淡入的 `Enter` 和淡出的 `Exit`：两个视图共享的东西从一个飞到另一个，其余的先淡出再淡入。离开的视图的副本不显示任何与另一个视图一起飞行的元素。

```go
fade := ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{}, Exit: &ui.Motion{}}
ui.Box(c.Key("stage")).Grow(1).Children(func() {
	if app.open == nil {
		ui.Column(c.Key("grid")).Absolute().Fill().Transition(fade).Children(app.grid)
	} else {
		ui.Column(c.Key("detail")).Absolute().Fill().Transition(fade).Children(app.detail)
	}
})
```

[`Router`](navigation.md#过渡) 用 `TransitionMatch` 为它的页面做到这一点。

### 缩放的内容

`ScaleContent` 通过从元素自己的大小缩放来表现它的大小变化，内容只排版一次，而不是在每个大小上排版：文本保持行数，随元素一起放大或缩小，例如一个小标题变成大标题，图片则随它的盒子拉伸。它会在 `Match` 的元素移动途中缩放它，也会缩放 `Size` 的变化。

### 变形的文字

一个带着 `Match` 和 `ContentTransition{Morph: true}` 飞行的文本，会从它 `Match` 的那个元素的文本变形而来：两者都有的字符从原来的位置移到现在的位置，放大或缩小到自己的大小，并变成自己的颜色；其他字符淡出，随文字一起走，再淡入，就像 Keynote 的 Magic Move 按字符进行。它的盒子像任何 `Match` 元素一样飞行，它的文字在自身大小上排版一次，然后移进盒子里。在它到达之前被接管时——被它来自的元素或它的 `Match` 的另一个元素——它会从字符所在的位置继续变形。富文本也会变形：它的字符会变成所在文本段的颜色，下划线、删除线和高亮则淡出再淡入：

```go
type titleMatch struct{ id int }
title := ui.ElementTransition{Animation: &ui.Snappy, Match: titleMatch{p.ID}, Fly: true}
morph := ui.ContentTransition{Morph: true}

// In the grid.
ui.Text(c, p.Title).FontSize(12).Transition(title).ContentTransition(morph)
// In the detail view.
ui.Text(c, p.Title+" in "+p.Place).FontSize(24).Bold().Transition(title).ContentTransition(morph)
```

`Morph` 也会让原地变化的文本变形（见 [Motion](motion.md#变化的内容)）。

## 什么都不动的时候

改变窗口大小引起的变化不做动画。当桌面要求更少的动效（`c.Preferences().ReduceMotion`）时，什么都不动：元素立刻显示在布局放置它们的地方，共享元素也不会飞行。颜色和阴影仍然平滑改变；一个只淡入淡出或模糊、而不收起的 `Enter` 或 `Exit` 会就地淡入淡出、模糊，正如 Apple 的指南所建议的；会收起的那个则会立刻完成，因为它会移动兄弟元素。只有在有东西移动时才会绘制帧。

[`List`](list.md) 的行由列表放置，列表只构建视口中的那些：它们的内容可以有过渡，但行本身不会随之滑动、出现或离开。
