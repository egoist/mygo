package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// listOfMessages is a Selectable List of 200 one-line messages, as a chat
// sharing one selection across its rows.
func listOfMessages(list *ListState) (*Tester, *[]string) {
	rows := make([]string, 200)
	for i := range rows {
		rows[i] = fmt.Sprintf("Message %03d", i)
	}
	list.Key = func(i int) any { return rows[i] }
	tt := coreNewTester(func(c *context) {
		coreList(c, list, len(rows), func(i int) { coreText(c, rows[i]).Padding(4) }).Selectable().Size(300, 200)
	}, 400, 300)
	return tt, &rows
}

func copied(tt *Tester) string {
	tt.SetClipboard("nothing")
	tt.Key(Cmd, KeyC)
	return tt.Clipboard()
}

// selectionOf returns the runes of paragraph text the selection paints,
// and whether it is built.
func selectionOf(tt *Tester, text string) (from, to int, built bool) {
	for _, s := range tt.rt.texts {
		if s.editor.source == text {
			return min(s.editor.anchor, s.editor.caret), max(s.editor.anchor, s.editor.caret), true
		}
	}
	return 0, 0, false
}

// A selection keeps both its ends while the list scrolls them away and
// back: a drag's end, whose row holds no focus, and the start of a
// selection a Shift-click extended, whose row gave the focus up.
func TestSelectableListKeepsSelectionOutOfView(t *testing.T) {
	var list ListState
	tt, rows := listOfMessages(&list)
	selectBetween(t, tt, (*rows)[2], 4, (*rows)[3], 4)
	const want = "age 002\nMess"
	if got := copied(tt); got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
	list.ScrollToEnd()
	tt.Frame()
	if got := copied(tt); got != want {
		t.Errorf("with the drag's end scrolled away, copied %q, want %q", got, want)
	}
	list.ScrollTo(0, Start)
	tt.Frame()
	if got := copied(tt); got != want {
		t.Errorf("scrolled back, copied %q, want %q", got, want)
	}
	if from, to, _ := selectionOf(tt, (*rows)[3]); from != 0 || to != 4 {
		t.Errorf("scrolled back, the drag's end paints runes %d to %d, want 0 to 4", from, to)
	}

	selectBetween(t, tt, (*rows)[2], 4, (*rows)[2], 4)
	list.ScrollTo(5, Start)
	tt.Frame()
	if err := tt.ClickWith(Shift, (*rows)[6]); err != nil {
		t.Fatal(err)
	}
	const extended = "age 002\nMessage 003\nMessage 004\nMessage 005\nMessage 006"
	if got := copied(tt); got != extended {
		t.Fatalf("Shift-click copied %q, want %q", got, extended)
	}
	// The start's row, kept out of view, comes before the rows in view:
	// those after the end show no selection.
	if from, to, built := selectionOf(tt, (*rows)[9]); !built || from != to {
		t.Errorf("a row after the selection paints runes %d to %d (built %v)", from, to, built)
	}
}

// Rows the list did not build between the ends copy as ListState.Text
// gives them, and Select All copies every row; without Text, the rows
// built alone.
func TestSelectableListCopiesRowsNotBuilt(t *testing.T) {
	var list ListState
	tt, rows := listOfMessages(&list)
	list.Text = func(i int) string { return (*rows)[i] }
	selectBetween(t, tt, (*rows)[2], 4, (*rows)[2], 4)
	list.ScrollTo(150, Start)
	tt.Frame()
	if err := tt.ClickWith(Shift, (*rows)[152]); err != nil {
		t.Fatal(err)
	}
	want := "age 002\n" + strings.Join((*rows)[3:153], "\n")
	if got := copied(tt); got != want {
		t.Fatalf("copied %d lines, want %d:\n%s", strings.Count(got, "\n")+1, strings.Count(want, "\n")+1, got)
	}
	parts := tt.rt.selectedTexts(tt.rt.selection.scope, true)
	if len(parts) != 151 || parts[0].ID == 0 || parts[1].ID != 0 || parts[1].Text != (*rows)[3] || parts[1].End != len((*rows)[3]) || parts[150].ID == 0 {
		t.Errorf("CopyData's parts: %d, the first %+v, %+v", len(parts), parts[0], parts[1])
	}
	// SelectedTexts, which the app asks as it builds, has the rows built.
	built := tt.rt.selectedTexts(tt.rt.selection.scope, false)
	if len(built) < 2 || len(built) >= 151 || built[0].Text != (*rows)[2] || built[len(built)-1].Text != (*rows)[152] || slices.ContainsFunc(built, func(p SelectedText) bool { return p.ID == 0 }) {
		t.Errorf("SelectedTexts: %+v", built)
	}

	// Select All from a click in view, which leaves the rows above out.
	tt.ClickAt(selectableAt(t, tt, (*rows)[151], 2))
	tt.Key(Cmd, KeyA)
	if got, want := copied(tt), strings.Join(*rows, "\n"); got != want {
		t.Fatalf("Select All copied %d lines, want 200", strings.Count(got, "\n")+1)
	}
	list.ScrollTo(80, Start)
	tt.Frame()
	if from, to, built := selectionOf(tt, (*rows)[85]); !built || from != 0 || to != len((*rows)[85]) {
		t.Errorf("after Select All, a row built later paints runes %d to %d (built %v)", from, to, built)
	}
	if got, want := copied(tt), strings.Join(*rows, "\n"); got != want {
		t.Errorf("scrolled after Select All, copied %d lines, want 200", strings.Count(got, "\n")+1)
	}
	x, y := selectableAt(t, tt, (*rows)[85], 3)
	tt.SetClipboard("nothing")
	tt.RightClickAt(x, y)
	if err := tt.ChooseMenuItem("Copy"); err != nil {
		t.Fatal(err)
	}
	if got, want := tt.Clipboard(), strings.Join(*rows, "\n"); got != want {
		t.Errorf("the context menu of a row built after Select All copied %d lines, want 200", strings.Count(got, "\n")+1)
	}

	list.Text = nil
	tt.Frame()
	tt.Key(Cmd, KeyA)
	got := strings.Split(copied(tt), "\n")
	if len(got) >= 50 || !slices.Contains(got, (*rows)[85]) {
		t.Errorf("without Text, Select All copied %d rows", len(got))
	}
}

