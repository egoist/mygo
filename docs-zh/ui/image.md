# 图像

<!-- preview image 240 -->

`ui.Image` 显示一个位图，或按自身颜色显示 SVG，默认按它的 DIP 尺寸显示，指定其他尺寸时缩放以适应。

```go
//go:embed logo.png
var logoPNG []byte

var logo, _ = ui.DecodeBitmap(logoPNG)

ui.Image(c, logo).Size(64, 64).Fit(ui.Contain).Radius(12)
```

## 位图

`ui.NewBitmap` 用 `image.Image` 创建位图，`ui.DecodeBitmap` 用 PNG、JPEG、GIF（其第一帧）、WebP 或 BMP 数据创建，并按照片相机的 EXIF 方向把照片摆正。只创建一次位图，不要在视图中创建：只要你使用它，MyGo 就把位图保留在 GPU 上。显示尺寸远小于其像素的位图（例如缩略图中的照片）会从一个副本绘制：该副本反复减半，直到再减半就会小于显示尺寸为止，且只创建一次，因此每个像素都能参与显示，而不会闪烁。

位图把像素同时保留在 CPU 和 GPU 上，充满照片的应用会注意到这一点。位图显示过之后，`ReleasePixels` 会丢弃它在 CPU 上的副本，包括图片本身和各减半副本的副本：它仍像以前一样从 GPU 绘制；当有东西需要像素时会重新生成它们，例如丢失纹理或在若干帧未使用后释放了纹理的 GPU、CPU 渲染器，或为缩小绘制而生成的减半副本；而 GPU 上传后又会再次丢弃它们。由 `DecodeBitmap` 创建的位图会从它接收的数据重新解码，代价只是像素数据的一小部分。由 `NewBitmap` 创建的位图会重新转换它创建时所用的 `image.Image`；从 `ReleasePixels` 起，它不复制地保留该图像：对于应用反正要保留的图像，例如它按完整尺寸处理的截图，像素于是在内存中只有一份而不是两份。无论哪种情况，图像都必须保持原样。如果 `NewBitmap` 所用的图像应用已不再保留（或不是包 `image` 的类型）、来自画布，或被 `Update` 修改过，它就没有其他来源来重新生成像素，因此会保留这些像素。像绘制一样，在主线程上调用 `ReleasePixels`：

```go
cover, _ := ui.DecodeBitmap(fileData)
// … once the cover has shown:
cover.ReleasePixels()

shot := ui.NewBitmap(capture) // the app keeps capture
// … once the shot has shown:
shot.ReleasePixels()
```

窗口关闭时会释放它显示过的内容：视图、元素、最后一帧的场景，以及它们绘制的位图。此后位图只在应用保留它期间存在。

`Update` 在位图中显示另一张图片，用于像视频那样变化的图像。尺寸相同时，它保留位图在 GPU 上的纹理，并由该纹理上传新像素；而每帧新建位图则会每帧创建一个纹理。由 `image.NewRGBA` 创建的 `*image.RGBA` 会被直接接收，不复制：在下一次 `Update` 之前让它的像素保持原样。在 goroutine 上解码的帧通过 `Window.Update` 到达位图，后者会重绘窗口：

```go
go func() {
	for frame := range frames { // *image.RGBA, a new one each time
		win.Update(func() { app.video.Update(frame) })
	}
}()
```

[`ui.Video`](video.md) 就这样播放视频的帧，提前解码，每一帧到期时显示，开箱即支持动画 GIF 和 PNG；视频文件和流用系统的媒体引擎（`ui/video`）播放，其帧保留在 GPU 上。

`UpdateYUV` 显示 Y′CbCr 格式的视频帧，即解码器给出的格式，而不把它转换成 RGBA。`ui.YUVImage` 保存其平面，每个平面按各自的跨距分行：NV12 的亮度平面，然后是交错着 Cb 和 Cr 的色度平面，如硬件解码器留下的那样；或 I420 的亮度、Cb 和 Cr 平面，如软件解码器给出的那样；还包括转换它们的矩阵（`ui.BT601`、`ui.BT709` 或 `ui.BT2020`）以及它们是否采用完整范围。GPU 上传这些平面，每像素 1.5 字节，而 RGBA 是 4 字节，并在采样时转换它们，CPU 因此无需做任何转换，而显示一帧本来会让它付出的主要代价正在于此。这些平面会被直接接收，不复制，但 I420 的色度平面除外：位图会把它交错进自己的一个平面，在下一次更新之前让它们保持原样。

