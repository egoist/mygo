package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func queryItem(t *testing.T, tree *platform.AccessTree, q platform.AccessQuery) platform.AccessNode {
	t.Helper()
	n, ok := tree.Query(q)
	if !ok {
		t.Fatalf("query %+v failed", q)
	}
	return n
}

func TestCollectionAccessibilityCellsAndVirtualization(t *testing.T) {
	var state ListState
	built := 0
	columns := []TableColumn{{Title: "Name", ID: "name", Width: 120, AccessibilityLabel: func(i int) string { return fmt.Sprintf("Name %d", i) }}, {Title: "Size", ID: "size", Width: 120}}
	tt := NewTester(func(c *Context) {
		Table(c, &state, columns, 1_000_000, func(row, col int) { built++; Textf(c, "Cell %d %d", row, col) }).Grow(1)
	}, 200, 180)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	table := node(t, tree, platform.RoleTable, "")
	if table.Collection.Rows != 1_000_000 || table.Collection.Columns != 2 || len(table.Collection.ColumnHeaders) != 2 {
		t.Fatalf("counts: %+v", table.Collection)
	}
	if table.Scroll.MaxX() <= 0 || table.Scroll.MaxY() <= 0 {
		t.Fatalf("scroll: %+v", table.Scroll)
	}
	before := built
	far := queryItem(t, tree, platform.AccessQuery{Kind: platform.AccessQueryCell, Container: table.ID, Row: 900_000, Column: 0})
	if built != before || len(tt.rt.states) > 500 {
		t.Fatalf("query built views: %d -> %d, states %d", before, built, len(tt.rt.states))
	}
	if far.Label != "Name 900000" || far.States&platform.AccessVirtualized == 0 || far.Cell.Row != 900_000 || far.Cell.Column != 0 || far.Cell.RowSpan != 1 || far.Cell.ColumnSpan != 1 || !slices.Equal(far.Cell.ColumnHeaders, table.Collection.ColumnHeaders[:1]) {
		t.Fatalf("cell: %+v %+v", far, far.Cell)
	}
	row := queryItem(t, tree, platform.AccessQuery{Container: table.ID, Index: 900_000})
	if row.Role != platform.RoleRow || row.Item.Key != 900_000 {
		t.Fatalf("row: %+v", row)
	}
	far = queryItem(t, tree, platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: far.Item, Realize: true})
	if far.States&(platform.AccessVirtualized|platform.AccessOffscreen) != 0 || far.Label != "Cell 900000 0" {
		t.Fatalf("realized: %+v", far)
	}
	if built-before > 200 || len(tt.rt.states) > 500 {
		t.Fatalf("realize built too much: %d cells, %d states", built-before, len(tt.rt.states))
	}
	if far.Cell.Table != table.ID || far.Item.Index != 900_000 {
		t.Fatal("realization changed coordinates")
	}
	for _, q := range []platform.AccessQuery{{Kind: platform.AccessQueryCell, Container: table.ID, Row: -1, Column: 0}, {Kind: platform.AccessQueryCell, Container: table.ID, Row: 1_000_000, Column: 0}, {Kind: platform.AccessQueryCell, Container: table.ID, Row: 0, Column: 2}} {
		if _, ok := tree.Query(q); ok {
			t.Errorf("invalid query %+v succeeded", q)
		}
	}
}

