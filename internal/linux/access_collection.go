//go:build linux && (amd64 || arm64)

package linux

import (
	"slices"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
)

var collectionScrollNames = map[platform.AccessActions][]byte{
	1 << 12: []byte("scroll left\x00"), 1 << 13: []byte("scroll right\x00"),
	1 << 14: []byte("scroll up\x00"), 1 << 15: []byte("scroll down\x00"),
}

func (an *accessNode) scrollActions() []platform.AccessActions {
	if an.n.Actions&platform.ActionScroll == 0 || an.n.Scroll == nil {
		return nil
	}
	var actions []platform.AccessActions
	if an.n.Scroll.MaxX() > 0 {
		actions = append(actions, 1<<12, 1<<13)
	}
	if an.n.Scroll.MaxY() > 0 {
		actions = append(actions, 1<<14, 1<<15)
	}
	return actions
}

func (an *accessNode) scrollAction(a platform.AccessActions) bool {
	if _, ok := collectionScrollNames[a]; !ok || an.n.Scroll == nil {
		return false
	}
	ev := platform.SurfaceEvent{Kind: platform.AccessAction, ID: an.n.ID, Action: platform.AccessScrollBy}
	s := an.n.Scroll
	switch a {
	case 1 << 12:
		ev.DX = -s.Width
	case 1 << 13:
		ev.DX = s.Width
	case 1 << 14:
		ev.DY = -s.Height
	case 1 << 15:
		ev.DY = s.Height
	}
	an.tree.s.send(ev)
	return true
}

// GObject's public ref_count follows the class pointer in its ABI.
func objectRefCount(obj ptr) uint32 {
	return *(*uint32)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&obj)), 8))
}

func (t *accessTree) ensure(n platform.AccessNode) *accessNode {
	an := t.nodes[n.ID]
	if an == nil {
		typ := accessType(n)
		an = &accessNode{obj: gObjectNew(typ, 0), typ: typ, tree: t}
		t.nodes[n.ID], accessObjects[an.obj] = an, an
	}
	an.n = n
	atkObjectSetName(an.obj, cs(n.Label))
	atkObjectSetRole(an.obj, an.role())
	if n.Item != nil {
		an.parent = t.nodes[n.Item.Container]
		if n.Item.Cell && t.source != nil && t.source.Query != nil {
			if row, ok := t.source.Query(platform.AccessQuery{Container: n.Item.Container, Index: n.Item.Index}); ok {
				an.parent = t.ensure(row)
			}
		}
		if an.parent != nil {
			atkObjectSetParent(an.obj, an.parent.obj)
		}
	}
	return an
}

func (an *accessNode) query(q platform.AccessQuery) *accessNode {
	if an.tree.source == nil || an.tree.source.Query == nil {
		return nil
	}
	if q.Container == 0 {
		q.Container = an.n.ID
	}
	if n, ok := an.tree.source.Query(q); ok {
		return an.tree.ensure(n)
	}
	return nil
}

func (an *accessNode) selectionNodes() []platform.AccessNode {
	if an.n.Collection != nil && an.tree.source != nil && an.tree.source.Selection != nil {
		return an.tree.source.Selection(an.n.ID)
	}
	var nodes []platform.AccessNode
	for _, child := range an.children {
		if chooses(child.n) && child.n.States&platform.AccessChecked != 0 {
			nodes = append(nodes, child.n)
		}
	}
	return nodes
}

func (an *accessNode) selectionAt(i int) *accessNode {
	if i < 0 {
		return nil
	}
	if nodes := an.selectionNodes(); i < len(nodes) {
		return an.tree.ensure(nodes[i])
	}
	return nil
}

func (an *accessNode) rowCollection() *platform.AccessCollection {
	if ref := an.n.Item; ref != nil && !ref.Cell && an.n.Role != platform.RoleListItem {
		if owner := an.tree.nodes[ref.Container]; owner != nil && owner.n.Collection != nil && len(owner.n.Collection.ColumnHeaders) > 0 {
			return owner.n.Collection
		}
	}
	return nil
}

