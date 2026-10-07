package ui

import (
	"math"
	"slices"

	"github.com/egoist/mygo/internal/platform"
)

// collectionFrame describes a virtual collection without building its items.
// Only the current frame is reachable by provider queries.
type collectionFrame struct {
	list       *listFrame
	grid       *GridState
	n, cols    int
	columns    []TableColumn
	columnKeys []any
	order      []int
	headers    []*Element
	outline    func(int, *platform.AccessNode)
}

func (f *collectionFrame) key(i int) any {
	if f.grid != nil {
		return f.grid.key(i)
	}
	return f.list.key(i)
}

func (f *collectionFrame) index(key any, hint int) (int, bool) {
	if f.grid == nil {
		if f.list.s.Key == nil {
			i, ok := key.(int)
			return i, ok && i >= 0 && i < f.n
		}
		return f.list.s.find(key, hint, f.n)
	}
	s := f.grid
	if s.Key == nil {
		i, ok := key.(int)
		return i, ok && i >= 0 && i < f.n
	}
	if s.Index != nil {
		i := s.Index(key)
		return i, i >= 0 && i < f.n && s.key(i) == key
	}
	for d := 0; d <= listSearch; d++ {
		for _, i := range []int{hint - d, hint + d} {
			if i >= 0 && i < f.n && s.key(i) == key {
				return i, true
			}
		}
	}
	return 0, false
}

func (f *collectionFrame) itemID(i int) uint64 {
	parent := f.list.e.id
	if f.grid != nil {
		return gridItemID(f.list.owner.id, f.key(i))
	}
	return keyedID(parent, f.key(i))
}

func gridItemID(parent uint64, key any) uint64 { return keyedID(mix(parent, 0x677269646974656d), key) }

func (f *collectionFrame) description() *platform.AccessCollection {
	d := &platform.AccessCollection{Rows: f.list.n, Columns: f.cols, Items: f.n, Grid: f.list.flat || f.grid != nil}
	for _, h := range f.headers {
		d.ColumnHeaders = append(d.ColumnHeaders, h.id)
	}
	sel, cursor := f.list.s.Selection, f.list.s.cursor()
	if f.grid != nil {
		sel, cursor = f.grid.Selection, f.grid.Selected
	}
	if sel != nil {
		d.SelectionVersion, d.SelectionSize = sel.version(), sel.size()
	} else if cursor != nil && *cursor >= 0 && *cursor < f.n {
		d.SelectionVersion, d.SelectionSize = f.itemID(*cursor), 1
	}
	return d
}

func scrollDescription(e *Element) *platform.AccessScroll {
	s := e.st
	return &platform.AccessScroll{X: s.scrollX, Y: s.scrollY, Width: float64(e.w), Height: float64(e.h), ContentWidth: e.contentW, ContentHeight: e.contentH}
}

func (rt *engine) collectionDetails(e *Element, n *platform.AccessNode) {
	if f := e.collection; f != nil {
		rt.collections[e.id] = f
		n.Collection, n.Scroll = f.description(), scrollDescription(f.list.e)
		if !e.IsDisabled() {
			n.Actions |= platform.ActionScroll
		}
	} else if e.scrolls() {
		n.Scroll = scrollDescription(e)
		if !e.IsDisabled() {
			n.Actions |= platform.ActionScroll
		}
	}
	var f *collectionFrame
	i := e.rowIndex
	switch {
	case e.listRow && e.parent.list != nil && !e.parent.list.grid:
		f = e.parent.list.owner.collection
	case e.gridItem:
		i = e.itemIndex
		for p := e.parent; p != nil; p = p.parent {
			if p.collection != nil {
				f = p.collection
				break
			}
		}
	case e.collectionCell:
		for p := e.parent; p != nil; p = p.parent {
			if p.listRow {
				i, f = p.rowIndex, p.parent.list.owner.collection
				break
			}
		}
	}
	if f == nil {
		return
	}
	n.Item = &platform.AccessItem{Container: f.list.owner.id, Index: i, Key: f.key(i), Cell: e.collectionCell, ColumnKey: e.cellColumn}
	if e.collectionCell || e.gridItem {
		row, col, span := i, e.cellIndex, 1
		if e.gridItem {
			row, col = i/f.cols, i%f.cols
		}
		if f.list.isHeader(i) {
			col, span = 0, f.cols
		}
		n.Cell = &platform.AccessCell{Table: f.list.owner.id, Row: row, Column: col, RowSpan: 1, ColumnSpan: span}
		for j := col; j < min(col+span, len(f.headers)); j++ {
			n.Cell.ColumnHeaders = append(n.Cell.ColumnHeaders, f.headers[j].id)
		}
		if n.Label == "" {
			n.Label = e.innerText()
		}
	}
}

