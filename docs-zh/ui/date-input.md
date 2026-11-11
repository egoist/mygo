# 日期输入

<!-- preview date-input 320 -->

`ui.DateInput` 创建一个字段，按用户区域格式以数字显示 `*time.Time`，例如美国的 1/5/2026、英国的 05/01/2026 或中国的 2026/1/5，下方的[日历](calendar.md)会更改它。单击、Enter 或 Space 打开日历，在日历中单击选择一天，方向键和 Enter 也可以；Page Up 和 Page Down 或它的按钮按月切换，Escape 关闭日历。`Changed` 报告新日期，新日期保留原值的时间与时区。

```go
ui.Field(c, "Birthday", func() {
	ui.DateInput(c, &app.birthday)
})
```

在它旁边放一个[时间输入](time-input.md)，两者编辑同一个 `time.Time`：

```go
ui.Row(c).Gap(8).Children(func() {
	ui.DateInput(c, &app.meeting).Label("Date")
	ui.TimeInput(c, &app.meeting).Label("Time")
})
```

## 辅助功能

辅助技术看到一个弹出按钮，其值是日期，由它的 `Label` 或它所在的[字段](form.md)命名；还会看到打开时的日历。
