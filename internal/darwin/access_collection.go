//go:build darwin

package darwin

import (
	"math"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

// NSArray's primitives let AX clients page through a million rows without
// allocating a million objects. It retains its native owner, avoiding
// address reuse, but holds no Go closure. After disposal its count is zero.
type accessArray struct {
	owner           id
	cells, children bool
}

var accessArrays = map[id]accessArray{}

func registerAccessArray() {
	classDef("MyGoAccessibilityArray", "NSArray", nil, []objc.MethodDef{
		method("init", func(self id, _ objc.SEL) id { return self }),
		method("count", func(self id, _ objc.SEL) uint {
			a := accessArrays[self]
			el := theBackend.accessElementOf(a.owner)
			if el == nil {
				return 0
			}
			if el.gridOwner != 0 {
				if owner := el.s.elements[el.gridOwner]; owner != nil && owner.node.Collection != nil {
					c := owner.node.Collection
					return uint(min(c.Columns, max(0, c.Items-el.gridRow*c.Columns)))
				}
				return 0
			}
			if a.cells {
				if c := el.s.elements[el.node.Item.Container]; c != nil && c.node.Collection != nil {
					return uint(c.node.Collection.Columns)
				}
				return 0
			}
			if c := el.node.Collection; c != nil {
				count := c.Items
				if el.isGrid() {
					count = c.Rows
				}
				if a.children && len(c.ColumnHeaders) > 0 {
					count++
				}
				return uint(count)
			}
			return 0
		}),
		method("objectAtIndex:", func(self id, _ objc.SEL, index uint) id {
			a := accessArrays[self]
			el := theBackend.accessElementOf(a.owner)
			if el == nil {
				return 0
			}
			q := platform.AccessQuery{Container: el.node.ID, Index: int(index)}
			if el.gridOwner != 0 {
				return el.query(platform.AccessQuery{Kind: platform.AccessQueryCell, Container: el.gridOwner, Row: el.gridRow, Column: int(index)})
			}
			if el.isGrid() {
				return el.gridRowElement(int(index))
			}
			if a.cells {
				if el.node.Item == nil {
					return 0
				}
				q = platform.AccessQuery{Kind: platform.AccessQueryCell, Container: el.node.Item.Container, Row: el.node.Item.Index, Column: int(index)}
			} else if a.children && el.node.Collection != nil && len(el.node.Collection.ColumnHeaders) > 0 {
				if index == 0 {
					if len(el.children) > 0 {
						return el.s.elements[el.children[0]].obj
					}
					return 0
				}
				q.Index--
			}
			return el.query(q)
		}),
		method("copyWithZone:", func(self id, _ objc.SEL, zone uintptr) id { return retain(self) }),
		method("dealloc", func(self id, cmd objc.SEL) {
			owner := accessArrays[self].owner
			delete(accessArrays, self)
			release(owner)
			sendSuper(self, "MyGoAccessibilityArray", cmd)
		}),
	})
}

func lazyAccessArray(el *accessElement, cells, children bool) id {
	obj := send(send(class("MyGoAccessibilityArray"), "alloc"), "init")
	accessArrays[obj] = accessArray{retain(el.obj), cells, children}
	return send(obj, "autorelease")
}

func (el *accessElement) isGrid() bool {
	return el.node.Collection != nil && el.node.Collection.Grid && el.node.Role == platform.RoleList
}

func (el *accessElement) gridRowElement(row int) id {
	c := el.node.Collection
	if c == nil || row < 0 || row >= c.Rows {
		return 0
	}
	n := platform.AccessNode{ID: el.node.ID ^ (0x67726964726f7700 + uint64(row)), Role: platform.RoleRow, PosInSet: row + 1, SetSize: c.Rows}
	for _, cell := range el.s.access.Nodes {
		if cell.Cell != nil && cell.Cell.Table == el.node.ID && cell.Cell.Row == row {
			b := cell.Bounds
			if n.Bounds.W == 0 {
				n.Bounds = b
			} else {
				n.Bounds.W = max(n.Bounds.X+n.Bounds.W, b.X+b.W) - min(n.Bounds.X, b.X)
				n.Bounds.X = min(n.Bounds.X, b.X)
				n.Bounds.H = max(n.Bounds.H, b.H)
			}
		}
	}
	r := el.s.ensureAccess(n)
	r.gridOwner, r.gridRow = el.node.ID, row
	send(r.obj, "setAccessibilityParent:", uintptr(el.obj))
	return r.obj
}

func (s *surface) ensureAccess(n platform.AccessNode) *accessElement {
	el := s.elements[n.ID]
	if el == nil {
		el = &accessElement{obj: send(send(class("MyGoAccessibilityElement"), "alloc"), "init"), s: s, owned: true}
		s.elements[n.ID], s.w.b.byAccess[el.obj] = el, el
		el.apply(n, true)
	} else {
		el.apply(n, false)
	}
	el.queried = true
	if n.Item != nil && n.Cell != nil && !n.Item.Cell {
		if owner := s.elements[n.Item.Container]; owner != nil && owner.isGrid() {
			send(el.obj, "setAccessibilityRole:", uintptr(nsString("AXCell")))
			send(el.obj, "setAccessibilitySubrole:", 0)
		}
	}
	if n.Item != nil {
		parent := s.elements[n.Item.Container]
		if n.Item.Cell && s.access.Query != nil {
			if row, ok := s.access.Query(platform.AccessQuery{Container: n.Item.Container, Index: n.Item.Index}); ok {
				parent = s.ensureAccess(row)
			}
		}
		if parent != nil {
			el.parent = parent.obj
			send(el.obj, "setAccessibilityParent:", uintptr(parent.obj))
		}
	}
	return el
}

func (el *accessElement) query(q platform.AccessQuery) id {
	if el.s.w.closed || el.s.access.Query == nil {
		return 0
	}
	if n, ok := el.s.access.Query(q); ok {
		return el.s.ensureAccess(n).obj
	}
	return 0
}

func (el *accessElement) accessHeaders(ids []uint64) id {
	var headers []id
	for _, nid := range ids {
		if h := el.s.elements[nid]; h != nil {
			headers = append(headers, h.obj)
		}
	}
	return nsArray(headers...)
}

func accessCollectionMethods() []objc.MethodDef {
	element := func(self id) *accessElement { return theBackend.accessElementOf(self) }
	methods := []objc.MethodDef{
		method("accessibilityParent", func(self id, _ objc.SEL) id {
			el := element(self)
			if el == nil {
				return 0
			}
			if ref := el.node.Item; ref != nil {
				owner := el.s.elements[ref.Container]
				if owner == nil {
					return 0
				}
				if owner.isGrid() && el.node.Cell != nil {
					return owner.gridRowElement(el.node.Cell.Row)
				}
				if ref.Cell {
					return el.query(platform.AccessQuery{Container: ref.Container, Index: ref.Index})
				}
				return owner.obj
			}
			if el.gridOwner != 0 {
				if owner := el.s.elements[el.gridOwner]; owner != nil {
					return owner.obj
				}
				return 0
			}
			return el.parent
		}),
		method("accessibilityRows", func(self id, cmd objc.SEL) id {
			if el := element(self); el != nil && el.node.Collection != nil {
				return lazyAccessArray(el, false, false)
			}
			return sendSuper(self, "MyGoAccessibilityElement", cmd)
		}),
		method("accessibilityChildren", func(self id, cmd objc.SEL) id {
			el := element(self)
			if el != nil {
				if el.gridOwner != 0 {
					return lazyAccessArray(el, true, false)
				}
				if el.node.Collection != nil {
					return lazyAccessArray(el, false, true)
				}
				if ref := el.node.Item; ref != nil && !ref.Cell && el.node.Role != platform.RoleListItem {
					if c := el.s.elements[ref.Container]; c != nil && c.node.Collection != nil && len(c.node.Collection.ColumnHeaders) > 0 {
						return lazyAccessArray(el, true, false)
					}
				}
			}
			return sendSuper(self, "MyGoAccessibilityElement", cmd)
		}),
		method("accessibilityRowCount", func(self id, _ objc.SEL) int {
			if el := element(self); el != nil && el.node.Collection != nil {
				c := el.node.Collection
				if el.node.Role == platform.RoleList && !c.Grid {
					return c.Items
				}
				return c.Rows
			}
			return 0
		}),
		method("accessibilityArrayAttributeCount:", func(self id, cmd objc.SEL, attribute id) uint {
			name := stringOf(attribute)
			if el := element(self); el != nil && (name == "AXRows" && el.node.Collection != nil || name == "AXChildren" && (el.node.Collection != nil || el.gridOwner != 0 || el.node.Item != nil && !el.node.Item.Cell && el.node.Role != platform.RoleListItem)) {
				array := send(self, "accessibilityRows")
				if name == "AXChildren" {
					array = send(self, "accessibilityChildren")
				}
				return uint(sendInt(array, "count"))
			}
			return uint(sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(attribute)))
		}),
		method("accessibilityArrayAttributeValues:index:maxCount:", func(self id, cmd objc.SEL, attribute id, index, count uint) id {
			name := stringOf(attribute)
			if el := element(self); el != nil && el.node.Collection != nil && (name == "AXRows" || name == "AXChildren") {
				array := send(self, "accessibilityRows")
				if name == "AXChildren" {
					array = send(self, "accessibilityChildren")
				}
				size := uint(sendInt(array, "count"))
				if index >= size {
					return nsArray()
				}
				var items []id
				for j := uint(0); j < min(count, size-index); j++ {
					items = append(items, send(array, "objectAtIndex:", uintptr(index+j)))
				}
				return nsArray(items...)
			}
			return sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(attribute), uintptr(index), uintptr(count))
		}),
		method("accessibilityVisibleCells", func(self id, _ objc.SEL) id {
			if el := element(self); el != nil && el.node.Collection != nil {
				var cells []id
				for _, n := range el.s.access.Nodes {
					if n.Cell != nil && n.Cell.Table == el.node.ID && n.States&platform.AccessOffscreen == 0 {
						cells = append(cells, el.s.elements[n.ID].obj)
					}
				}
				return nsArray(cells...)
			}
			return nsArray()
		}),
		method("accessibilityVisibleRows", func(self id, cmd objc.SEL) id {
			if el := element(self); el != nil && el.isGrid() {
				var rows []id
				seen := map[int]bool{}
				for _, n := range el.s.access.Nodes {
					if n.Cell != nil && n.Cell.Table == el.node.ID && n.States&platform.AccessOffscreen == 0 && !seen[n.Cell.Row] {
						seen[n.Cell.Row] = true
						rows = append(rows, el.gridRowElement(n.Cell.Row))
					}
				}
				return nsArray(rows...)
			}
			return sendSuper(self, "MyGoAccessibilityElement", cmd)
		}),
		method("accessibilitySelectedCells", func(self id, _ objc.SEL) id {
			if el := element(self); el != nil && el.isGrid() && el.s.access.Selection != nil {
				var cells []id
				for _, n := range el.s.access.Selection(el.node.ID) {
					cells = append(cells, el.s.ensureAccess(n).obj)
				}
				return nsArray(cells...)
			}
			return nsArray()
		}),
		method("accessibilityColumnCount", func(self id, _ objc.SEL) int {
			if el := element(self); el != nil && el.node.Collection != nil {
				return el.node.Collection.Columns
			}
			return 0
		}),
		method("accessibilityCellForColumn:row:", func(self id, _ objc.SEL, column, row int) id {
			if el := element(self); el != nil {
				return el.query(platform.AccessQuery{Kind: platform.AccessQueryCell, Container: el.node.ID, Row: row, Column: column})
			}
			return 0
		}),
		method("accessibilityRowIndexRange", func(self id, _ objc.SEL) nsRange {
			if el := element(self); el != nil && el.node.Cell != nil {
				return nsRange{Location: uint(el.node.Cell.Row), Length: uint(el.node.Cell.RowSpan)}
			}
			return nsRange{}
		}),
		method("accessibilityColumnIndexRange", func(self id, _ objc.SEL) nsRange {
			if el := element(self); el != nil && el.node.Cell != nil {
				return nsRange{Location: uint(el.node.Cell.Column), Length: uint(el.node.Cell.ColumnSpan)}
			}
			return nsRange{}
		}),
		method("accessibilityColumnHeaderUIElements", func(self id, _ objc.SEL) id {
			if el := element(self); el != nil {
				if el.node.Collection != nil {
					return el.accessHeaders(el.node.Collection.ColumnHeaders)
				}
				if el.node.Cell != nil {
					return el.accessHeaders(el.node.Cell.ColumnHeaders)
				}
			}
			return nsArray()
		}),
		method("accessibilityRowHeaderUIElements", func(self id, _ objc.SEL) id {
			if el := element(self); el != nil && el.node.Cell != nil {
				return el.accessHeaders(el.node.Cell.RowHeaders)
			}
			return nsArray()
		}),
		method("accessibilitySelectedRows", func(self id, cmd objc.SEL) id {
			if el := element(self); el != nil && el.node.Collection != nil && el.s.access.Selection != nil {
				var rows []id
				selection := el.s.access.Selection(el.node.ID)
				if el.isGrid() {
					counts := map[int]int{}
					for _, n := range selection {
						counts[n.Cell.Row]++
					}
					c := el.node.Collection
					for row, n := range counts {
						if n == min(c.Columns, c.Items-row*c.Columns) {
							rows = append(rows, el.gridRowElement(row))
						}
					}
				} else {
					for _, n := range selection {
						rows = append(rows, el.s.ensureAccess(n).obj)
					}
				}
				return nsArray(rows...)
			}
			return sendSuper(self, "MyGoAccessibilityElement", cmd)
		}),
		method("setAccessibilitySelectedRows:", func(self id, cmd objc.SEL, rows id) {
			el := element(self)
			if el == nil || el.s.updating || el.node.Collection == nil {
				sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(rows))
				return
			}
			if el.isGrid() {
				return
			}
			el.act(platform.AccessClearSelection, "")
			for i := 0; i < sendInt(rows, "count"); i++ {
				if row := element(send(rows, "objectAtIndex:", uintptr(i))); row != nil && row.node.Item != nil && row.node.Item.Container == el.node.ID {
					a := platform.AccessSelect
					if el.node.States&platform.AccessMultiselectable != 0 {
						a = platform.AccessAddToSelection
					}
					row.act(a, "")
				}
			}
		}),
		method("setAccessibilitySelectedCells:", func(self id, cmd objc.SEL, cells id) {
			el := element(self)
			if el == nil || el.s.updating || !el.isGrid() {
				sendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(cells))
				return
			}
			el.act(platform.AccessClearSelection, "")
			for i := 0; i < sendInt(cells, "count"); i++ {
				if cell := element(send(cells, "objectAtIndex:", uintptr(i))); cell != nil && cell.node.Item != nil && cell.node.Item.Container == el.node.ID {
					action := platform.AccessSelect
					if el.node.States&platform.AccessMultiselectable != 0 {
						action = platform.AccessAddToSelection
					}
					cell.act(action, "")
				}
			}
		}),
	}
	for axis, name := range []string{"accessibilityHorizontalScrollBar", "accessibilityVerticalScrollBar"} {
		methods = append(methods, method(name, func(self id, _ objc.SEL) id {
			if el := element(self); el != nil {
				return el.scrollBar(axis)
			}
			return 0
		}))
	}
	for i, name := range []string{"accessibilityPerformScrollLeft", "accessibilityPerformScrollRight", "accessibilityPerformScrollUp", "accessibilityPerformScrollDown"} {
		methods = append(methods, method(name, func(self id, _ objc.SEL) bool {
			el := element(self)
			if el == nil || el.node.Scroll == nil || el.node.States&platform.AccessDisabled != 0 {
				return false
			}
			s := el.node.Scroll
			dx, dy := 0.0, 0.0
			switch i {
			case 0:
				dx = -s.Width
			case 1:
				dx = s.Width
			case 2:
				dy = -s.Height
			case 3:
				dy = s.Height
			}
			if dx != 0 && s.MaxX() == 0 || dy != 0 && s.MaxY() == 0 {
				return false
			}
			el.s.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: el.node.ID, Action: platform.AccessScrollBy, DX: dx, DY: dy})
			return true
		}))
	}
	return methods
}