func TestCollectionAccessibilityKeyedLifetimeAndColumnOrder(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	state := ListState{Key: func(i int) any { return items[i] }, Index: func(k any) int { return slices.Index(items, k.(string)) }}
	columns := []TableColumn{{Title: "Name", ID: "name"}, {Title: "Size", ID: "size"}}
	show := true
	tt := NewTester(func(c *Context) {
		if show {
			Table(c, &state, columns, len(items), func(r, col int) { Textf(c, "%s %d", items[r], col) }).Grow(1)
		}
	}, 400, 180)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	table := node(t, tree, platform.RoleTable, "")
	cell := queryItem(t, tree, platform.AccessQuery{Kind: platform.AccessQueryCell, Container: table.ID, Row: 1, Column: 1})
	items = []string{"d", "c", "a", "b"}
	state.Columns.Order = []string{"size", "name"}
	tt.Frame()
	moved := queryItem(t, tree, platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: cell.Item})
	if moved.ID != cell.ID || moved.Cell.Row != 3 || moved.Cell.Column != 0 {
		t.Fatalf("moved cell lost identity: %+v -> %+v", cell, moved)
	}
	header, _ := byID(tt.h.access, moved.Cell.ColumnHeaders[0])
	if header.Label != "Size" {
		t.Fatalf("wrong header: %+v", header)
	}
	items = items[:3]
	tt.Frame()
	if _, ok := tree.Resolve(cell); ok {
		t.Fatal("removed item remained live")
	}
	show = false
	tt.Frame()
	if _, ok := tree.Query(platform.AccessQuery{Container: table.ID, Index: 0}); ok {
		t.Fatal("removed collection remained queryable")
	}
	tt.rt.close()
	if _, ok := tree.Resolve(moved); ok {
		t.Fatal("closed content remained queryable")
	}
}

func TestCollectionAccessibilitySelectionBeyondViewport(t *testing.T) {
	chosen := Selection[int]{}
	selected := -1
	state := ListState{Selected: &selected, Selection: &chosen, Label: func(i int) string { return fmt.Sprintf("Row %d", i) }}
	tt := NewTester(func(c *Context) { List(c, &state, 100_000, func(i int) { Textf(c, "Row %d", i).Height(30) }).Grow(1) }, 300, 180)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	list := node(t, tree, platform.RoleList, "")
	act := func(i int, a platform.AccessActionKind) {
		n := queryItem(t, tree, platform.AccessQuery{Container: list.ID, Index: i})
		tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: n.ID, Item: n.Item, Action: a})
	}
	act(3, platform.AccessSelect)
	act(90_000, platform.AccessAddToSelection)
	selection := tree.Selection(list.ID)
	if len(selection) != 2 || selection[0].Item.Index != 3 || selection[1].Item.Index != 90_000 || selected != 90_000 {
		t.Fatalf("selection: %+v, cursor %d", selection, selected)
	}
	if tt.h.access.Focus != selection[1].ID {
		t.Fatalf("active-descendant focus %d, want %d", tt.h.access.Focus, selection[1].ID)
	}
	act(3, platform.AccessRemoveFromSelection)
	if chosen.Has(3) || !chosen.Has(90_000) {
		t.Fatal("removing selection affected other rows")
	}
	act(90_000, platform.AccessSelect)
	act(4, platform.AccessAddToSelection)
	act(90_000, platform.AccessSelect)
	if chosen.Len() != 1 || !chosen.Has(90_000) {
		t.Fatal("Select on an already selected row did not choose it alone")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: list.ID, Action: platform.AccessClearSelection})
	if len(tree.Selection(list.ID)) != 0 {
		t.Fatal("selection was not cleared")
	}
}

func TestCollectionAccessibilityScrollAndSorting(t *testing.T) {
	var order SortOrder
	state := ListState{Sort: &order}
	tt := NewTester(func(c *Context) {
		Table(c, &state, []TableColumn{{Title: "Name", Sortable: true, Width: 500}}, 10_000, func(r, col int) { Textf(c, "Row %d", r) }).Grow(1)
	}, 300, 180)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	table := node(t, tree, platform.RoleTable, "")
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: table.ID, Action: platform.AccessScrollBy, DY: 100})
	table = node(t, tt.h.access, platform.RoleTable, "")
	if table.Scroll.Y <= 0 {
		t.Fatal("relative scroll did not move")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: table.ID, Action: platform.AccessScrollTo, X: table.Scroll.MaxX(), Y: table.Scroll.MaxY()})
	table = node(t, tt.h.access, platform.RoleTable, "")
	if !state.AtEnd() || table.Scroll.X <= 0 {
		t.Fatalf("absolute scroll: %+v", table.Scroll)
	}
	head := node(t, tt.h.access, platform.RoleColumnHeader, "Name")
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: head.ID, Action: platform.AccessPress})
	if order.Column != "Name" || order.Descending || node(t, tt.h.access, platform.RoleColumnHeader, "Name").States&platform.AccessSortAscending == 0 {
		t.Fatal("ascending sort")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: head.ID, Action: platform.AccessPress})
	if !order.Descending || node(t, tt.h.access, platform.RoleColumnHeader, "Name").States&platform.AccessSortDescending == 0 {
		t.Fatal("descending sort")
	}
	if !slices.Contains(tt.h.announced, "Name, ascending") || !slices.Contains(tt.h.announced, "Name, descending") {
		t.Fatalf("sorting announcements: %v", tt.h.announced)
	}
}

