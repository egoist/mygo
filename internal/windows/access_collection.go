//go:build windows && (amd64 || arm64)

package windows

import (
	"math"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

const (
	eInvalidArg                     = 0x80070057
	uiaScrollHorizontalPercent      = 30053
	uiaScrollHorizontalViewSize     = 30054
	uiaScrollVerticalPercent        = 30055
	uiaScrollVerticalViewSize       = 30056
	uiaScrollHorizontallyScrollable = 30057
	uiaScrollVerticallyScrollable   = 30058
	uiaGridRowCount                 = 30062
	uiaGridColumnCount              = 30063
)

func collectionProperty(n platform.AccessNode, id int) (variant, bool) {
	if s := n.Scroll; s != nil {
		var value float64
		switch id {
		case uiaScrollHorizontalPercent:
			value = scrollPercent(s.X, s.MaxX())
		case uiaScrollVerticalPercent:
			value = scrollPercent(s.Y, s.MaxY())
		case uiaScrollHorizontalViewSize:
			value = scrollViewSize(s.Width, s.ContentWidth)
		case uiaScrollVerticalViewSize:
			value = scrollViewSize(s.Height, s.ContentHeight)
		case uiaScrollHorizontallyScrollable:
			return boolVariant(s.MaxX() > 0), true
		case uiaScrollVerticallyScrollable:
			return boolVariant(s.MaxY() > 0), true
		default:
			return gridProperty(n, id)
		}
		return variant{VT: vtR8, Val: math.Float64bits(value)}, true
	}
	return gridProperty(n, id)
}

func gridProperty(n platform.AccessNode, id int) (variant, bool) {
	if c := n.Collection; c != nil && c.Grid {
		if id == uiaGridRowCount {
			return variant{VT: vtI4, Val: uint64(c.Rows)}, true
		}
		if id == uiaGridColumnCount {
			return variant{VT: vtI4, Val: uint64(c.Columns)}, true
		}
	}
	if c := n.Cell; c != nil {
		var value int
		switch id {
		case 30064:
			value = c.Row
		case 30065:
			value = c.Column
		case 30066:
			value = c.RowSpan
		case 30067:
			value = c.ColumnSpan
		default:
			return variant{}, false
		}
		return variant{VT: vtI4, Val: uint64(value)}, true
	}
	return variant{}, false
}

func (t *uiaTree) ensure(n platform.AccessNode) *uiaElement {
	e := t.nodes[n.ID]
	if e == nil {
		e = newUIAElement(t)
		t.nodes[n.ID] = e
	}
	e.n = n
	if n.Item != nil {
		e.parent = t.nodes[n.Item.Container]
		if n.Item.Cell && t.source != nil && t.source.Query != nil {
			if row, ok := t.source.Query(platform.AccessQuery{Container: n.Item.Container, Index: n.Item.Index}); ok {
				e.parent = t.ensure(row)
			}
		}
	}
	if e.parent == nil {
		e.parent = t.root
	}
	return e
}

func (e *uiaElement) query(q platform.AccessQuery) *uiaElement {
	if e.dead || e.tree.source == nil || e.tree.source.Query == nil {
		return nil
	}
	if q.Container == 0 {
		q.Container = e.n.ID
	}
	n, ok := e.tree.source.Query(q)
	if !ok {
		return nil
	}
	return e.tree.ensure(n)
}

func uiaArray(p uintptr, nodes []*uiaElement) uintptr {
	sa, _, _ := procSafeArrayCreateVector.Call(vtUnknown, 0, uintptr(len(nodes)))
	for i, n := range nodes {
		index := int32(i)
		procSafeArrayPutElement.Call(sa, uintptr(unsafe.Pointer(&index)), n.ptr(ifaceSimple))
	}
	*(*uintptr)(native(p)) = sa
	return sOK
}

func (e *uiaElement) headers(ids []uint64) []*uiaElement {
	var nodes []*uiaElement
	for _, id := range ids {
		if n := e.tree.nodes[id]; n != nil {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func scrollPercent(offset, maximum float64) float64 {
	if maximum <= 0 {
		return -1
	} // UIA_ScrollPatternNoScroll
	return max(0, min(100, offset/maximum*100))
}

func scrollViewSize(view, content float64) float64 {
	if content <= 0 {
		return 100
	}
	return min(100, max(0, view/content*100))
}

func initUIACollections(cb func(any) uintptr, vtbl func(...uintptr) []uintptr, live func(uintptr) (*uiaElement, uintptr)) {
	gridCount := func(column bool) uintptr {
		return cb(func(this, p uintptr) uintptr {
			*(*int32)(native(p)) = 0
			e, hr := live(this)
			if e == nil {
				return hr
			}
			if c := e.n.Collection; c != nil {
				n := c.Rows
				if column {
					n = c.Columns
				}
				*(*int32)(native(p)) = int32(n)
			}
			return sOK
		})
	}
	uiaVtbls[ifaceGrid] = vtbl(
		cb(func(this, row, column, p uintptr) uintptr {
			*(*uintptr)(native(p)) = 0
			e, hr := live(this)
			if e == nil {
				return hr
			}
			n := e.query(platform.AccessQuery{Kind: platform.AccessQueryCell, Row: int(int32(row)), Column: int(int32(column))})
			if n == nil {
				return eInvalidArg
			}
			return out(p, n, ifaceSimple)
		}), gridCount(false), gridCount(true),
	)
	cellInt := func(get func(*platform.AccessCell) int) uintptr {
		return cb(func(this, p uintptr) uintptr {
			*(*int32)(native(p)) = 0
			e, hr := live(this)
			if e == nil {
				return hr
			}
			if e.n.Cell != nil {
				*(*int32)(native(p)) = int32(get(e.n.Cell))
			}
			return sOK
		})
	}
	uiaVtbls[ifaceGridItem] = vtbl(
		cellInt(func(c *platform.AccessCell) int { return c.Row }),
		cellInt(func(c *platform.AccessCell) int { return c.Column }),
		cellInt(func(c *platform.AccessCell) int { return c.RowSpan }),
		cellInt(func(c *platform.AccessCell) int { return c.ColumnSpan }),
		cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			if e == nil {
				return hr
			}
			return out(p, e.tree.nodes[e.n.Cell.Table], ifaceSimple)
		}),
	)
	uiaVtbls[ifaceTable] = vtbl(
		cb(func(this, p uintptr) uintptr { // GetRowHeaders: no row headers in current widgets
			e, hr := live(this)
			if e == nil {
				return hr
			}
			return uiaArray(p, nil)
		}),
		cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			if e == nil {
				return hr
			}
			return uiaArray(p, e.headers(e.n.Collection.ColumnHeaders))
		}),
		cb(func(this, p uintptr) uintptr { // RowOrColumnMajor_RowMajor
			if _, hr := live(this); hr != sOK {
				return hr
			}
			*(*int32)(native(p)) = 0
			return sOK
		}),
	)
	cellHeaders := func(column bool) uintptr {
		return cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			if e == nil {
				return hr
			}
			ids := e.n.Cell.RowHeaders
			if column {
				ids = e.n.Cell.ColumnHeaders
			}
			return uiaArray(p, e.headers(ids))
		})
	}
	uiaVtbls[ifaceTableItem] = vtbl(cellHeaders(false), cellHeaders(true))
	uiaVtbls[ifaceVirtualizedItem] = vtbl(cb(func(this uintptr) uintptr {
		e, hr := live(this)
		if e == nil {
			return hr
		}
		if e.n.Item == nil {
			return uiaInvalidOperation
		}
		if e.query(platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: e.n.Item, Realize: true}) == nil {
			return uiaElementNotAvailable
		}
		return sOK
	}))
	// Find next item is O(1). Selected-item searches enumerate only the
	// selection; explicit name searches may inspect subsequent item labels.
	uiaVtbls[ifaceItemContainer] = vtbl(cb(func(this, start, property, value, p uintptr) uintptr {
		*(*uintptr)(native(p)) = 0
		e, hr := live(this)
		if e == nil {
			return hr
		}
		from := 0
		if start != 0 {
			found := false
			for _, n := range e.tree.nodes {
				if n.ptr(ifaceSimple) != start {
					continue
				}
				current := n.query(platform.AccessQuery{Kind: platform.AccessQueryResolve, Item: n.n.Item})
				if current == nil || current.n.Item.Container != e.n.ID {
					return eInvalidArg
				}
				from, found = current.n.Item.Index+1, true
				break
			}
			if !found {
				return eInvalidArg
			}
		}
		if property == 0 {
			return out(p, e.query(platform.AccessQuery{Index: from}), ifaceSimple)
		}
		v := *(*variant)(native(value))
		if property == uiaIsSelectedProperty && v.VT == vtBool && v.Val != 0 {
			for _, n := range e.chosen() {
				if n.n.Item.Index >= from {
					return out(p, n, ifaceSimple)
				}
			}
			return sOK
		}
		if property != uiaNameProperty && property != uiaIsSelectedProperty {
			return 0x80040204
		} // UIA_E_NOTSUPPORTED
		if property == uiaNameProperty && v.VT != vtBSTR || property == uiaIsSelectedProperty && v.VT != vtBool {
			return eInvalidArg
		}
		for i := from; i < e.n.Collection.Items; i++ {
			n, ok := e.tree.source.Query(platform.AccessQuery{Container: e.n.ID, Index: i})
			if !ok {
				continue
			}
			match := false
			if property == uiaNameProperty {
				match = n.Label == wstr(uintptr(v.Val))
			} else {
				match = (n.States&platform.AccessChecked != 0) == (v.Val != 0)
			}
			if match {
				return out(p, e.tree.ensure(n), ifaceSimple)
			}
		}
		return sOK
	}))
	scrollGet := func(get func(platform.AccessScroll) float64) uintptr {
		return cb(func(this, p uintptr) uintptr {
			setFloat(p, 0)
			e, hr := live(this)
			if e == nil {
				return hr
			}
			if e.n.Scroll != nil {
				setFloat(p, get(*e.n.Scroll))
			}
			return sOK
		})
	}
	scrollable := func(horizontal bool) uintptr {
		return cb(func(this, p uintptr) uintptr {
			setBool(p, false)
			e, hr := live(this)
			if e == nil {
				return hr
			}
			s := e.n.Scroll
			if s != nil {
				v := s.MaxY()
				if horizontal {
					v = s.MaxX()
				}
				setBool(p, v > 0)
			}
			return sOK
		})
	}
	uiaSetScrollCallback = cb(func(this, x, y uintptr) uintptr {
		e, hr := live(this)
		if e == nil {
			return hr
		}
		xp, yp := math.Float64frombits(uint64(x)), math.Float64frombits(uint64(y))
		valid := func(v float64) bool { return v == -1 || v >= 0 && v <= 100 }
		if !valid(xp) || !valid(yp) {
			return eInvalidArg
		}
		s := e.n.Scroll
		if xp != -1 && s.MaxX() <= 0 || yp != -1 && s.MaxY() <= 0 {
			return uiaInvalidOperation
		}
		xp, yp = percentOffset(xp, s.MaxX()), percentOffset(yp, s.MaxY())
		return e.scrollAction(platform.SurfaceEvent{Action: platform.AccessScrollTo, X: xp, Y: yp})
	})
	_, _, setScroll := uiaThunks()
	uiaVtbls[ifaceScroll] = vtbl(
		cb(func(this, horizontal, vertical uintptr) uintptr {
			e, hr := live(this)
			if e == nil {
				return hr
			}
			s := e.n.Scroll
			delta := func(amount int32, view, maximum float64) (float64, bool) {
				if amount == 2 {
					return 0, true
				} // NoAmount
				if maximum <= 0 {
					return 0, false
				}
				switch amount {
				case 0:
					return -view, true
				case 1:
					return -40, true
				case 3:
					return view, true
				case 4:
					return 40, true
				}
				return 0, false
			}
			dx, okX := delta(int32(horizontal), s.Width, s.MaxX())
			dy, okY := delta(int32(vertical), s.Height, s.MaxY())
			if !okX || !okY {
				return uiaInvalidOperation
			}
			return e.scrollAction(platform.SurfaceEvent{Action: platform.AccessScrollBy, DX: dx, DY: dy})
		}), setScroll,
		scrollGet(func(s platform.AccessScroll) float64 { return scrollPercent(s.X, s.MaxX()) }),
		scrollGet(func(s platform.AccessScroll) float64 { return scrollPercent(s.Y, s.MaxY()) }),
		scrollGet(func(s platform.AccessScroll) float64 { return scrollViewSize(s.Width, s.ContentWidth) }),
		scrollGet(func(s platform.AccessScroll) float64 { return scrollViewSize(s.Height, s.ContentHeight) }),
		scrollable(true), scrollable(false),
	)
}

func percentOffset(percent, maximum float64) float64 {
	if percent == -1 {
		return -1
	}
	return percent / 100 * maximum
}

func (e *uiaElement) scrollAction(ev platform.SurfaceEvent) uintptr {
	if e.n.States&platform.AccessDisabled != 0 {
		return uiaElementNotEnabled
	}
	ev.Kind, ev.ID = platform.AccessAction, e.n.ID
	e.tree.s.send(ev)
	return sOK
}