// The selection goes with the item holding an end, and only with it.
func TestSelectableListSelectionFollowsItems(t *testing.T) {
	var list ListState
	tt, rows := listOfMessages(&list)
	selectBetween(t, tt, (*rows)[2], 4, (*rows)[3], 4)
	*rows = append([]string{"New 1", "New 2"}, *rows...) // rows come above
	list.ScrollToEnd()
	tt.Frame()
	if got := copied(tt); got != "age 002\nMess" {
		t.Errorf("with rows added above, copied %q", got)
	}
	*rows = slices.Delete(*rows, 5, 6) // the end's item goes
	tt.Frame()
	if got := copied(tt); got != "nothing" {
		t.Errorf("with the end's item gone, copied %q", got)
	}
}

// After Select All, the selection's ends are the list's first and last
// rows: a Shift-click extends from the first, and changing a row's text
// clears it.
func TestSelectableListSelectAllEnds(t *testing.T) {
	var list ListState
	tt, rows := listOfMessages(&list)
	list.Key = func(i int) any { return i } // a row keeps its element as its text changes
	list.Text = func(i int) string { return (*rows)[i] }
	list.ScrollTo(100, Start)
	tt.Frame()
	tt.ClickAt(selectableAt(t, tt, (*rows)[101], 2))
	tt.Key(Cmd, KeyA)
	if err := tt.ClickWith(Shift, (*rows)[103]); err != nil {
		t.Fatal(err)
	}
	if got, want := copied(tt), strings.Join((*rows)[:104], "\n"); got != want {
		t.Errorf("Shift-click after Select All copied %d lines, want 104", strings.Count(got, "\n")+1)
	}

	tt.Key(Cmd, KeyA)
	tt.Frame()
	(*rows)[102] = "Edited"
	tt.Frame()
	if got := copied(tt); got != "nothing" {
		t.Errorf("with a row's text changed after Select All, copied %d lines", strings.Count(got, "\n")+1)
	}
}

// Copy puts the selection on the clipboard as it is asked for, as a
// browser's copy event needs, with the rows of the last frame: Select All
// and Copy with no frame between them copy every row.
func TestSelectableListCopiesAtOnce(t *testing.T) {
	var list ListState
	tt, rows := listOfMessages(&list)
	list.Text = func(i int) string { return (*rows)[i] }
	tt.ClickAt(selectableAt(t, tt, (*rows)[1], 2))
	tt.SetClipboard("nothing")
	tt.rt.event(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: "selectAll"})
	tt.rt.event(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: "copy"})
	if got, want := tt.Clipboard(), strings.Join(*rows, "\n"); got != want {
		t.Errorf("copied %d lines at once, want 200", strings.Count(got, "\n")+1)
	}
}

