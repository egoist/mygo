# 量度计

<!-- preview meter 200 -->

`ui.Meter` 创建一条显示两个边界之间数值的条，如同 AppKit 的电平指示器和 Web 的 `meter`：默认使用主题的 `Success` 颜色，从给定的级别起改用 `Warning` 和 `Danger`，传 nil 表示没有级别。

```go
size := app.disk.Size
ui.Meter(c, app.disk.Used, 0, size, &ui.MeterLevels{Warning: 0.8 * size, Critical: 0.95 * size}).Label("Disk")
```

`MeterLevels` 指定从哪些级别起表示情况变糟：从 `Warning` 起用 `Warning` 颜色，从 `Critical` 起用 `Danger` 颜色。`Critical` 低于 `Warning` 时，低值才是坏值，如电池电量：

```go
ui.Meter(c, app.battery, 0, 100, &ui.MeterLevels{Warning: 20, Critical: 10}).Label("Battery")
```

## 颜色

`NormalColor`、`WarningColor` 和 `CriticalColor` 用你自己的颜色填充条，代替主题的 `Success`、`Warning` 和 `Danger`；留为零的则保持主题的颜色。一条存储条在几乎满之前显示为中性灰色，然后变为琥珀色，最后是主题的 `Danger`：

```go
ui.Meter(c, used, 0, size, &ui.MeterLevels{
	Warning: 0.8 * size, Critical: 0.95 * size,
	NormalColor: ui.RGB(142, 142, 147), WarningColor: ui.RGB(255, 159, 10),
}).Label("Storage")
```

若没有 `Warning` 和 `Critical`，条会全程使用 `NormalColor`，例如产品自有配色的配额条。`Background` 为轨道着色，默认为主题的 `Border`：

```go
ui.Meter(c, app.used, 0, app.quota, &ui.MeterLevels{NormalColor: brand}).Background(brandTrack).Label("Backup quota")
```

这条会保持它的形状、范围、从右到左的方向，以及辅助技术读取的内容。

对于进行中的工作，用[进度条](progress.md)。

## 辅助功能

辅助技术看到一个表示该值的电平指示器，由 `Label` 命名。