func (el *accessElement) scrollBar(axis int) id {
	s := el.node.Scroll
	if s == nil {
		return 0
	}
	maximum, offset := s.MaxX(), s.X
	if axis == 1 {
		maximum, offset = s.MaxY(), s.Y
	}
	if maximum <= 0 {
		return 0
	}
	n := platform.AccessNode{ID: el.node.ID ^ (0x7363726f6c6c0000 + uint64(axis)), Role: platform.RoleSlider,
		Max: 1, Now: offset / maximum, Actions: platform.ActionSetValue | platform.ActionIncrement | platform.ActionDecrement, States: el.node.States & platform.AccessDisabled}
	bar := el.s.ensureAccess(n)
	bar.scrollOwner, bar.scrollAxis = el.node.ID, axis
	send(bar.obj, "setAccessibilityRole:", uintptr(nsString("AXScrollBar")))
	orient := 2
	if axis == 1 {
		orient = 1
	}
	send(bar.obj, "setAccessibilityOrientation:", uintptr(orient))
	send(bar.obj, "setAccessibilityParent:", uintptr(el.obj))
	return bar.obj
}

func (el *accessElement) scrollStep(action platform.AccessActionKind) bool {
	owner := el.s.elements[el.scrollOwner]
	if owner == nil || owner.node.Scroll == nil || owner.node.States&platform.AccessDisabled != 0 {
		return false
	}
	d := 40.0
	if action == platform.AccessDecrement {
		d = -d
	}
	ev := platform.SurfaceEvent{Kind: platform.AccessAction, ID: owner.node.ID, Action: platform.AccessScrollBy}
	if el.scrollAxis == 0 {
		ev.DX = d
	} else {
		ev.DY = d
	}
	el.s.send(ev)
	return true
}

func (el *accessElement) scrollValue(value id) {
	owner := el.s.elements[el.scrollOwner]
	if owner == nil || owner.node.Scroll == nil || owner.node.States&platform.AccessDisabled != 0 {
		return
	}
	v := msgFloat(value, sel("doubleValue"))
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
		return
	}
	ev := platform.SurfaceEvent{Kind: platform.AccessAction, ID: owner.node.ID, Action: platform.AccessScrollTo, X: -1, Y: -1}
	if el.scrollAxis == 0 {
		ev.X = v * owner.node.Scroll.MaxX()
	} else {
		ev.Y = v * owner.node.Scroll.MaxY()
	}
	el.s.send(ev)
}