func (an *accessNode) child(i int) *accessNode {
	if i < 0 {
		return nil
	}
	if c := an.n.Collection; c != nil {
		if len(c.ColumnHeaders) > 0 {
			if i == 0 {
				if len(an.children) > 0 {
					return an.children[0]
				}
				return nil
			}
			i--
		}
		return an.query(platform.AccessQuery{Index: i})
	}
	if an.rowCollection() != nil {
		return an.query(platform.AccessQuery{Kind: platform.AccessQueryCell, Container: an.n.Item.Container, Row: an.n.Item.Index, Column: i})
	}
	if i < len(an.children) {
		return an.children[i]
	}
	return nil
}

func initAccessCollections() {
	cb := purego.NewCallback
	node := func(obj ptr) *accessNode { return accessObjects[obj] }
	cell := func(obj ptr, row, col int32) *accessNode {
		if an := node(obj); an != nil {
			return an.query(platform.AccessQuery{Kind: platform.AccessQueryCell, Row: int(row), Column: int(col)})
		}
		return nil
	}
	refAt := cb(func(obj ptr, row, col int32) ptr {
		if c := cell(obj, row, col); c != nil {
			return gObjectRef(c.obj)
		}
		return 0
	})
	count := func(columns bool) ptr {
		return cb(func(obj ptr) int32 {
			if an := node(obj); an != nil && an.n.Collection != nil {
				n := an.n.Collection.Rows
				if columns {
					n = an.n.Collection.Columns
				}
				return int32(n)
			}
			return 0
		})
	}
	extent := func(columns bool) ptr {
		return cb(func(obj ptr, row, col int32) int32 {
			if c := cell(obj, row, col); c != nil && c.n.Cell != nil {
				n := c.n.Cell.RowSpan
				if columns {
					n = c.n.Cell.ColumnSpan
				}
				return int32(n)
			}
			return 0
		})
	}
	header := cb(func(obj ptr, column int32) ptr {
		if an := node(obj); an != nil && an.n.Collection != nil {
			hs := an.n.Collection.ColumnHeaders
			if column >= 0 && int(column) < len(hs) {
				if h := an.tree.nodes[hs[column]]; h != nil {
					return h.obj
				}
			}
		}
		return 0
	})
	selectedRows := cb(func(obj, output ptr) int32 {
		*slot(output, 0) = 0
		an := node(obj)
		if an == nil {
			return 0
		}
		rows := an.selectionNodes()
		var indices []int
		if c := an.n.Collection; c != nil && an.n.Role == platform.RoleList && c.Grid {
			counts := map[int]int{}
			for _, item := range rows {
				counts[item.Cell.Row]++
			}
			for row, n := range counts {
				if n == min(c.Columns, c.Items-row*c.Columns) {
					indices = append(indices, row)
				}
			}
			slices.Sort(indices)
		} else {
			for _, row := range rows {
				indices = append(indices, row.Item.Index)
			}
		}
		if len(indices) == 0 {
			return 0
		}
		mem := gMalloc0(uintptr(len(indices)) * 4)
		for i, row := range indices {
			setInt(mem+ptr(i)*4, row)
		}
		*slot(output, 0) = mem
		return int32(len(indices))
	})
	isRowSelected := cb(func(obj ptr, row int32) bool {
		if an := node(obj); an != nil {
			if c := an.n.Collection; c != nil && c.Grid && an.n.Role == platform.RoleList {
				if row < 0 || int(row) >= c.Rows {
					return false
				}
				for col := 0; col < min(c.Columns, c.Items-int(row)*c.Columns); col++ {
					n := an.query(platform.AccessQuery{Kind: platform.AccessQueryCell, Row: int(row), Column: col})
					if n == nil || n.n.States&platform.AccessChecked == 0 {
						return false
					}
				}
				return true
			}
			if n := an.query(platform.AccessQuery{Index: int(row)}); n != nil {
				return n.n.States&platform.AccessChecked != 0
			}
		}
		return false
	})
	isSelected := cb(func(obj ptr, row, col int32) bool {
		an := node(obj)
		if an == nil || an.n.Collection == nil || col < 0 || int(col) >= an.n.Collection.Columns {
			return false
		}
		q := platform.AccessQuery{Index: int(row)}
		if an.n.Role == platform.RoleList {
			q.Index = int(row)*an.n.Collection.Columns + int(col)
		}
		if n := an.query(q); n != nil {
			return n.n.States&platform.AccessChecked != 0
		}
		return false
	})
	chooseRow := func(remove bool) ptr {
		return cb(func(obj ptr, row int32) bool {
			an := node(obj)
			if an == nil {
				return false
			}
			if c := an.n.Collection; c != nil && c.Grid && an.n.Role == platform.RoleList {
				return false
			} // item selection, not row selection
			n := an.query(platform.AccessQuery{Index: int(row)})
			if n == nil || !chooses(n.n) {
				return false
			}
			a := platform.AccessSelect
			if an.n.States&platform.AccessMultiselectable != 0 {
				a = platform.AccessAddToSelection
			}
			if remove {
				a = platform.AccessRemoveFromSelection
			}
			n.act(a, "")
			return true
		})
	}
	indexAt := cb(func(obj ptr, row, col int32) int32 {
		if an := node(obj); an != nil && an.n.Collection != nil && row >= 0 && col >= 0 && int(row) < an.n.Collection.Rows && int(col) < an.n.Collection.Columns {
			return row*int32(an.n.Collection.Columns) + col
		}
		return -1
	})
	columnAt := cb(func(obj ptr, index int32) int32 {
		if an := node(obj); an != nil && an.n.Collection != nil && an.n.Collection.Columns > 0 && index >= 0 {
			return index % int32(an.n.Collection.Columns)
		}
		return -1
	})
	rowAt := cb(func(obj ptr, index int32) int32 {
		if an := node(obj); an != nil && an.n.Collection != nil && an.n.Collection.Columns > 0 && index >= 0 {
			return index / int32(an.n.Collection.Columns)
		}
		return -1
	})
	tableFns := map[int]ptr{0: refAt, 1: indexAt, 2: columnAt, 3: rowAt, 4: count(true), 5: count(false), 6: extent(true), 7: extent(false), 10: header, 21: selectedRows, 23: isRowSelected, 24: isSelected, 25: chooseRow(false), 26: chooseRow(true)}
	cbTableInit = cb(func(iface, data ptr) { setIface(iface, tableFns) })
	span := func(column bool) ptr {
		return cb(func(obj ptr) int32 {
			if an := node(obj); an != nil && an.n.Cell != nil {
				n := an.n.Cell.RowSpan
				if column {
					n = an.n.Cell.ColumnSpan
				}
				return int32(n)
			}
			return 0
		})
	}
	position := cb(func(obj, row, col ptr) bool {
		if an := node(obj); an != nil && an.n.Cell != nil {
			setInt(row, an.n.Cell.Row)
			setInt(col, an.n.Cell.Column)
			return true
		}
		return false
	})
	positionSpan := cb(func(obj, row, col, rs, cs ptr) bool {
		if an := node(obj); an != nil && an.n.Cell != nil {
			c := an.n.Cell
			setInt(row, c.Row)
			setInt(col, c.Column)
			setInt(rs, c.RowSpan)
			setInt(cs, c.ColumnSpan)
			return true
		}
		return false
	})
	unref, _ := purego.Dlsym(libGObject, "g_object_unref")
	headers := func(column bool) ptr {
		return cb(func(obj ptr) ptr {
			an := node(obj)
			if an == nil || an.n.Cell == nil {
				return 0
			}
			ids := an.n.Cell.RowHeaders
			if column {
				ids = an.n.Cell.ColumnHeaders
			}
			array := gPtrArrayNewWithFreeFunc(unref)
			for _, id := range ids {
				if h := an.tree.nodes[id]; h != nil {
					gPtrArrayAdd(array, gObjectRef(h.obj))
				}
			}
			return array
		})
	}
	getTable := cb(func(obj ptr) ptr {
		if an := node(obj); an != nil && an.n.Cell != nil {
			if t := an.tree.nodes[an.n.Cell.Table]; t != nil {
				return gObjectRef(t.obj)
			}
		}
		return 0
	})
	cellFns := map[int]ptr{0: span(true), 1: headers(true), 2: position, 3: span(false), 4: headers(false), 5: positionSpan, 6: getTable}
	cbTableCellInit = cb(func(iface, data ptr) { setIface(iface, cellFns) })
}
