# 测试

`ui.NewTester` 无需窗口即可运行视图，速度与单元测试相当：它在内存中渲染帧，按文本查找元素，并执行单击、键入、滚动和按键。

```go
func TestCounter(t *testing.T) {
	app := &counter{}
	tt := ui.NewTester(app.view, 320, 240)
	if err := tt.Click("Increment"); err != nil {
		t.Fatal(err)
	}
	if app.n != 1 || !tt.HasText("1") {
		t.Errorf("count %d, texts %q", app.n, tt.Texts())
	}
}
```

元素按文本查找，没有文本的则按 `Label` 查找，例如图标按钮：`tt.Click("Close")`、`tt.Find("Volume")`、`tt.Focused("Name")`。

`Click`、`ClickWith` 和 `RightClick` 在发送输入之前检查第一个匹配的元素。如果它没有可见区域，或该区域中心会落到另一个控件上或落到空处，它们就返回错误。它们不会把它滚动到可见范围内，也不会另找一个未被遮挡的点。对于没有自己的上下文菜单的元素，例如列表的行，`RightClick` 会在该点打开元素在该处所含内容的菜单，如同在那里右键单击；其中若有接受单击或自身输入的控件，那就算另一个控件。`Find` 仍会返回匹配的裁剪后盒子，它可能宽度或高度为零；`ClickAt` 在给定坐标发送输入，没有命名目标。

同一父元素下两个元素被赋予相同的 `Key` 时会共享同一个状态，因此本应给其中一个的单击或焦点会去到另一个：测试器会在赋予第二个 key 的位置 panic，而应用只会记录它。

## 测试器做什么

