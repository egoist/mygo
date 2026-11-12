package ui

import "testing"

// The layout looks for the list, the toolbar, the form and the column fit
// of every element it lays out, and the special field tells it that there
// are none to look for: it must say what the element has.
func TestSpecialTellsWhatLaysOut(t *testing.T) {
	var root *node
	tt := coreNewTester(func(c *context) {
		root = coreColumn(c).Children(func() {
			coreList(c, &ListState{}, 3, func(i int) {
				coreText(c, "row")
			})
			coreForm(c, func() {
				coreField(c, "label", func() {
					coreText(c, "value")
				})
			})
			coreToolbar(c, func() {
				coreButton(c, "ok")
			})
			coreTable(c, &ListState{}, []TableColumn{{Title: "Name"}}, 2, func(row, col int) {
				coreText(c, "cell")
			})
			coreGridView(c, &GridState{}, 4, 100, 40, func(i int) {
				coreText(c, "item")
			})
			coreBox(c)
		})
	}, 400, 400)
	_ = tt
	var walk func(e *node)
	walk = func(e *node) {
		for ; e != nil; e = e.next {
			want := e.list != nil || e.overflow != nil || e.form != nil ||
				e.colFit != nil || e.gridFit != nil
			if e.special != want {
				t.Errorf("special is %v, and the element has %v", e.special, want)
			}
			walk(e.first)
		}
	}
	walk(root)
}