func (rt *engine) queryCollection(q platform.AccessQuery) (platform.AccessNode, bool) {
	if rt.accessClosed {
		return platform.AccessNode{}, false
	}
	if q.Kind == platform.AccessQueryResolve {
		if q.Item == nil {
			return platform.AccessNode{}, false
		}
		q.Container = q.Item.Container
	}
	f := rt.collections[q.Container]
	if f == nil {
		return platform.AccessNode{}, false
	}
	i, col := q.Index, -1
	switch q.Kind {
	case platform.AccessQueryCell:
		if q.Row < 0 || q.Row >= f.list.n || q.Column < 0 || q.Column >= f.cols {
			return platform.AccessNode{}, false
		}
		i, col = q.Row, q.Column
		if f.grid != nil {
			i, col = q.Row*f.cols+q.Column, -1
		}
	case platform.AccessQueryResolve:
		var ok bool
		id := keyedID(f.list.e.id, q.Item.Key)
		if f.grid != nil {
			id = gridItemID(f.list.owner.id, q.Item.Key)
		}
		if visible, found := rt.accessNodes[id]; found && visible.Item != nil {
			i, ok = visible.Item.Index, true
		} else {
			i, ok = f.index(q.Item.Key, q.Item.Index)
		}
		if !ok {
			return platform.AccessNode{}, false
		}
		if q.Item.Cell {
			if f.list.isHeader(i) && f.cols > 0 {
				col = 0
			} else {
				col = slices.IndexFunc(f.order, func(j int) bool { return f.columnKeys[j] == q.Item.ColumnKey })
			}
			if col < 0 {
				return platform.AccessNode{}, false
			}
		}
	}
	if i < 0 || i >= f.n {
		return platform.AccessNode{}, false
	}
	n := platform.AccessNode{ID: f.itemID(i), Parent: -1, Role: platform.RoleListItem,
		PosInSet: i + 1, SetSize: f.n, States: platform.AccessOffscreen | platform.AccessVirtualized,
		Actions: platform.ActionScrollIntoView | platform.ActionRealize,
		Item:    &platform.AccessItem{Container: q.Container, Index: i, Key: f.key(i)}}
	s := f.list.s
	if f.grid == nil {
		if f.list.flat {
			n.Role = platform.RoleRow
		}
		if f.list.tree {
			n.Role = platform.RoleTreeItem
		}
		if s.Label != nil {
			n.Label = s.Label(i)
		}
		if s.cursor() != nil && !f.list.isHeader(i) {
			n.States |= platform.AccessSelectable | platform.AccessFocusable
			n.Actions |= platform.ActionPress | platform.ActionFocus
			if f.list.chosen(i, f.key(i)) {
				n.States |= platform.AccessChecked
			}
		}
		if f.outline != nil {
			f.outline(i, &n)
		}
	} else {
		if f.grid.Label != nil {
			n.Label = f.grid.Label(i)
		}
		if f.grid.Selected != nil {
			n.States |= platform.AccessSelectable | platform.AccessFocusable
			n.Actions |= platform.ActionPress | platform.ActionFocus
			if f.grid.chosen(i) {
				n.States |= platform.AccessChecked
			}
		}
		n.Cell = &platform.AccessCell{Table: q.Container, Row: i / f.cols, Column: i % f.cols, RowSpan: 1, ColumnSpan: 1}
	}
	if col >= 0 && f.list.flat {
		span := 1
		if f.list.isHeader(i) {
			col, span = 0, f.cols
		}
		column := &f.columns[f.order[col]]
		n.ID = keyedID(mix(n.ID, 0), f.columnKeys[f.order[col]])
		if span > 1 || f.list.isHeader(i) {
			n.ID = mix(f.itemID(i), 0)
		}
		n.Role, n.PosInSet, n.SetSize = platform.RoleCell, 0, 0
		n.States &^= platform.AccessSelectable | platform.AccessChecked | platform.AccessFocusable
		n.Actions &^= platform.ActionPress | platform.ActionFocus
		n.Item.Cell, n.Item.ColumnKey = true, f.columnKeys[f.order[col]]
		n.Cell = &platform.AccessCell{Table: q.Container, Row: i, Column: col, RowSpan: 1, ColumnSpan: span}
		for j := col; j < min(col+span, len(f.headers)); j++ {
			n.Cell.ColumnHeaders = append(n.Cell.ColumnHeaders, f.headers[j].id)
		}
		if column.AccessibilityLabel != nil {
			n.Label = column.AccessibilityLabel(i)
		} else {
			n.Label = ""
		}
	}
	if live, ok := rt.accessNodes[n.ID]; ok {
		n = live
	}
	if f.list.owner.IsDisabled() {
		n.States |= platform.AccessDisabled
		n.Actions = 0
	}
	if q.Realize && n.States&platform.AccessDisabled == 0 {
		if rt.inFrame {
			return platform.AccessNode{}, false
		}
		ref := n.Item
		f.scrollIntoView(i)
		rt.runFrame()
		live, ok := rt.queryCollection(platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: ref})
		if ok && live.Cell != nil {
			// Reveal a horizontally clipped table cell as well as its row.
			rt.reveal(live.ID)
			rt.runFrame()
			return rt.queryCollection(platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: ref})
		}
		return live, ok
	}
	return n, true
}

