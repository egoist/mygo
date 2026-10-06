//go:build linux && (amd64 || arm64)

package linux

import (
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

var testCollectionOnce sync.Once
var testTableRows, testTableColumns func(ptr) int32
var testTableRefAt func(ptr, int32, int32) ptr
var testCellSpan func(ptr, *int32, *int32, *int32, *int32) bool
var testTableHeader func(ptr, int32) ptr
var testSelectionCount func(ptr) int32
var testSelectionAdd, testSelectionRemove func(ptr, int32) bool
var testSelectionRef func(ptr, int32) ptr
var testObjectIndex func(ptr) int32
var testComponentScroll func(ptr, int32) bool
var testActionName func(ptr, int32) ptr

// TestAccessibilityCollectionLifetime keeps a GObject reference after the
// window closes and asks ATK for its defunct state.
func TestAccessibilityCollectionLifetime(handle uintptr, label string, row, column int, destroy func()) bool {
	an := testCollection(handle, label)
	if an == nil {
		return false
	}
	accessTestsOnce.Do(loadAccessTests)
	testCollectionOnce.Do(loadCollectionTests)
	cell := testTableRefAt(an.obj, int32(row), int32(column))
	if cell == 0 {
		return false
	}
	defer gObjectUnref(cell)
	destroy()
	states := atkObjectRefStateSet(cell)
	if states == 0 {
		return false
	}
	defer gObjectUnref(states)
	return atkStateSetContainsState(states, atkStates.defunct)
}

func loadCollectionTests() {
	a, _ := open("libatk-1.0.so.0")
	mustBind(a, &testTableRows, "atk_table_get_n_rows")
	mustBind(a, &testTableColumns, "atk_table_get_n_columns")
	mustBind(a, &testTableRefAt, "atk_table_ref_at")
	mustBind(a, &testCellSpan, "atk_table_cell_get_row_column_span")
	mustBind(a, &testTableHeader, "atk_table_get_column_header")
	mustBind(a, &testSelectionCount, "atk_selection_get_selection_count")
	mustBind(a, &testSelectionAdd, "atk_selection_add_selection")
	mustBind(a, &testSelectionRemove, "atk_selection_remove_selection")
	mustBind(a, &testSelectionRef, "atk_selection_ref_selection")
	mustBind(a, &testObjectIndex, "atk_object_get_index_in_parent")
	mustBind(a, &testActionName, "atk_action_get_name")
	bind(a, &testComponentScroll, "atk_component_scroll_to")
}

func testCollection(handle uintptr, label string) *accessNode {
	s := surfaceByHandle(handle)
	if s == nil || tableType == 0 {
		return nil
	}
	rootTree(gtkWidgetGetAccessible(s.area))
	t := surfaceTrees[s]
	if t == nil {
		return nil
	}
	for _, an := range t.nodes {
		if an.n.Collection != nil && an.n.Label == label {
			return an
		}
	}
	return nil
}

// TestAccessibilityCollection reads the public ATK interfaces used by
// GTK's AT-SPI bridge, with native ownership of the returned cell.
func TestAccessibilityCollection(handle uintptr, label string, row, column int) (out platform.AccessCollectionProbe, ok bool) {
	an := testCollection(handle, label)
	if an == nil {
		return
	}
	accessTestsOnce.Do(loadAccessTests)
	testCollectionOnce.Do(loadCollectionTests)
	out.Rows, out.Columns = int(testTableRows(an.obj)), int(testTableColumns(an.obj))
	out.Selected = int(testSelectionCount(an.obj))
	cell := testTableRefAt(an.obj, int32(row), int32(column))
	if cell == 0 {
		return
	}
	defer gObjectUnref(cell)
	var r, c, rs, cs int32
	if !testCellSpan(cell, &r, &c, &rs, &cs) {
		return
	}
	out.Row, out.Column, out.RowSpan, out.ColumnSpan = int(r), int(c), int(rs), int(cs)
	out.Label = goStr(atkObjectGetName(cell))
	if h := testTableHeader(an.obj, c); h != 0 {
		out.Header = goStr(atkObjectGetName(h))
	}
	var x, y, w, h int32
	atkComponentGetExtents(cell, &x, &y, &w, &h, 0)
	out.Virtualized = w == 0 || h == 0
	ok = true
	return
}

func TestAccessibilityCollectionPerform(handle uintptr, label string, row, column int, action string) bool {
	an := testCollection(handle, label)
	if an == nil {
		return false
	}
	accessTestsOnce.Do(loadAccessTests)
	testCollectionOnce.Do(loadCollectionTests)
	index := row + 1
	if an.n.Role == platform.RoleList && an.n.Collection.Grid {
		index = row*an.n.Collection.Columns + column
	}
	switch action {
	case "realize":
		cell := testTableRefAt(an.obj, int32(row), int32(column))
		if cell == 0 || testComponentScroll == nil {
			return false
		}
		defer gObjectUnref(cell)
		return testComponentScroll(cell, 6)
	case "select", "add":
		return testSelectionAdd(an.obj, int32(index))
	case "remove":
		for i := int32(0); i < testSelectionCount(an.obj); i++ {
			item := testSelectionRef(an.obj, i)
			if item == 0 {
				continue
			}
			nativeIndex := testObjectIndex(item)
			gObjectUnref(item)
			if nativeIndex == int32(index) {
				return testSelectionRemove(an.obj, i)
			}
		}
	case "scroll":
		for i := int32(0); i < atkActionGetNActions(an.obj); i++ {
			if goStr(testActionName(an.obj, i)) == "scroll down" {
				return atkActionDoAction(an.obj, i)
			}
		}
	}
	return false
}