- **指针。** `tt.Click`、`tt.ClickAt`、`tt.ClickWith(ui.Shift, "Row 3")`、`tt.Press`、`tt.Move` 和 `tt.Release`、用 `tt.PressButton` 和 `tt.ReleaseButton` 操作另一个按钮（例如右键拖动用 `tt.PressButton(1, x, y)`）、`tt.CancelPress`（像系统取消按下那样结束它，不产生单击）、`tt.Scroll`（像触摸板那样立即滚动）和 `tt.Wheel`（鼠标滚轮的一格，会随着时钟前进而滑向目标，见 `tt.Advance`）。`tt.UsePointer(ui.PointerPen, 0.6)` 让之后的按下和移动来自以该压力按下的笔，或手指（`ui.PointerTouch`）。
- **键盘。** `tt.Key(ui.Cmd, ui.KeyS)` 按下一个键，`tt.KeyRepeat` 在按住时像自动重复那样再次按下它，`tt.TypeKey` 输入一个键及它产生的文本，`tt.Type` 向持有焦点的元素输入文本。`tt.Compose` 显示输入法的组合状态，`tt.Command` 执行菜单的编辑命令，如 `"copy"`，`tt.TextCaret` 返回输入法将会组合的位置。
- **菜单。** `tt.RightClick` 打开上下文菜单，`tt.Menu` 列出菜单，`tt.ChooseMenuItem("Move to", "Archive")` 从中选择，`tt.CloseMenu` 关闭它：系统的菜单，或[在窗口中绘制](context-menu.md#在窗口中绘制)的菜单，因此测试在两种菜单下都能通过。
- **窗口。** `tt.SetSize`、`tt.SetScale`、`tt.SetZoom`（内容的[缩放](windows.md#缩放)，如同 `Context.SetZoom`；测试器的坐标——`Find`、`ClickAt`、`Move` 和 `TextCaret` 的——仍是窗口的）、`tt.SetDark`、`tt.SetPreferences` 和 `tt.SetDirection`（按[从右到左](right-to-left.md)布局）改变窗口和桌面的设置；`ui.NewTesterWith(view, w, h, ui.TesterOptions{Preferences: p, Dark: true})` 从桌面的设置启动，第一帧便遵循它们，如同 Media Queries 5 让用户的偏好从样式首次解析时起生效：在减少动效下挂载的视图不会在最初几帧启动动画，而 `NewTester` 会在 `SetPreferences` 来得及生效之前就稳定下来；`tt.SetFocused` 从窗口取走键盘再交还，`tt.SetWindowActive` 把它先绘制为非活动再绘制为活动，如同另一个窗口取得用户的输入（[非活动窗口](windows.md#活动与非活动窗口)），`tt.SetWindowHidden` 把它完全隐藏再重新显示（[隐藏的窗口](windows.md#隐藏的窗口)）。
- **它显示的内容。** `tt.Texts`、`tt.HasText`、`tt.Find`（文本所在的盒子）、`tt.Focused`、`tt.Cursor`、`tt.Clipboard`、`tt.OpenedURLs` 和 `tt.Announcements`，即视图请求屏幕阅读器朗读的内容（[实时区域与播报](#实时区域与播报)）；`tt.FailOpenURL` 让它打开的链接失败，如同没有任何应用能打开它们，`tt.FailClipboard` 让它向剪贴板写入文本失败，如同浏览器拒绝该操作（`WriteClipboardThen`）。
- **辅助技术看到什么。** `tt.Access` 和 `tt.AccessByRole` 读取元素的角色、名称、值和状态，`tt.AccessAll` 读取每个元素的（[见下文](#辅助技术看到什么)）。
- **帧。** `tt.Frame` 绘制一帧，用于视图之外发生变化时，`tt.Image()` 是最后一帧，用于快照；`ui.Render` 在给定缩放下绘制一次视图。输入和 `tt.Frame` 会运行视图请求的帧，直到它不再请求，或运行 20 帧：`tt.FramePending` 说明它是否仍在请求，例如一个动画不停或永远无法稳定的视图。
- **时间。** 测试器有自己的时钟 `tt.Now()`，它静止不动，直到 `tt.Advance(d)` 把它向前移动 `d`，并按各自的时间运行期间到期的帧：有东西移动时每次刷新一帧，`c.After` 和 `Painter.After` 的帧在到期时运行。依赖时间的东西都跟随它：动画、`Context.After`、`Context.Now`、构成双击的单击、一步撤销的键入、插入符的闪烁、工具提示和 toast。因此测试在繁忙的机器上和快速的机器上会得到相同结果：绝不要靠睡眠等待某事发生，而要推进时钟。`Advance` 只运行视图请求的帧：从视图之外改变视图显示的内容后，先调用 `tt.Frame`。`editor.Buffer` 和 `richtext.Document` 的撤销步骤使用系统的时钟。

  ```go
  tt := ui.NewTester(app.view, 320, 240)
  tt.Click("More")
  tt.Advance(100 * time.Millisecond) // half way through a 200 ms animation
  tt.Advance(time.Second)            // and done
  if tt.FramePending() {
  	t.Error("the panel keeps asking for frames")
  }
  ```

## 实时区域与播报

`tt.Announcements()` 返回视图用 `c.Announce` 请求屏幕阅读器朗读的内容，然后清空记录。要测试实时区域朗读什么，用 `tt.UseScreenReader()` 打开屏幕阅读器：从那时起，每一帧的辅助功能树都会送到它那里；如同 ARIA，变化的 `RoleStatus` 文本和添加到 `RoleLog` 的内容会被礼貌地朗读一次，出现在 `tt.Announcements()` 中，与 `c.Announce` 的文本一起，而 `RoleAlert` 会立即朗读，出现在 `tt.Alerts()` 中。屏幕阅读器开启之前显示的内容不会被朗读，如同真实的屏幕阅读器附加时。

```go
tt := ui.NewTester(view, 400, 300)
tt.UseScreenReader()
tt.Click("Save")
if got := tt.Announcements(); !slices.Equal(got, []string{"Saved 2 files"}) {
	t.Errorf("read out %q", got)
}
```

## 辅助技术看到什么

`tt.AccessByRole(role, name)` 返回屏幕阅读器从最后一帧中第一个具有该角色和可访问名称的元素（`""` 表示任意名称）得到的内容，如同 Testing Library 的 `getByRole(role, { name })` 和 Playwright 的查找方式，`tt.Access(name)` 返回第一个具有该名称的元素，无论其角色。它们返回的 `ui.AccessInfo` 包含角色、名称、值、描述和 `RoleDescription`，以及各项状态，如[辅助功能](accessibility.md#状态)列出的：`Checked` 和 `Mixed`（`aria-checked`、`aria-pressed`）、`Selected`（`aria-selected`，被选中的标签页、树项、行或网格单元格，带 `Selected` 或 `AccessSelected`）、`Expanded`、`HasPopup`、`Busy`、`Current`、`AutoComplete`（`aria-autocomplete`）、`Sort`（列标题的 `ui.SortDirection`）、`Disabled`、`Invalid`、`Focused`（焦点所在的元素或选项，包括 `ActiveDescendant`）、`Controls`（它控制的元素的名称，`Access` 能找到它们）、`DescribedBy` 和 `ErrorMessage`（描述它的元素和保存它错误的元素的名称）、`Vertical` 和 `Horizontal`（`aria-orientation`）、`Level`、`PosInSet` 和 `SetSize`（`aria-posinset`、`aria-setsize`，如 `PositionInSet` 给出的）、`Multiselectable`、`RowCount`、`ColumnCount`、`RowIndex` 和 `ColumnIndex`（[网格](accessibility.md#网格)的）、`RowSpan` 和 `ColumnSpan`（其单元格的）、范围的 `Min`、`Max` 和 `Now`，以及它的 `Bounds`。这样测试检查的是状态确实到达了辅助技术，而不只是视图设置了它：

```go
tt := ui.NewTester(app.view, 320, 240)
reply, ok := tt.AccessByRole(ui.RoleStatus, "Reply")
if !ok || !reply.Busy {
	t.Errorf("Reply while streaming: %+v, found %v", reply, ok)
}
if docs, _ := tt.AccessByRole(ui.RoleLink, "Docs"); docs.Current != ui.CurrentPage {
	t.Errorf("Docs is not the current page: %+v", docs)
}
```

`tt.AccessAll()` 返回最后一帧每个元素的相同信息，按屏幕阅读器读取它们的顺序，用于对整个视图的检查，例如没有控件缺少名称：

```go
for _, a := range tt.AccessAll() {
	if a.Role == ui.RoleButton && a.Name == "" {
		t.Errorf("a button at %v has no name", a.Bounds)
	}
}
```

角色是元素被赋予的角色（来自它的控件或 `Role`），或根据它的作用推断出的角色：文本、按钮、组。树按屏幕阅读器会得到的样子构建，无论是否有屏幕阅读器读取窗口，读取它都不会改变任何东西：它不会构建帧。

## 测试构建后的应用

测试器在 `go test` 中运行视图。要测试构建后的应用，包括真实窗口，并从另一个进程进行，如同端到端测试运行器或 agent 所做，把[自动化插件](../plugins/automation.md)导入测试构建：它像测试器的 `Find` 那样，按标签或 `Key` 读取每个窗口的元素，对它们执行操作，发送输入，并像 `FramePending` 指示的那样等待帧。
