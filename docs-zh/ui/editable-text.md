# 可编辑文本

<!-- preview editable-text 120 -->

`ui.EditableText` 把一个 `*string` 显示为就地编辑的文本，如同 Finder 中文件的名字：双击它，或它持有焦点时按 Enter，会显示一个编辑它的文本框，并选中扩展名之前的文本。Enter 会保留输入的内容，移开焦点也一样，Escape 则恢复原值；`Changed` 报告新值。

```go
if ui.EditableText(c, &app.title).Changed() {
	app.save()
}
```

## 在列表和表格中

在[列表](list.md)或[表格](table.md)的行中，由列表持有焦点，文本不取得焦点：macOS 上的 Return 和其他平台上的 F2 会编辑选中行的文本，而双击行则为 `Submitted`；单击已选中行上的文本会编辑它，此时第二次单击已不再构成双击。

```go
ui.Table(c, &app.files, cols, len(files), func(row, col int) {
	if col == 0 && ui.EditableText(c, &files[row].Name).Changed() {
		app.rename(files[row])
	}
})
```

## 辅助功能

辅助技术读取文本，以及编辑时的文本框。