func TestCollectionAccessibilityGridAndOutline(t *testing.T) {
	selected := -1
	state := GridState{Selected: &selected, Label: func(i int) string { return fmt.Sprintf("Item %d", i) }}
	tt := NewTester(func(c *Context) { GridView(c, &state, 10_003, 100, 60, func(i int) { Textf(c, "Item %d", i) }).Grow(1) }, 448, 180)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	grid := node(t, tree, platform.RoleList, "")
	if grid.Collection.Columns != 4 || grid.Collection.Rows != 2501 || grid.Collection.Items != 10_003 {
		t.Fatalf("grid counts: %+v", grid.Collection)
	}
	last := queryItem(t, tree, platform.AccessQuery{Kind: platform.AccessQueryCell, Container: grid.ID, Row: 2500, Column: 2})
	if last.Item.Index != 10_002 || last.Cell.Row != 2500 || last.Cell.Column != 2 {
		t.Fatalf("grid cell: %+v", last)
	}
	if _, ok := tree.Query(platform.AccessQuery{Kind: platform.AccessQueryCell, Container: grid.ID, Row: 2500, Column: 3}); ok {
		t.Fatal("padding cell was an item")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: last.ID, Item: last.Item, Action: platform.AccessSelect})
	if selected != 10_002 {
		t.Fatal("offscreen grid selection")
	}
	tt.SetSize(248, 180)
	after := queryItem(t, tree, platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: last.Item, Realize: true})
	if after.ID != last.ID || after.Cell.Column != 0 {
		t.Fatalf("grid regrouping lost identity: %+v", after)
	}
	var outline OutlineState[int]
	roots := make([]int, 10_000)
	for i := range roots {
		roots[i] = i
	}
	ot := NewTester(func(c *Context) {
		Outline(c, &outline, roots, func(i int) []int {
			if i == 9000 {
				return []int{20_000}
			}
			return nil
		}, func(i int) { Textf(c, "Node %d", i) }).Grow(1)
	}, 300, 180)
	ot.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	outTree := ot.h.access
	root := node(t, outTree, platform.RoleTree, "")
	branch := queryItem(t, outTree, platform.AccessQuery{Container: root.ID, Index: 9000})
	if branch.Level != 1 || branch.States&platform.AccessExpandable == 0 {
		t.Fatalf("virtual outline branch: %+v", branch)
	}
	ot.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: branch.ID, Item: branch.Item, Action: platform.AccessExpand})
	if !outline.Open.Has(9000) || node(t, ot.h.access, platform.RoleTree, "").Collection.Rows != 10_001 {
		t.Fatal("offscreen branch did not expand")
	}
	child := queryItem(t, outTree, platform.AccessQuery{Container: root.ID, Index: 9001})
	if child.Level != 2 || child.Item.Key != 20_000 {
		t.Fatalf("outline child: %+v", child)
	}
}

