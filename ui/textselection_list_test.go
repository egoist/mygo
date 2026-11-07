package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
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
	parts := tt.rt.selectedTexts(tt.rt.selection.scope)
	if len(parts) != 151 || parts[0].ID == 0 || parts[1].ID != 0 || parts[1].Text != (*rows)[3] || parts[1].End != len((*rows)[3]) || parts[150].ID == 0 {
		t.Errorf("SelectedTexts: %d parts, the first %+v, %+v", len(parts), parts[0], parts[1])
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

// Copy takes the rows as the app has them when the list builds them: rows
// gone since the last frame are not asked for.
func TestSelectableListCopyAfterRowsGo(t *testing.T) {
	var list ListState
	tt, rows := listOfMessages(&list)
	list.Text = func(i int) string { return (*rows)[i] }
	tt.ClickAt(selectableAt(t, tt, (*rows)[0], 0))
	tt.Key(Cmd, KeyA)
	tt.Frame()
	*rows = (*rows)[:100] // no frame between this and the copy
	if got, want := copied(tt), strings.Join(*rows, "\n"); got != want {
		t.Errorf("copied %d lines, want 100", strings.Count(got, "\n")+1)
	}
}

// SelectedTexts asked as the view builds, after other elements came
// before the list, still has the rows the list did not build.
func TestSelectableListSelectedTextsWhileBuilding(t *testing.T) {
	var list ListState
	rows := make([]string, 200)
	for i := range rows {
		rows[i] = fmt.Sprintf("Message %03d", i)
	}
	list.Key = func(i int) any { return rows[i] }
	list.Text = func(i int) string { return rows[i] }
	extra := false
	var parts []SelectedText
	tt := coreNewTester(func(c *context) {
		col := coreColumn(c).Selectable().Size(300, 200)
		col.Children(func() {
			if extra {
				coreBox(c).Size(10, 10)
			}
			c.nextKey, c.keySet = "list", true // the same list, after the box
			coreList(c, &list, len(rows), func(i int) { coreText(c, rows[i]).Padding(4) }).Grow(1)
		})
		parts = col.SelectedTexts()
	}, 400, 300)
	tt.ClickAt(selectableAt(t, tt, rows[0], 0))
	tt.Key(Cmd, KeyA)
	tt.Frame()
	extra = true
	tt.Frame()
	if len(parts) != 200 {
		t.Errorf("SelectedTexts as the view builds has %d parts, want 200", len(parts))
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

// A Copy of a selection reaching into a list copies it as it was asked
// for, though it changes before the frame that copies it.
func TestSelectableListCopyAsAsked(t *testing.T) {
	var list ListState
	tt, rows := listOfMessages(&list)
	selectBetween(t, tt, (*rows)[2], 4, (*rows)[3], 4)
	tt.SetClipboard("nothing")
	tt.rt.textSelectionCommand(tt.rt.states[tt.rt.focused], "copy")
	tt.rt.selection.caret = tt.rt.selection.anchor // the selection changes before the frame
	tt.Frame()
	if got := tt.Clipboard(); got != "age 002\nMess" {
		t.Errorf("copied %q, want the selection asked for", got)
	}
}
