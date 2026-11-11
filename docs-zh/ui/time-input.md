# 时间输入

<!-- preview time-input 120 -->

`ui.TimeInput` 创建一个字段，显示 `*time.Time` 的当天时间，采用用户区域格式（`Context.Region`）的小时制：15:04 或 3:04 PM，小时、分钟和上下午各可取得键盘焦点，如同 AppKit 日期选择器的分段：Up 和 Down 让持有焦点的那一段按钟面循环步进，键入数字会设置该段，小时输入完后会自动转到分钟，AM 或 PM 的首字母（或该语言中对应名称的首字母）会选择上下午，Left 和 Right 在它们之间移动焦点。上下午的名称及其位置取决于窗口的语言：中文为“下午3:04”，上下午在前。`Changed` 报告新时间，新时间保留原值的日期和时区。

```go
ui.TimeInput(c, &app.alarm).Label("Alarm")
```

单击小时或分钟会让它取得焦点。与[日期输入](date-input.md)一起使用时，两者编辑同一个 `time.Time`。

## 辅助功能

辅助技术看到几个微调按钮，名称由字段的 `Label` 加上“hours”“minutes”和“AM/PM”组成，使用窗口的语言。