func TestCollectionAccessibilitySectionSpanAndEmpty(t *testing.T) {
	state := ListState{Header: func(i int) bool { return i == 100 }}
	tt := NewTester(func(c *Context) {
		Table(c, &state, []TableColumn{{Title: "A"}, {Title: "B"}}, 200, func(r, col int) { Textf(c, "%d/%d", r, col) }).Grow(1)
	}, 300, 180)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	table := node(t, tree, platform.RoleTable, "")
	n := queryItem(t, tree, platform.AccessQuery{Kind: platform.AccessQueryCell, Container: table.ID, Row: 100, Column: 1})
	if n.Cell.Column != 0 || n.Cell.ColumnSpan != 2 || len(n.Cell.ColumnHeaders) != 2 {
		t.Fatalf("section span: %+v", n.Cell)
	}
	r := queryItem(t, tree, platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: n.Item, Realize: true})
	if r.ID != n.ID || r.Cell.ColumnSpan != 2 {
		t.Fatalf("realized span: %+v", r.Cell)
	}
	empty := NewTester(func(c *Context) { Table(c, nil, []TableColumn{{Title: "A"}}, 0, func(r, col int) {}).Grow(1) }, 300, 180)
	empty.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	e := node(t, empty.h.access, platform.RoleTable, "")
	if e.Collection.Rows != 0 || e.Collection.Columns != 1 {
		t.Fatalf("empty counts: %+v", e.Collection)
	}
	if _, ok := empty.h.access.Query(platform.AccessQuery{Container: e.ID, Index: 0}); ok {
		t.Fatal("empty collection returned an item")
	}
}

func TestCollectionAccessibilityAnonymousColumnsAndGridKeys(t *testing.T) {
	tt := NewTester(func(c *Context) {
		Table(c, nil, []TableColumn{{}, {Title: ""}}, 5, func(r, col int) { Textf(c, "%d/%d", r, col) }).Grow(1)
	}, 300, 180)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	table := node(t, tt.h.access, platform.RoleTable, "")
	a := queryItem(t, tt.h.access, platform.AccessQuery{Kind: platform.AccessQueryCell, Container: table.ID, Row: 0, Column: 0})
	b := queryItem(t, tt.h.access, platform.AccessQuery{Kind: platform.AccessQueryCell, Container: table.ID, Row: 0, Column: 1})
	if a.ID == b.ID || a.Cell.ColumnHeaders[0] == b.Cell.ColumnHeaders[0] {
		t.Fatal("anonymous columns share an identity")
	}
	items := []string{"a", "b", "c", "d", "e"}
	selected := 1
	state := GridState{Selected: &selected, Key: func(i int) any { return items[i] }, Index: func(key any) int { return slices.Index(items, key.(string)) }}
	gt := NewTester(func(c *Context) { GridView(c, &state, len(items), 100, 60, func(i int) { Text(c, items[i]) }).Grow(1) }, 448, 180)
	gt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	grid := node(t, gt.h.access, platform.RoleList, "")
	item := queryItem(t, gt.h.access, platform.AccessQuery{Container: grid.ID, Index: 1})
	items = []string{"x", "a", "c", "d", "e", "b"}
	gt.Frame()
	moved := queryItem(t, gt.h.access, platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: item.Item})
	if selected != 5 || moved.ID != item.ID || moved.Item.Index != 5 {
		t.Fatalf("grid key after reorder: cursor %d, item %+v", selected, moved)
	}
	items = items[:5]
	gt.Frame()
	if selected != -1 {
		t.Fatal("grid cursor did not leave a removed item")
	}
}

func TestCollectionAccessibilityDoesNotEnumerateSelectionOnFrames(t *testing.T) {
	selected := -1
	var selection Selection[int]
	for i := range 10_000 {
		selection.Add(i)
	}
	reads := 0
	state := ListState{Selected: &selected, Selection: &selection, Key: func(i int) any { reads++; return i }, Index: func(key any) int { return key.(int) }}
	tt := NewTester(func(c *Context) { List(c, &state, 1_000_000, func(i int) { Textf(c, "Row %d", i).Height(30) }).Grow(1) }, 300, 180)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	reads = 0
	tt.Frame()
	if reads > 200 {
		t.Fatalf("frame enumerated the selection: %d key reads", reads)
	}
}
