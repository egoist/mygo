//go:build windows && (amd64 || arm64)

package windows

import (
	"math"
	"runtime"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
)

type collectionHandler struct {
	platform.WindowHandler
	events []platform.SurfaceEvent
}

func (h *collectionHandler) SurfaceEvent(ev platform.SurfaceEvent) bool {
	h.events = append(h.events, ev)
	return true
}

func collectionProvider(t *testing.T) (*uiaTree, *uiaElement, *collectionHandler) {
	t.Helper()
	uiaOnce.Do(initUIA)
	h := &collectionHandler{}
	tree := &uiaTree{s: &surface{w: &window{h: h}}, nodes: map[uint64]*uiaElement{}}
	tree.root = newUIAElement(tree)
	tree.root.root = true
	table := tree.ensure(platform.AccessNode{ID: 1, Role: platform.RoleTable, Collection: &platform.AccessCollection{Rows: 1_000_000, Columns: 2, Items: 1_000_000, Grid: true, ColumnHeaders: []uint64{2, 3}}, Scroll: &platform.AccessScroll{Width: 100, Height: 100, ContentWidth: 300, ContentHeight: 1100}, Actions: platform.ActionScroll})
	tree.ensure(platform.AccessNode{ID: 2, Role: platform.RoleColumnHeader, Label: "Name"})
	tree.ensure(platform.AccessNode{ID: 3, Role: platform.RoleColumnHeader, Label: "Size"})
	t.Cleanup(func() {
		for _, e := range tree.nodes {
			e.disconnect()
		}
		tree.root.disconnect()
		tree.source = nil
	})
	return tree, table, h
}

func TestUIACollectionProviders(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tree, table, _ := collectionProvider(t)
	realized := false
	tree.source = &platform.AccessTree{Query: func(q platform.AccessQuery) (platform.AccessNode, bool) {
		index := q.Index
		if q.Kind == platform.AccessQueryCell {
			index = q.Row
		}
		if q.Item != nil {
			index = q.Item.Index
		}
		if index < 0 || index >= 1_000_000 {
			return platform.AccessNode{}, false
		}
		n := platform.AccessNode{ID: uint64(100 + index), Role: platform.RoleRow, Item: &platform.AccessItem{Container: 1, Index: index, Key: index}}
		if q.Kind == platform.AccessQueryCell || q.Item != nil && q.Item.Cell {
			if q.Kind == platform.AccessQueryCell && q.Column != 1 {
				return platform.AccessNode{}, false
			}
			n.ID = uint64(2_000_000 + index)
			n.Role = platform.RoleCell
			n.Item.Cell = true
			n.Cell = &platform.AccessCell{Table: 1, Row: index, Column: 1, RowSpan: 1, ColumnSpan: 1, ColumnHeaders: []uint64{3}}
			n.States = platform.AccessVirtualized | platform.AccessOffscreen
			if q.Realize {
				realized = true
			}
			if realized {
				n.States = 0
				n.Label = "Cell"
			}
		}
		return n, true
	}}
	grid := uiaPattern(table.ptr(ifaceSimple), 10006)
	if grid == 0 {
		t.Fatal("missing Grid")
	}
	defer release(grid)
	var rows, cols int32
	comCall(grid, 4, uintptr(unsafe.Pointer(&rows)))
	comCall(grid, 5, uintptr(unsafe.Pointer(&cols)))
	if rows != 1_000_000 || cols != 2 {
		t.Fatalf("counts %d x %d", rows, cols)
	}
	var cell uintptr
	if hr := comCall(grid, 3, 900_000, 1, uintptr(unsafe.Pointer(&cell))); hr != sOK || cell == 0 {
		t.Fatalf("GetItem %#x", hr)
	}
	defer release(cell)
	if len(tree.nodes) > 6 {
		t.Fatalf("GetItem enumerated items: %d", len(tree.nodes))
	}
	item := uiaPattern(cell, 10007)
	if item == 0 {
		t.Fatal("missing GridItem")
	}
	defer release(item)
	var row int32
	comCall(item, 3, uintptr(unsafe.Pointer(&row)))
	if row != 900_000 {
		t.Fatalf("row %d", row)
	}
	header := uiaPattern(cell, 10013)
	if header == 0 {
		t.Fatal("missing TableItem")
	}
	defer release(header)
	var array uintptr
	comCall(header, 4, uintptr(unsafe.Pointer(&array)))
	if testArrayLength(array) != 1 {
		t.Fatal("missing column header")
	}
	testSafeArrayDestroy.Call(array)
	virtual := uiaPattern(cell, 10020)
	if virtual == 0 {
		t.Fatal("missing VirtualizedItem")
	}
	defer release(virtual)
	if v := uiaProperty(cell, 30109); v.VT != vtBool || v.Val == 0 {
		t.Fatal("virtual pattern availability")
	}
	before := uiaOf(cell)
	if hr := comCall(virtual, 3); hr != sOK || !realized {
		t.Fatalf("Realize %#x", hr)
	}
	if uiaOf(cell) != before || uiaOf(cell).n.Label != "Cell" || uiaProperty(cell, 30109).Val != 0 {
		t.Fatal("realization replaced the retained provider")
	}
	var invalid uintptr
	if hr := comCall(grid, 3, 1_000_000, 1, uintptr(unsafe.Pointer(&invalid))); hr != eInvalidArg {
		t.Fatalf("invalid row HRESULT %#x", hr)
	}
}

func TestUIAScrollDoubleABI(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_, table, h := collectionProvider(t)
	scroll := uiaPattern(table.ptr(ifaceSimple), 10004)
	if scroll == 0 {
		t.Fatal("missing Scroll")
	}
	defer release(scroll)
	// A typed native call exercises the x64/ARM64 floating-point thunks.
	var setPercent func(uintptr, float64, float64) uintptr
	purego.RegisterFunc(&setPercent, uiaVtbls[ifaceScroll][4])
	if hr := setPercent(scroll, 25, 75); hr != sOK {
		t.Fatalf("SetScrollPercent %#x", hr)
	}
	if len(h.events) != 1 || h.events[0].Action != platform.AccessScrollTo || h.events[0].X != 50 || h.events[0].Y != 750 {
		t.Fatalf("scroll event: %+v", h.events)
	}
	if hr := setPercent(scroll, -1, 100); hr != sOK || h.events[1].X != -1 || h.events[1].Y != 1000 {
		t.Fatal("NoScroll axis was changed")
	}
	if hr := setPercent(scroll, math.NaN(), 0); hr != eInvalidArg {
		t.Fatalf("NaN HRESULT %#x", hr)
	}
	if hr := comCall(scroll, 3, 2, 3); hr != sOK || h.events[len(h.events)-1].DY != 100 {
		t.Fatal("page scroll")
	}
	if value := uiaProperty(table.ptr(ifaceSimple), uiaScrollVerticalViewSize); value.VT != vtR8 || math.Abs(math.Float64frombits(value.Val)-100.0/11) > 1e-9 {
		t.Fatalf("view size: %+v", value)
	}
}