func (f *collectionFrame) scrollIntoView(i int) {
	if f.grid != nil {
		f.grid.ScrollIntoView(i)
	} else {
		f.list.s.ScrollIntoView(i)
	}
}

func (rt *engine) collectionSelection(id uint64) []platform.AccessNode {
	f := rt.collections[id]
	if f == nil || rt.accessClosed {
		return nil
	}
	s, sel, cursor := f.list.s, f.list.s.Selection, f.list.s.cursor()
	if f.grid != nil {
		sel, cursor = f.grid.Selection, f.grid.Selected
	}
	var nodes []platform.AccessNode
	add := func(key any, hint int) {
		if i, ok := f.index(key, hint); ok {
			if n, ok := rt.queryCollection(platform.AccessQuery{Container: id, Index: i}); ok && n.States&platform.AccessChecked != 0 {
				nodes = append(nodes, n)
			}
		}
	}
	if sel != nil {
		for key := range sel.all() {
			hint := 0
			if cursor != nil {
				hint = max(0, *cursor)
			}
			if f.grid == nil && s.Key != nil && s.Index == nil {
				for _, r := range s.rows {
					if r.key == key {
						hint = r.row
						break
					}
				}
			} else if f.grid != nil && f.grid.Key != nil && f.grid.Index == nil {
				for _, i := range f.grid.cells {
					if f.key(i) == key {
						hint = i
						break
					}
				}
			}
			add(key, hint)
		}
	} else if cursor != nil && *cursor >= 0 && *cursor < f.n {
		add(f.key(*cursor), *cursor)
	}
	slices.SortFunc(nodes, func(a, b platform.AccessNode) int { return a.Item.Index - b.Item.Index })
	return nodes
}

