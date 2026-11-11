# 动效

<!-- preview motion 220 -->

元素可以自动对数值进行动画插值：给定目标值后，它会返回当前帧的值，并持续绘制，
直到到达目标。视图只需指定目标状态，动画负责平滑过渡：

```go
card := ui.Box(c).Size(120, 80).Radius(12).Background(t.Accent)
target := float32(0)
if app.open {
	target = 200
}
card.Translate(card.AnimateTo("x", target, ui.Snappy), 0)
```

键（`"x"`）用来区分同一个元素的多个值，这个元素需要逐帧保持相同的 ID：一个
`Key`，或它在兄弟中的同一个位置。值从它的第一个目标开始。这个值可以以任何
方式使用：变换（见[变换](transforms.md)）、尺寸、不透明度、颜色混合，或
`Draw` 绘制的内容。

## 动画

`Animation` 说明值如何前往新目标：

- **弹簧**，即零值 `Animation` 以及预设 `ui.Smooth`（不回弹）、
  `ui.Snappy`（轻微回弹）和 `ui.Bouncy`（明显回弹）。弹簧在途中目标改变时
  保留其速度：在飞行途中再次点击，上面的卡片会平滑地掉头，而不是骤然停住。
- **曲线**：一个 `Duration` 和一个 `Ease`（为 nil 时用 `ui.EaseOut`），如
  `ui.Animation{Duration: 150 * time.Millisecond, Ease: ui.EaseInOut}`。
  `Animate(key, target, d)` 和 `AnimateWith(key, target, d, ease)` 是它们的
  简写。

弹簧的 `SpringOptions` 按人的感知描述它，带有 `Duration`（为零时是 500 ms），
大致是它到达所需的时间，以及 `Bounce`，从 0（到达且不越过）趋向 1；负值到达
得更慢。这些是 SwiftUI 和 Motion 的参数。`Stiffness`、`Damping` 和 `Mass`
则从物理上描述它：

```go
gentle := ui.Animation{Spring: ui.SpringOptions{Duration: 700 * time.Millisecond, Bounce: 0.1}}
stiff := ui.Animation{Spring: ui.SpringOptions{Stiffness: 400, Damping: 30}}
x := card.AnimateTo("x", target, gentle)
```

`Spring(key, target, options)` 是带弹簧的 `AnimateTo` 的简写。弹簧是精确
求解的，而不是逐步推进，所以慢帧不会改变它们的路径。值可以在途中改变动画
种类：弹簧接管曲线时会保留曲线的速度。

`Easing` 函数有 `ui.Linear`、`ui.EaseIn`、`ui.EaseOut`、`ui.EaseInOut`、
`ui.Bounce(e)`、CSS 的 `ui.CubicBezier(x1, y1, x2, y2)`，或任何
`func(t float32) float32`。

`AnimateColor(key, target, a)` 以同样的方式为颜色做动画，用预乘 alpha 混合，
这样从透明淡入的颜色能保持色相：

```go
row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
```

