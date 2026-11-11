# Loading

loading 插件为原生 UI 提供 [loading.dev](https://loading.dev) 的 29 个加载指示器，即 Jakub Krehel 为 React 编写的 `loading-dev`：弧、环、点、条和方块，全部由 MyGo 自己绘制，每个都是显示时会移动的元素。它们的外观和运动方式与 loading.dev 的一致：相同的形状、关键帧和时间函数，相同的节奏。全部用 Go 写成：没有 JavaScript 包，也不需要 `mygo.Use`。页面有 CSS，可以直接使用 `loading-dev` 本身。

[examples/loading](../../examples/loading) 展示了全部指示器及其选项：

<!-- app loading 480 -->

```go
import "github.com/egoist/mygo/plugins/loading"

ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
	loading.Arc(c).Label("Saving")
	ui.Text(c, "Saving…")
})
```

## 指示器

| 函数 | 绘制的内容 |
| --- | --- |
| `Arc` | 在圆中转动的一条开口描边。 |
| `Atom` | 在圆内翻滚的三个环。 |
| `Blocks` | 九个方块扫过网格，缩小又放大。 |
| `BouncingDots` | 三个错开的点上下弹跳。 |
| `Cascade` | 三条嵌套的弧展开成螺旋，然后瞬间收回成一线。 |
| `CircularDots` | 环上的八个点，最亮的那个绕圈。 |
| `Classic` | 十二根以放射状排列、逐渐淡出的条。 |
| `ClassicV2` | 环上的八个刻度依次点亮。 |
| `Clock` | 一根指针在暗淡的表盘上扫过。 |
| `Comet` | 一个完整的环淡出成它的尾迹。 |
| `Compass` | 四个刻度每次转动四分之一圈。 |
| `Dual` | 两条弧朝相反方向转动。 |
| `Eclipse` | 两个点交换位置，一个从另一个后面经过。 |
| `Flip` | 一个方块先绕一条轴翻转，再绕另一条。 |
| `Gather` | 四个方块聚拢、转动，再散开。 |
| `Leap` | 一排三个点，最后一个跳到最前面。 |
| `LinearDots` | 三个点从左到右依次点亮。 |
| `Loading` | loading.dev 的标志，其最亮的一段绕环转动。 |
| `Morph` | 一个方块在转动时变圆，再变回方形。 |
| `Orbit` | 一个逐渐淡出的半环绕着一个点转动。 |
| `Pulse` | 一个环从一个点向外扩散。 |
| `Radar` | 一道逐渐淡出的光束扫过暗淡的圆盘。 |
| `Ring` | 一条弧在暗淡的圆中转动。 |
| `Ripple` | 三个环从中心向外扩散。 |
| `Slide` | 三个点滑进一个方块空着的角。 |
| `Snake` | 一条弧在绕圈时伸长又缩短。 |
| `Swirl` | 一个明亮的小格绕着方块追逐自己的尾迹。 |
| `Trace` | 一条短线沿圆角方块的轮廓描画。 |
| `Wave` | 五根条像波浪一样起伏。 |

`loading.All()` 列出它们及其名称，用于画廊或让用户选择的设置：

```go
for _, s := range loading.All() {
	ui.Column(c).Gap(8).Center().Children(func() {
		s.New(c).Label(s.Name)
		ui.Text(c, s.Name)
	})
}
```

## 选项

每个函数接受 `loading.Options`，其零值是 loading.dev 的默认值：

```go
loading.Ring(c, loading.Options{
	Size:     32,                 // DIPs high: 20 by default
	Color:    t.Accent,           // the theme's text color by default
	Duration: 600 * time.Millisecond, // one cycle: the indicator's own by default
	Easing:   loading.EaseInOut,
	Cap:      loading.FlatCap,
})
```

- **`Size`** 是指示器的高度，也是它的宽度，`BouncingDots` 和 `LinearDots` 除外，它们是点组成的行，宽分别是高的 1.15 和 0.9375 倍。元素自身的大小（由 `Size`、`Fill` 或父级的布局决定）会缩放绘制内容以居中适应。
- **`Color`** 是它绘制所用的颜色，较淡的部分（环的轨道、淡出的点）以各自的透明度使用它。零值是主题的 `Text`。
- **`Duration`** 是它运动的一个周期，各指示器不同：`Arc` 为 800 ms，`Slide` 为 2.4 s。
- **`Paused`** 让它停在当前位置；设回 false，它从那里继续。
- **`Easing`** 是转动的指示器（`Arc`、`Atom`、`Clock`、`Comet`、`Dual`、`Orbit`、`Radar`、`Ring`、`Snake`、`Trace`）如何转动：`Linear` 匀速，`EaseInOut` 在每圈结束时减速，`Stacked` 两者相加，每周期两圈，中间快。
- **`Cap`** 决定 `Arc`、`Cascade`、`Dual`、`Ring`、`Snake` 和 `Trace` 描边端点的形状：`RoundCap` 或 `FlatCap`。
- **`Sweep`** 是 `Blocks` 方块的顺序：`Diagonal`、`Rows` 或 `Columns`。
- **`Direction`** 让 `Ripple` 的环从中心向外（`RippleOut`）或向内（`RippleIn`）。
- **`Origin`** 让 `Wave` 的条从中间（`WaveCenter`）或底部（`WaveBottom`）长出。

## 动效与辅助功能

指示器每次被绘制时都会请求一帧，与旋转加载指示器一样（`Painter.AnimationFrame`），并且只在显示时请求：不在视野中的指示器不会被绘制，也不会请求帧，它不会重新构建视图。每个指示器在首次显示时开始自己的周期，与 CSS 动画一样。

当桌面要求减少动效时（`ui.Preferences.ReduceMotion`：macOS 的“减弱动态效果”、Windows 的动画效果关闭或 GNOME 的动画关闭），指示器会停在 loading.dev 为减少动效设定的姿态，例如变暗的点或一圈环，并且不请求帧。

辅助技术会将其识别为进度不确定的进度指示器。用 `Label` 设置名称，说明正在加载什么：`loading.Ring(c).Label("Loading messages")`。