// collectionAction also handles placeholders: actions resolve a key first,
// and never act on the item that replaced it at an old index.
func (rt *engine) collectionAction(ev platform.SurfaceEvent) bool {
	ref := ev.Item
	if ref == nil {
		if n, ok := rt.accessNodes[ev.ID]; ok {
			ref = n.Item
		}
	}
	id := ev.ID
	if ref != nil {
		id = ref.Container
	}
	f := rt.collections[id]
	if f == nil || f.list.owner.IsDisabled() {
		return false
	}
	i := -1
	if ref != nil {
		n, ok := rt.queryCollection(platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: ref})
		if !ok {
			return true
		}
		ref, i = n.Item, n.Item.Index
	}
	selectable := f.list.s.cursor() != nil
	if f.grid != nil {
		selectable = f.grid.Selected != nil
	}
	switch ev.Action {
	case platform.AccessRealize, platform.AccessScrollIntoView:
		if i < 0 {
			return true
		}
		f.scrollIntoView(i)
		if _, ok := rt.states[ev.ID]; ok {
			rt.reveal(ev.ID)
		}
	case platform.AccessExpand, platform.AccessCollapse:
		if i < 0 || !f.list.tree || ref.Cell {
			return true
		}
		n, ok := rt.queryCollection(platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: ref, Realize: true})
		if !ok || n.States&platform.AccessExpandable == 0 {
			return true
		}
		if st := rt.states[n.ID]; st != nil {
			st.expand = 1
			if ev.Action == platform.AccessCollapse {
				st.expand = -1
			}
		}
	case platform.AccessPress, platform.AccessFocus, platform.AccessSelect, platform.AccessAddToSelection, platform.AccessRemoveFromSelection:
		if i < 0 || !selectable || ref.Cell || f.grid == nil && f.list.isHeader(i) {
			return true
		}
		selectItem := ev.Action != platform.AccessRemoveFromSelection
		if ev.Action == platform.AccessAddToSelection || ev.Action == platform.AccessRemoveFromSelection {
			sel := f.list.s.Selection
			if f.grid != nil {
				sel = f.grid.Selection
			}
			if sel != nil {
				if sel.set(f.key(i), selectItem) {
					f.list.changed()
				}
				if selectItem {
					if f.grid != nil {
						*f.grid.Selected = i
						f.grid.pivot = i
					} else {
						f.list.lead(i, true)
					}
				}
			} else if ev.Action == platform.AccessRemoveFromSelection {
				cursor := f.list.s.cursor()
				if f.grid != nil {
					cursor = f.grid.Selected
				}
				if *cursor == i {
					*cursor = -1
					f.list.changed()
				}
			} else {
				return true
			}
		} else if f.grid != nil {
			f.grid.choose(f.list.owner, i)
		} else {
			f.list.choose(i)
		}
		if selectItem {
			rt.focusOn(f.list.owner.st)
			f.scrollIntoView(i)
		}
	case platform.AccessClearSelection, platform.AccessSelectAll:
		sel, cursor := f.list.s.Selection, f.list.s.cursor()
		if f.grid != nil {
			sel, cursor = f.grid.Selection, f.grid.Selected
		}
		if sel != nil {
			changed := false
			if ev.Action == platform.AccessClearSelection {
				changed = sel.clear()
			} else {
				for j := range f.n {
					if f.grid != nil || !f.list.isHeader(j) {
						changed = sel.set(f.key(j), true) || changed
					}
				}
			}
			if changed {
				f.list.changed()
			}
		} else if cursor != nil && ev.Action == platform.AccessClearSelection && *cursor != -1 {
			*cursor = -1
			f.list.changed()
		}
	default:
		return false
	}
	rt.requestFrame()
	return true
}

func (rt *engine) accessScroll(ev platform.SurfaceEvent) bool {
	if ev.Action != platform.AccessScrollBy && ev.Action != platform.AccessScrollTo {
		return false
	}
	s := rt.states[ev.ID]
	if f := rt.collections[ev.ID]; f != nil {
		s = f.list.e.st
	}
	if s == nil || s.flags&flagDisabled != 0 {
		return true
	}
	x, y := s.scrollX, s.scrollY
	if ev.Action == platform.AccessScrollBy {
		x += ev.DX
		y += ev.DY
	} else {
		if ev.X >= 0 {
			x = ev.X
		}
		if ev.Y >= 0 {
			y = ev.Y
		}
	}
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return true
	}
	s.scrollTo(max(0, min(x, max(0, s.contentW-float64(s.w)))), max(0, min(y, max(0, s.contentH-float64(s.h)))))
	if f := rt.collections[ev.ID]; f != nil {
		// The latest explicit scroll supersedes a pending reveal requested
		// by selection before the next frame, as wheel input would.
		f.list.s.req = listRequest{set: true, offset: true, y: s.scrollY}
	}
	rt.requestFrame()
	return true
}