颜色在 sRGB 中混合，除非 `Animation` 有 `Oklab`：那就在 Oklab 中混合，如
渐变的 `Oklab` 混合那样，它在两种饱和颜色之间的路径保持它们的明度和饱和度，
而不是经过一个暗淡、更深的中间色（黄色到蓝色会经过一些蓝色，而不是灰色）。
[过渡](transitions.md#移动的是什么)的颜色也用它来混合，`ContentTransition`
变形中的颜色也是如此，`Color.MixOklab` 则这样混合两种颜色：

```go
badge.Background(badge.AnimateColor("bg", level, ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond}, Oklab: true}))
```

## 只绘制的姿态

上面的值由视图读取，所以每个移动的帧都会运行视图。`AnimatePose` 则移动元素：
在它自己的变换之后平移、缩放、旋转和淡出它，让它移动的帧只绘制，不运行视图，
就像 Core Animation 和 Web Animations API 处理变换和不透明度那样。这是最便宜
的动效，能让大型视图保持流畅：

```go
lift := ui.Pose{}
if card.Hovered() {
	lift = ui.Pose{Y: -4, Scale: 1.02}
}
card.AnimatePose(lift, ui.Snappy)
```

`Pose` 通过 `X` 和 `Y` 移动，通过 `XPercent` 和 `YPercent` 按元素自身宽高的
百分比移动（就像 [`TranslatePercent`](transforms.md) 那样，会随元素尺寸变化
而调整），通过 `Scale` 缩放（0 保持尺寸），按 `Rotate` 指定的角度旋转，通过
`Fade` 淡出，从 0（不透明）到 1（消失）。元素移动时，指针、文本光标和浮层都
会跟着它，`Transitioning()` 报告它的姿态是否在移动。

姿态从元素第一次被赋予姿态时的位置开始：上一帧没有设置姿态的元素（如第一次
构建的元素）会立刻显示在它的姿态位置，只有之后的帧把它摆到别处时才移动。要
从一个姿态进入，如浮出面板出现时升起并淡入，就在一次构建中把元素摆在那里，
用 `c.Invalidate()` 请求另一帧，然后在下一帧把它摆到它的位置：

```go
panel := ui.Column(c)
entered := ui.Local(panel, "entered", func() bool { return false })
p := ui.Pose{}
if !*entered {
	p, *entered = ui.Pose{Y: 8, Fade: 1}, true
	c.Invalidate()
}
panel.AnimatePose(p, ui.Snappy)
```

没有第二次构建，元素会停留在第一次姿态放置它的地方。出现在上一帧已显示的
父元素中的元素，也会带着其[过渡](transitions.md#出现与离开)的 `Enter`
进入，如添加到列表中的一行。

姿态可以嵌套：每个姿态都在周围元素的姿态之后生效，所以一个元素内部的元素会
沿自己的曲线添加自己的动效。在[彩纸示例](../../examples/confetti)中，一团
彩纸落下并淡出，其中的每一片沿一条减速的曲线迸出，并在内部旋转：数百片飞舞
而无需再次构建视图。每个被赋予姿态的元素在绘制它的每一帧仍有开销，所以移动
方式相同的元素可以共享一个姿态（如整团的下落），而不是每个都用自己的。

## 延迟与错开

`Delay` 在目标改变后让值保持原来的走势一段时间。随索引增长的延迟会让列表
依次错开：

```go
for i, it := range app.items {
	row := ui.Row(c.Key(it.ID))
	a := ui.Snappy
	a.Delay = time.Duration(i) * 30 * time.Millisecond
	row.Opacity(row.AnimateTo("shown", 1, a))
}
```

## 手势

`Draggable` 让指针可以移动并抛出元素，如 motion.dev 的 `drag` 所做：元素
跟随指针，一旦松开，指针的速度会带着它去往停下的位置，用弹簧：

```go
card := ui.Box(c).Size(90, 60).Radius(10).Background(t.Accent)
card.Draggable(&app.card, ui.DraggableOptions{
	Axis:   ui.Horizontal,
	Bounds: &ui.Rect{W: 200},
	Snap:   ui.Nearest(ui.Point{}, ui.Point{X: 200}),
})
```

`app.card` 是一个 `ui.Point`，即元素相对其在布局中位置的偏移。指针拖动元素
时，它跟随指针；指针松开后，它就是元素停下的偏移；设置它就能把元素移到
那里，就像按钮翻页那样。如果偏移指针为 nil，偏移就由元素自己保存。选项说明
它如何移动：

- **`Axis`**，`ui.Horizontal` 或 `ui.Vertical`，是它移动的方向；为零值时两个
  方向都可以。
- **`Bounds`** 让偏移在静止时保持在一个矩形内，从 `X` 到 `X+W`、从 `Y` 到
  `Y+H`。指针把元素拉过边界时，拉得越远，能移动的距离越小，就像 iOS 对待
  被拉过边缘的内容那样；松开后它会弹回边界内。`Rigid` 让它在边界处停住。
- **`Snap`** 决定它在哪里停下，依据抛出会把它带到哪里（`ui.Project`，限制在
  边界内），这样一次轻扫会继续到下一页，或下一个高度：`ui.Nearest(points...)`
  选择最近的点。如果 `Snap` 把点原样返回，元素就会滑到抛出带它去的地方。
  没有 `Snap` 时，它停在指针松开的地方，如放在画布上的一项。
- **`Animation`** 是它如何停下，以及如何前往应用设置的偏移：为零时是
  `ui.Smooth`。弹簧从指针的速度开始。

指针移动几个 DIP 后才会开始拖动元素，那次按下不算点击；元素移动时按在它
上面，就能把它接住。元素在自身的变换和姿态之后移动，沿着父元素的轴，所以它
在缩放或旋转时仍留在指针下，并且会在只绘制的帧中停下，不构建视图，就像
`AnimatePose` 移动它那样。`Dragging()` 报告指针是否正在拖动它，以便在拖动时把它抬起：

```go
lift := ui.Pose{}
if card.Dragging() {
	lift = ui.Pose{Scale: 1.05}
}
card.AnimatePose(lift, ui.Snappy)
```

一旦它停下，`Transitioning()` 为 false，好让视图继续处理，比如移除被滑走的一行：

```go
for i := range app.rows {
	r := &app.rows[i]
	row := ui.Row(c).Key(r.ID)
	row.Draggable(&r.swipe, ui.DraggableOptions{
		Axis: ui.Horizontal,
		Snap: func(p ui.Point) ui.Point {
			if p.X < -160 {
				return ui.Point{X: -400} // away to the left
			}
			return ui.Point{}
		},
	})
	if r.swipe.X == -400 && !row.Transitioning() {
		app.remove(r.ID)
	}
}
```

`DragOffset()` 返回元素在这一帧显示的位置，跟随指针或在停下的途中，供跟随
它的东西使用，如随表单下滑而淡出的背景。读取它会在元素移动的每一帧中构建
视图，就像 `AnimateTo` 那样；要在 `Draggable` 之后调用它：

```go
backdrop := ui.Box(c).Absolute().Fill().Background(ui.RGBA(0, 0, 0, 0.4))
sheet := ui.Column(c).Draggable(&app.sheet, ui.DraggableOptions{
	Axis:   ui.Vertical,
	Bounds: &ui.Rect{H: 400},
	Snap:   ui.Nearest(ui.Point{}, ui.Point{Y: 400}),
})
backdrop.Opacity(1 - sheet.DragOffset().Y/400)
```

当桌面要求减少动效时，元素跟随指针，并立即停下。

### 手动实现

对于除跟随指针的偏移之外的值，如拨盘的角度，要用更小的部件构建手势。`Jump`
立即设置值，跟随指针，并记录它移动得多快；指针松开后接管的弹簧从那个速度
开始。`DragEnded` 报告指针松开时的速度，`SpringOptions.Velocity` 把它交给
弹簧：

```go
dial := ui.Box(c).Size(80, 80)
var turn float32
if dx, _, ok := dial.Dragged(); ok {
	app.turn += dx
	turn = dial.Jump("turn", app.turn)
} else {
	a := ui.Snappy
	if vx, _, ok := dial.DragEnded(); ok {
		// Where the throw would carry it decides the notch it lands on.
		app.turn = float32(math.Round(float64(ui.Project(app.turn, vx)/45))) * 45
		a.Spring.Velocity = vx
	}
	turn = dial.AnimateTo("turn", app.turn, a)
}
dial.Transform(ui.Rotation(turn))
```

一次拖动的最后几次移动会在 `Dragged` 仍报告按下时到来，而 `DragEnded` 在
之后的一次构建中到来：拨盘先转到指针所到的位置，然后弹到一个刻度。

`DragVelocity` 是指针按下时的速度，以沿元素各轴每秒的 DIP 计，停留片刻后为
零。`Velocity(key)` 是正在做动画的值的速度。不移动的点击不会结束拖动。

`ui.Project(value, velocity)` 是快速抛出会把值带到的地方，按 iOS 滚动视图的
减速方式计算，用来选择它落在哪里，如分页器的一页。
`ui.Rubberband(value, lo, hi, dimension)` 让被拖过 lo 或 hi 的值保持弹性，如
iOS 对待被拉过边缘的内容：拖得越远，能移动的距离越小：

```go
if _, dy, ok := sheet.Dragged(); ok {
	app.y += dy
	y = sheet.Jump("y", ui.Rubberband(app.y, 0, 400, 600))
}
```

## 完成时

`Settled(key)` 报告值是否停在它的目标处，`Arrived(key)` 在它停下的那一帧为
true，好让视图继续处理，比如移除淡出的内容：

```go
toast.Opacity(toast.AnimateTo("fade", 0, ui.Animation{Duration: 200 * time.Millisecond}))
if toast.Arrived("fade") {
	app.dismiss(toast)
}
```

## 不使用动画

`c.DisableAnimations()` 让这一帧的值、过渡、姿态和路由页面立即到达目标，如
SwiftUI 的 `disablesAnimations`：用于不应显示为动效的变化，如恢复窗口状态或
跳到搜索结果。在构建会变化的内容之前调用它。`Keyframes` 和时间线自行播放，
而不是朝向目标，并按原样继续；要跳转时间线，用 `Seek`。`c.Instant()` 报告这
一帧是否如此，供自己移动值的视图使用，好让它在相同的帧中停下。

## 关键帧

`Keyframes` 播放带时间的值，每个区段沿它到达的那一关键帧的 `Ease` 进行，并
保持最后一个：

```go
tile := ui.Box(c).Key("tile").Size(80, 50)
x := tile.Keyframes("x", []ui.Keyframe{
	{Value: 0},
	{At: 200 * time.Millisecond, Value: 100, Ease: ui.EaseOut},
	{At: 700 * time.Millisecond, Value: 40},
	{At: time.Second, Value: 0, Ease: ui.EaseInOut},
}, ui.KeyframeOptions{Run: app.shakes})
tile.Translate(x, 0)
```

第一个时间必须为零，之后的时间必须递增。改变 `Run` 可以再次播放序列，或播放
改变后的序列：被中断后重新播放时，它从当前显示的值开始，而不是像第一次播放
那样跳到第一个值。`Repeat` 是它在第一次之后再次播放的次数，`RepeatForever` 表示只要
它被构建就一直播放，`Alternate` 让每隔一次反向播放；`Delay` 在播放前保持
第一个值一段时间，`RepeatDelay` 在两次播放之间保持它到达的值。

## 时间线

`Timeline` 沿一个播放头播放多个补间，所以属于同一组的动效可以一起安排、控制
和测试：卡片到达时标题淡入、各行依次跟随、卡片落地后的小回弹。`AnimateTo`
把一个元素的一个值移向目标，`Keyframes` 沿带时间的停靠点播放一个值；时间线
围绕同一个时钟协调许多这样的值，每个补间各管各的。

元素会像保存 `Keyframes` 一样，在一个键下保存时间线：在视图中构建它，如其他
一切，并给每个补间它这一帧要显示的值：

```go
card := ui.Column(c)
tl := card.Timeline("intro", ui.TimelineOptions{Run: app.opened})
x := tl.Tween(ui.Tween{From: -200, To: 0, Duration: 400 * time.Millisecond})
fade := tl.Tween(ui.Tween{From: 0, To: 1, Duration: 300 * time.Millisecond},
	ui.AfterPrevious(-150*time.Millisecond))
card.Translate(x, 0).Opacity(fade)
```

补间默认缓出（`EaseOut`），除非它的 `Ease` 另有说明，和 `Animation` 一样。
没有指定放置方式的补间会紧接着前一个开始，所以这里第二个会在 400ms 开始；
放置方式可以把它放到别处，就像 GSAP 的位置参数那样：

| 放置方式 | 补间开始于 |
| --- | --- |
| `AfterPrevious(50*time.Millisecond)` | 前一个结束后的 50ms；负偏移与它重叠，如上面的 `-150ms` 让淡入在 250ms 开始 |
| `WithPrevious(0)` | 前一个开始的地方，与它一起移动，或在那之后的一个偏移 |
| `AtLabel("rows", 100*time.Millisecond)` | 某个标签之后的 100ms |
| `AtTime(500*time.Millisecond)` | 时间线的某个时间 |

任何补间都不会早于时间线开始：早于开始的放置会落在开始处。

### 标签与错开

`Label` 在时间线中标记一个时间点，供补间从此开始；在标签的时间上加上偏移，
就能按索引把它们错开：

```go
tl := list.Timeline("rows", ui.TimelineOptions{Run: app.shown})
tl.Label("rows", ui.AtTime(300*time.Millisecond))
for i := range app.rows {
	app.rows[i] = tl.Tween(ui.Tween{From: 0, To: 1, Duration: 200 * time.Millisecond},
		ui.AtLabel("rows", time.Duration(i)*40*time.Millisecond))
}
for i, shown := range app.rows {
	rows[i].Opacity(shown).Translate(0, (1-shown)*12)
}
```

### 播放

`Duration` 是最后一个补间的结束，`Time` 是播放头所在的位置，`Progress` 是它
的进度，从 0 到 1，供拖动进度条使用。`Paused` 把播放头保持在原处：时间线
暂停期间花掉的时间，不算它播放过的时间，所以解除暂停后，它会从停下的地方
继续播放，无论暂停了多久。`Reverse` 从播放头所在处反向播放到它的开始，如
一个按打开时相反方向关闭的面板（`Reverse: !app.open`）；在反向状态下被重新
播放的时间线会从末尾开始。`Seek` 和 `Progress` 在给出期间把时间线保持在某个
时间或某个进度，不给它们时则从那里继续；最多给出其中一个。选项的内容在设置
它的那一帧就生效，所以暂停或再次播放时间线的按钮，在点击的那一帧就显示出
效果：

```go
o := ui.TimelineOptions{Run: app.run, Paused: app.paused, Reverse: app.reverse}
if app.scrubbing {
	o.Progress = &app.scrub
}
tl := card.Timeline("intro", o)
```

`Repeat` 是时间线在第一次之后再次播放的次数，和 `KeyframeOptions.Repeat`
一样，`RepeatForever` 表示只要视图构建它就播放，`Alternate` 让每隔一次反向
播放；`Delay` 在播放前把时间线保持在它的第一个值一段时间，`RepeatDelay` 在
两次播放之间保持它到达的值。`Settled` 报告它已停下，在它的末尾，或反向时在它的
开始；`Arrived` 在它停下的那一帧为 true，如 `Settled(key)` 和 `Arrived(key)`
之于值。

改变 `Run` 可以再次播放时间线，或播放改变后的时间线。构建它时如果不改变
`Run`，播放头会保持在原处，所以视图可以逐帧重新安排它的补间：时间线播放它
这一帧安排的补间。如果安排的补间更长，它会从播放头所在处继续，在安排的时间线
播放完之前不会 `Settled`。视图停止构建元素时，时间线随元素的状态一起消失。
元素每次构建时，对它的每条时间线只会请求一次，它返回的 `*Timeline` 在视图
构建那一帧期间有效。

当桌面要求减少动效时，本会播放的时间线会立即处于它将停下的位置，每个补间
都停在 `To`，如果它反向播放到起点，则停在 `From`，和值、姿态与 `Keyframes`
一样；并且它不请求帧，除非它的 `NonMotion` 说明它只做淡入淡出（见
[减少动效](#减少动效与帧)）。视图自己对播放头做的操作仍然有效，如拖动仍会
移动 `Draggable` 元素：`Seek` 和 `Progress` 把它放到拖动进度条要求的地方，
`Paused` 把它保持在那里。`DisableAnimations` 不改变时间线，它像
`Keyframes` 一样自行播放；要跳转就用 `Seek`。时间线只在播放时绘制帧，由
`c.Now()` 计时。

## 滚动

`InView()` 报告元素是否有一部分在上一帧中显示在它周围的滚动容器内，以便在
它滚动进入视野时把它带入。当它依据的状态变化时，比如滚动把元素带入视野，
MyGo 会构建另一帧，这样视图无需等待下一次输入就能读到新答案；`InViewMargin`
和 `InViewAmount` 也一样，不读取它们任何一个的元素不消耗帧。`ScrollState`
（见[滚动](scroll.md)）给出它的容器滚动了多远，用于跟随滚动的效果：

```go
card := ui.Box(c).Key(it.ID).Height(120)
shown := float32(0)
if card.InView() {
	shown = 1
}
card.Opacity(card.AnimateTo("shown", shown, ui.Smooth))

header.Opacity(1 - min(app.scroll.Y/80, 1)) // fades as the page scrolls
```

`InViewMargin(margin...)` 在查看之前把窗口和元素周围的滚动容器扩大 `margin`
个 DIP，如 IntersectionObserver 的 `rootMargin` 和 `scrollMargin` 所做
（Motion 的 `margin`）：写法与 CSS 的 `Padding` 一样，所有边、垂直和水平，或
上、右、下、左。缩略图在滚入前 300 DIP 开始加载，这样它永远不会显示空白：

```go
thumb := ui.Image(c, placeholder).Key(it.ID).Size(160, 120)
if thumb.InViewMargin(300) {
	app.load(it.ID) // once; the next frames show the picture
}
```

负的边距则缩小它们，让元素只有在进入视口那么深之后才算进入视野。它周围的
其他裁剪（`Clip`、`ClipInset`）会按自己的方式裁剪它，不受边距影响。没有边距
的 `InViewMargin()` 就是 `InView()`。`List` 或 `GridView` 只构建靠近其边缘的
行，每边多两行，所以在其中边距不会超出它构建的行。

`InViewAmount(amount, margin...)` 等到元素的这一比例面积（从 0 到 1）显示
出来，如 IntersectionObserver 的 `threshold` 和 Motion 的 `amount` 所做：
一张卡片显示一半才算一次曝光，而不是它在列表底部露头时就算；动画则在它显示
五分之一时开始。`1` 等待全部显示，`0` 等同于 `InViewMargin`；边距和
`InViewMargin` 一样会扩大窗口与滚动容器。它的面积按变换移动后的盒子计算：

```go
card := ui.Column(c).Key(ad.ID)
if card.InViewAmount(0.5) {
	app.counted(ad.ID)
}
```

`ScrollState.ScrollTo(x, y, animation)` 平滑滚动一个容器，带 `Animation`
选项的 `ScrollIntoView` 滚动元素周围的容器（见[滚动](scroll.md)）。

## 变化的内容

`ContentTransition` 为元素内容的变化做动画：`Text` 的文本、`Icon` 的图标、
`Image` 的图片。默认情况下旧内容淡出、新内容淡入，图标缩走而新图标长入。
`Numeric` 让一行文本中变化的字符滚动，如计数器的数字，不变的字符则保持
不动：向上，或带 `Down` 时向下，如 SwiftUI 的 numericText：

```go
ui.Text(c, strconv.Itoa(app.count)).FontSize(32).Bold().
	ContentTransition(ui.ContentTransition{Numeric: true, Down: app.decreased})
ui.Icon(c, playOrPause).ContentTransition(ui.ContentTransition{})
```

`Morph` 把新旧文本共有的字符从它们原来的位置移到它们现在的位置，只属于旧
文本的字符淡出，只属于新文本的字符淡入，如 Keynote 的 Magic Move 按字符
进行：词语重排、标题改写、字母重新排列。富文本也会变形，它的字符会变成所在
文本段的颜色。用 [`Match`](transitions.md#变形的文字) 飞行的文本，会从它
`Match` 的那个元素的文本变形而来。

```go
ui.Text(c, app.title).ContentTransition(ui.ContentTransition{Morph: true})
```

内容过渡在只绘制的帧中运行。当桌面要求减少动效时，内容淡入淡出而不移动。

## 循环与自己的动效

`Loop` 返回每个周期重新开始的动画的进度，用于加载指示器和脉冲（见
[绘制与动画](drawing.md#动画)）。要以其他方式动画，从 `c.Now()` 计算，
并在每个移动的帧中调用 `c.AnimationFrame()`。

## 减少动效与帧

当桌面要求减少动效时（`c.Preferences().ReduceMotion`），值和姿态立即到达
目标，序列停在它们的最后一个值。不移动而变化的东西仍然平滑变化：颜色
（`AnimateColor`）、姿态的 `Fade`、内容交叉淡入淡出，以及
[过渡](transitions.md)的颜色和淡入淡出。`Loop` 继续，如系统的加载指示器
那样。

一个值是否移动什么，由视图自己说明。不移动任何东西的值（如不透明度、模糊或
视图自己驱动的交叉淡入淡出）在其 `Animation`、`KeyframeOptions` 或
`TimelineOptions` 上用 `NonMotion` 说明，这样在减少动效时它仍然会有动画，如
元素自己的淡入淡出那样：

```go
op := e.AnimateTo("fade", target, ui.Animation{Duration: 200 * time.Millisecond, NonMotion: true})
ui.Image(c, first).Opacity(1 - op)
ui.Image(c, second).Opacity(op)
```

对于位置、尺寸、缩放或滚动，不要设置它，并给会移动的部分单独配一条时间线，
与淡入淡出的时间线并列，它会在减少动效时立即停到终点。

只有在有东西移动时，动画才会绘制帧，按显示器的速率；窗口不在视线内时（见
下文）则不绘制。移动视图读取的值的帧会运行视图；姿态、内容过渡以及从 `Draw` 动画的
绘制只绘制。

## 不在视线内

当窗口没有任何部分显示时（macOS 上：隐藏、最小化、在另一个 Space 上，或被
其他窗口完全遮住；浏览器中：后台标签页；Windows 上以及 X11 下的 Linux：
最小化），MyGo 不会为它绘制帧，如浏览器不为不在视线内的页面绘制任何东西，
也如 Apple 对 Mac 应用的要求。视图不运行，`Draw` 不被调用，`After` 和
`Painter.After` 请求的帧会等待。只要窗口有一部分显示，它就绘制一次，带上
期间变化的一切；在那之前，屏幕显示它的最后一帧，大约一个刷新周期。输入之后
（输入到被其他应用窗口遮住的键窗口中的按键）、辅助功能打开时，以及为
`Window.CapturePage`，仍会构建一帧，好让捕获显示窗口现在的样子。Windows 和
X11 只告诉 MyGo 窗口最小化了，别的不说，Wayland 什么都不说，所以被其他窗口
遮住的窗口，或在另一个桌面上的窗口，仍像以往一样在那里绘制。

这对动效意味着：

- **MyGo 做的动画会按时间继续到相应位置。** 窗口离开视线时正在运行的弹簧、
  途中的 `Animate`、`Loop`、关键帧和时间线，在窗口重新显示时已经走得更远，
  或已经完成。
- **在视线外改变的目标会在窗口显示时动画。** 视图到那时才第一次看到新目标，
  所以值会从它原来所在处移向它，就像 CSS 过渡处理在隐藏标签页中做出的更改
  那样。
- **从时间而不是帧来计算自己的动效。** 在绘制东西的第一帧取的开始时间，或
  帧的计数，会在窗口显示时重新开始，并把期间发生的事当作刚开始一样播放。
  在改变状态的代码里，取状态改变那一刻的时间，视图就会把在视线外做出的改变
  显示为已完成：

```go
// counter eases from one value to the next over 400 ms, by the clock.
type counter struct {
	from, to float32
	changed  time.Time
}

const ease = 400 * time.Millisecond

// at returns the value shown at t.
func (n counter) at(t time.Time) float32 {
	p := min(float32(t.Sub(n.changed))/float32(ease), 1)
	return n.from + (n.to-n.from)*ui.EaseOut(p)
}

// Where the count changes, outside the view:
now := time.Now()
app.count = counter{from: app.count.at(now), to: float32(n), changed: now}
win.Update(func() {})

// In the view:
value := app.count.at(c.Now())
if c.Now().Sub(app.count.changed) < ease {
	c.AnimationFrame()
}
```

- **让工作远离视图。** 从视图、从 `After` 或从 `Draw` 进行轮询、保存或启动
  任务，会在窗口不在视线内时等待。要从应用（goroutine 或 `time.Ticker`）运行
  这样的工作，并用 `Window.Update` 显示它所做的。