```go
win.Update(func() {
	app.video.UpdateYUV(&ui.YUVImage{
		Format: ui.NV12, Width: 1920, Height: 1080,
		Planes:  [3][]byte{luma, chroma},
		Strides: [3]int{lumaStride, chromaStride},
		Matrix:  ui.BT709,
	})
})
```

这样的帧按它的尺寸显示，GPU 在采样时缩放它，不会像显示尺寸远小于自身尺寸的位图那样生成减半副本：只要解码器能做到，就让它给出帧显示时的尺寸。CPU 渲染器和截图则在绘制时转换该帧。

`Pixelated` 把显示尺寸大于其像素的位图绘制成按像素颜色着色的方块，就像绘制像素艺术和游戏精灵那样，而不是把相邻像素平滑混合。在自身尺寸的整数倍时，每个像素是一块完整的屏幕像素。在其他尺寸下，例如 2.5 倍缩放，各方块保持同一尺寸，边缘只在一个屏幕像素内混合，而不会有些方块比另一些宽一个像素；它们平滑移动，而不是跳跃，如同 Ebitengine 的 `FilterPixelated` 所绘制的那样。显示得更小时，它与其他位图一样平滑。

```go
tiles, err := ui.DecodeBitmap(tilesPNG)
if err != nil {
	log.Fatal(err)
}
tiles.Pixelated()
ui.Image(c, tiles).Size(64, 64) // 16×16 pixels, each 4×4
```

## 适应

`Fit` 说明图像如何填充盒子：`Contain`（默认）把它完整放进盒子，`Cover` 覆盖盒子并裁剪其余部分，`FillBox` 拉伸它，`ScaleDown` 按自身尺寸显示它，除非放不下，`NaturalSize` 按自身尺寸显示并裁剪。`Grayscale` 用灰阶绘制它，例如用于禁用的内容。`Radius` 把它的角做成圆角，`AspectRatio` 在会增长的盒子中保持它的比例。

```go
ui.Image(c, photo).AspectRatio(16.0 / 9).Fit(ui.Cover).Radius(8)
```

`ObjectPosition(x, y)` 决定 `Fit` 裁剪图片或在其周围留白时图片的位置，如同 CSS 的 `object-position`：即在该空间中横向和纵向的比例，0 表示左边缘或上边缘，1 表示右边缘或下边缘，0.5（默认）表示居中。其边缘关键字是 0 和 1：`left top` 就是 `ObjectPosition(0, 0)`。例如一个头像，人脸靠近照片顶部，需要保持在视野中，如同 `object-position: 50% 15%`：

```go
ui.Image(c, photo).Size(40, 40).Radius(20).Fit(ui.Cover).ObjectPosition(0.5, 0.15)
```

## SVG

`ui.ParseSVG` 解析 SVG，`ui.MustParseSVG` 解析作为程序一部分（例如嵌入文件）的 SVG，出错时会 panic。`Image` 按 SVG 自身的颜色显示它，其 `currentColor` 用文本颜色，尺寸采用 SVG 给出的尺寸（它的 `width` 和 `height`，或其 `viewBox` 的尺寸），除非另有指定：

```go
ui.Image(c, illustration).Width(240)
```

要按文本颜色显示 SVG，例如作为图标，请使用[图标](icon.md)。

对 SVG 显示的每个尺寸，MyGo 都像绘制文本那样把它的形状绘制进 GPU 的图集一次，之后由 GPU 从图集绘制：移动、滚动图像或改变它的颜色都不会再次绘制。MyGo 绘制路径和基本形状、组、`use` 和 `symbol` 元素、变换、颜色、`currentColor` 以及线性和径向渐变的填充和描边（及其连接、端点和虚线）、不透明度、裁剪路径、蒙版，以及简单选择器（类型、类和 id）的样式表。它不绘制文本、嵌入图像、图案、标记和滤镜：在导出前请在编辑器中把文本转换为路径。

在 `Draw` 回调中，`Painter.Image` 也绘制位图和 SVG。

## 任何格式的图片

`ui.DecodeImage` 解码事先不知道格式的图片，例如从链接下载的图片：`DecodeBitmap` 认识其格式时是位图，否则是带尺寸的 SVG。`ui.ImageSize` 给出 `Image` 默认显示位图或 SVG 的尺寸，以 DIP 为单位，便于按宽高比设置尺寸：

```go
pic, err := ui.DecodeImage(data) // once, as it arrives, not in the view
if err != nil {
	return err
}
// … in the view:
if w, h := ui.ImageSize(pic); w > 0 && h > 0 {
	ui.Image(c, pic).MaxWidthPercent(100).AspectRatio(w / h)
}
```

## 辅助功能

图像是辅助技术看不到的装饰，除非用 `Label` 为它们命名，例如一张重要的照片。
