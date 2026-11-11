# 范围滑块

<!-- preview range-slider 120 -->

`ui.RangeSlider` 创建一个带两个旋钮的滑块，用于在两个边界之间设置低值和高值的 `*float64`，如价格范围：每个旋钮都可取得键盘焦点，Tab 在两者之间移动焦点，持有焦点时方向键、Home 和 End 可移动它；在轨道上按下会移动离按下点最近的旋钮。两个旋钮不会互相越过。`Changed` 报告新值。

```go
ui.RangeSlider(c, &app.minPrice, &app.maxPrice, 0, 1000, 10).Label("Price")
ui.Textf(c, "$%.0f – $%.0f", app.minPrice, app.maxPrice)
```

步长大于 0 时，值会对齐到下界以及从下界起步长的整数倍，并带刻度线；步长为 0 时则不受限制。

## 辅助功能

辅助技术看到两个滑块，名称由范围滑块的 `Label` 加上“minimum”和“maximum”组成。