// Rows the app rebuilds as the view builds, as a reply regenerated with
// fewer parts, are not asked for by the last frame's indices:
// SelectedTexts has the paragraphs built, and Copy after the frame the
// rows the app has then.
func TestSelectableListRowsRebuilt(t *testing.T) {
	var list ListState
	rows := make([]string, 200)
	for i := range rows {
		rows[i] = fmt.Sprintf("Message %03d", i)
	}
	list.Key = func(i int) any { return rows[i] }
	list.Text = func(i int) string { return rows[i] } // out of range for rows gone
	fewer := false
	var parts []SelectedText
	tt := coreNewTester(func(c *context) {
		if fewer {
			rows = rows[:50]
		}
		col := coreColumn(c).Selectable().Size(300, 200)
		parts = col.SelectedTexts() // as a context menu's item asks
		col.Children(func() {
			coreList(c, &list, len(rows), func(i int) { coreText(c, rows[i]).Padding(4) }).Grow(1)
		})
	}, 400, 300)
	tt.ClickAt(selectableAt(t, tt, rows[0], 0))
	tt.Key(Cmd, KeyA)
	fewer = true
	tt.Frame()
	if len(parts) == 0 || len(parts) >= 50 || slices.ContainsFunc(parts, func(p SelectedText) bool { return p.ID == 0 }) {
		t.Errorf("SelectedTexts as the rows are rebuilt has %d parts, want those built", len(parts))
	}
	if got, want := copied(tt), strings.Join(rows, "\n"); got != want {
		t.Errorf("copied %d lines, want 50", strings.Count(got, "\n")+1)
	}
}

// A list choosing rows inside a Selectable container is a control owning
// its rows' text: it does not join the selection, nor is Text asked.
func TestSelectableChoosableList(t *testing.T) {
	var list ListState
	selected := -1
	list.Selected = &selected
	list.Text = func(i int) string {
		t.Errorf("Text asked for row %d of a list choosing rows", i)
		return ""
	}
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Selectable().Size(300, 250).Children(func() {
			coreText(c, "Before")
			coreList(c, &list, 200, func(i int) { coreText(c, fmt.Sprintf("Row %03d", i)) }).Grow(1)
			coreText(c, "After")
		})
	}, 400, 300)
	tt.ClickAt(selectableAt(t, tt, "Before", 2))
	tt.Key(Cmd, KeyA)
	if got := copied(tt); got != "Before\nAfter" {
		t.Errorf("Select All copied %q, want %q", got, "Before\nAfter")
	}
}

// A Selectable container inside each row keeps a selection of its own:
// the rows of the list around it are not its.
func TestSelectableInsideListRows(t *testing.T) {
	var list ListState
	rows := make([]string, 200)
	for i := range rows {
		rows[i] = fmt.Sprintf("Message %03d", i)
	}
	list.Text = func(i int) string { return rows[i] }
	tt := coreNewTester(func(c *context) {
		coreList(c, &list, len(rows), func(i int) {
			coreColumn(c).Selectable().Children(func() { coreText(c, rows[i]).Padding(4) })
		}).Size(300, 200)
	}, 400, 300)
	tt.ClickAt(selectableAt(t, tt, rows[3], 2))
	tt.Key(Cmd, KeyA)
	if got := copied(tt); got != rows[3] {
		t.Errorf("Select All in a row copied %q, want %q", got, rows[3])
	}
}

// A list whose rows built hold no selectable text, as images, still
// copies the rows with text it did not build, between the text around it.
func TestSelectableListWithoutTextBuilt(t *testing.T) {
	var list ListState
	text := func(i int) string {
		if i < 20 {
			return "" // an image
		}
		return fmt.Sprintf("Row %03d", i)
	}
	list.Text = text
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Selectable().Size(300, 250).Children(func() {
			coreText(c, "Before")
			coreList(c, &list, 200, func(i int) {
				if i < 20 {
					coreBox(c).Size(20, 20)
				} else {
					coreText(c, text(i))
				}
			}).Grow(1)
			coreText(c, "After")
		})
	}, 400, 300)
	var want []string
	for i := 20; i < 200; i++ {
		want = append(want, text(i))
	}
	all := "Before\n" + strings.Join(want, "\n") + "\nAfter"
	selectBetween(t, tt, "Before", 0, "After", 5)
	if got := copied(tt); got != all {
		t.Errorf("Before to After copied %d lines, want %d", strings.Count(got, "\n")+1, len(want)+2)
	}
	tt.Key(Cmd, KeyA)
	if got := copied(tt); got != all {
		t.Errorf("Select All copied %d lines, want %d", strings.Count(got, "\n")+1, len(want)+2)
	}
}

// Shift with the arrows moves the selection's end into the row next to
// it, which the list keeps built with it out of view.
func TestSelectableListKeysOutOfView(t *testing.T) {
	var list ListState
	tt, rows := listOfMessages(&list)
	list.ScrollTo(50, Start)
	tt.Frame()
	tt.ClickAt(selectableAt(t, tt, (*rows)[53], 0)) // away from the edge, which a press scrolls
	list.ScrollTo(0, Start)
	tt.Frame()
	tt.Key(Shift, KeyLeft)
	tt.Key(Shift, KeyLeft)
	if got := copied(tt); got != "2\n" {
		t.Errorf("Shift-Left twice from the start of a row out of view copied %q, want %q", got, "2\n")
	}
}
