//go:build linux && !android && (amd64 || arm64)

package linux

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"slices"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// indicatorCalls stands in for libayatana and GTK's menus, recording the
// calls a tray makes, and gives the tests a fresh watcher state.
func indicatorCalls(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	str := func(p *byte) string { return goStr(ptr(unsafe.Pointer(p))) }
	oldIcon, oldLabel, oldTitle, oldTip, oldMenu := appIndicatorSetIconFull, appIndicatorSetLabel, appIndicatorSetTitle, appIndicatorSetTooltipFull, appIndicatorSetMenu
	oldMenuNew, oldShowAll, oldDrop, oldCached, oldWatcher := gtkMenuNew, gtkWidgetShowAll, dropFallbackTooltips, cachedTrayMenuItem, trayWatcher
	t.Cleanup(func() {
		appIndicatorSetIconFull, appIndicatorSetLabel, appIndicatorSetTitle, appIndicatorSetTooltipFull, appIndicatorSetMenu = oldIcon, oldLabel, oldTitle, oldTip, oldMenu
		gtkMenuNew, gtkWidgetShowAll, dropFallbackTooltips, cachedTrayMenuItem, trayWatcher = oldMenuNew, oldShowAll, oldDrop, oldCached, oldWatcher
	})
	appIndicatorSetIconFull = func(_ ptr, file, _ *byte) { calls = append(calls, "icon "+str(file)) }
	appIndicatorSetLabel = func(_ ptr, label, _ *byte) { calls = append(calls, "label "+str(label)) }
	appIndicatorSetTitle = func(_ ptr, title *byte) { calls = append(calls, "title "+str(title)) }
	appIndicatorSetTooltipFull = func(_ ptr, _, title, _ *byte) { calls = append(calls, "tooltip "+str(title)) }
	appIndicatorSetMenu = func(_, _ ptr) { calls = append(calls, "menu") }
	gtkMenuNew = func() ptr { return 1 }
	gtkWidgetShowAll = func(ptr) {}
	dropFallbackTooltips = func(ptr) { calls = append(calls, "drop tooltips") }
	cachedTrayMenuItem = func(ptr) ptr { return 0 }
	trayWatcher = struct{ subscribed, seen, away bool }{subscribed: true}
	return &calls
}

func testTray(t *testing.T) (*Backend, *tray) {
	b := &Backend{trays: map[int]*tray{}}
	tr := &tray{b: b, id: 1, ind: 1, iconDir: t.TempDir()}
	b.trays[tr.id] = tr
	return b, tr
}

func pngImage(t *testing.T) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestTrayWithoutWatcher checks that a session where no watcher was ever
// seen, whose only icon is libayatana's fallback, calls the indicator
// as before, even when told the watcher has no owner.
func TestTrayWithoutWatcher(t *testing.T) {
	calls := indicatorCalls(t)
	b, tr := testTray(t)
	b.watcherChanged(false)
	tr.SetToolTip("tip")
	tr.SetTitle("title")
	if want := []string{"title tip", "tooltip tip", "label title"}; !slices.Equal(*calls, want) || tr.pending {
		t.Errorf("calls %q, pending %v; want %q, not pending", *calls, tr.pending, want)
	}
}

// TestTrayHoldsWhileWatcherAway checks that once a watcher seen goes
// away, the tray keeps what the app sets, empty strings too, without
// calling the indicator, and gives it all once when a watcher returns,
// the image under a new file name.
func TestTrayHoldsWhileWatcherAway(t *testing.T) {
	calls := indicatorCalls(t)
	b, tr := testTray(t)
	b.watcherChanged(true)
	img := pngImage(t)
	if err := tr.SetImage(img, false); err != nil {
		t.Fatal(err)
	}
	tr.SetTitle("before")
	*calls = nil

	b.watcherChanged(false)
	menu := &platform.Menu{}
	if err := tr.SetImage(img, false); err != nil {
		t.Fatal(err)
	}
	tr.SetTitle("")
	tr.SetToolTip("away")
	tr.SetMenu(menu)
	if len(*calls) != 0 {
		t.Fatalf("called the indicator while the watcher was away: %q", *calls)
	}
	if !tr.pending || !bytes.Equal(tr.image, img) || tr.title != "" || tr.tip != "away" || tr.spec != menu {
		t.Fatalf("state not kept: pending %v, title %q, tip %q, menu kept %v", tr.pending, tr.title, tr.tip, tr.spec == menu)
	}

	b.watcherChanged(true)
	want := []string{"drop tooltips", "icon " + tr.iconDir + "/icon-2.png", "label ", "title away", "tooltip away", "menu"}
	if !slices.Equal(*calls, want) || tr.pending {
		t.Errorf("on return: calls %q, pending %v; want %q, not pending", *calls, tr.pending, want)
	}
	*calls = nil
	b.watcherChanged(true)
	if want := []string{"drop tooltips"}; !slices.Equal(*calls, want) {
		t.Errorf("a second owner reapplied the state: calls %q, want %q", *calls, want)
	}
}

// TestTrayDestroyedWhileHeld checks that a tray destroyed while it held
// calls gives the indicator nothing when the watcher returns.
func TestTrayDestroyedWhileHeld(t *testing.T) {
	calls := indicatorCalls(t)
	b, tr := testTray(t)
	b.watcherChanged(true)
	b.watcherChanged(false)
	tr.SetToolTip("away")
	oldStatus, oldUnref := appIndicatorSetStatus, gObjectUnref
	t.Cleanup(func() { appIndicatorSetStatus, gObjectUnref = oldStatus, oldUnref })
	appIndicatorSetStatus = func(ptr, int32) {}
	gObjectUnref = func(ptr) {}
	tr.Destroy()
	*calls = nil
	b.watcherChanged(true)
	tr.resume()
	if len(*calls) != 0 {
		t.Errorf("a destroyed tray called the indicator: %q", *calls)
	}
}

type testMenuHarnessHandler struct {
	willOpen func()
	clicks   []int
}

func (h *testMenuHarnessHandler) Clicked()           {}
func (h *testMenuHarnessHandler) RightClicked()      {}
func (h *testMenuHarnessHandler) AppearanceChanged() {}
func (h *testMenuHarnessHandler) MenuWillOpen() {
	if h.willOpen != nil {
		h.willOpen()
	}
}

type testMenuBackendHandler struct {
	platform.AppHandler
	clicks []int
}

func (h *testMenuBackendHandler) MenuItemClicked(id int) { h.clicks = append(h.clicks, id) }

func trayMenuTestCalls(t *testing.T) (*[]string, map[ptr][]ptr) {
	t.Helper()
	var calls []string
	shells := map[ptr][]ptr{}
	nextWidget := ptr(100)
	allocWidget := func() ptr {
		nextWidget++
		return nextWidget
	}
	str := func(p *byte) string { return goStr(ptr(unsafe.Pointer(p))) }

	oldAppMenu := appIndicatorSetMenu
	oldMenuNew, oldShowAll := gtkMenuNew, gtkWidgetShowAll
	oldItemNew, oldCheckNew := gtkMenuItemNewWithLabel, gtkCheckMenuItemNewWithLabel
	oldRadio, oldActive := gtkCheckMenuItemSetDrawAsRadio, gtkCheckMenuItemSetActive
	oldSepNew, oldSetLabel, oldSetSub := gtkSeparatorMenuItemNew, gtkMenuItemSetLabel, gtkMenuItemSetSubmenu
	oldAppend, oldInsert, oldRemove := gtkMenuShellAppend, gtkMenuShellInsert, gtkContainerRemove
	oldSens, oldNoShow, oldVis, oldTip := gtkWidgetSetSensitive, gtkWidgetSetNoShowAll, gtkWidgetSetVisible, gtkWidgetSetTooltipText
	oldCached, oldConn, oldDisc := cachedTrayMenuItem, trayConnect, trayDisconnect
	oldWatcher := trayWatcher

	t.Cleanup(func() {
		appIndicatorSetMenu = oldAppMenu
		gtkMenuNew, gtkWidgetShowAll = oldMenuNew, oldShowAll
		gtkMenuItemNewWithLabel, gtkCheckMenuItemNewWithLabel = oldItemNew, oldCheckNew
		gtkCheckMenuItemSetDrawAsRadio, gtkCheckMenuItemSetActive = oldRadio, oldActive
		gtkSeparatorMenuItemNew, gtkMenuItemSetLabel, gtkMenuItemSetSubmenu = oldSepNew, oldSetLabel, oldSetSub
		gtkMenuShellAppend, gtkMenuShellInsert, gtkContainerRemove = oldAppend, oldInsert, oldRemove
		gtkWidgetSetSensitive, gtkWidgetSetNoShowAll, gtkWidgetSetVisible, gtkWidgetSetTooltipText = oldSens, oldNoShow, oldVis, oldTip
		cachedTrayMenuItem, trayConnect, trayDisconnect = oldCached, oldConn, oldDisc
		trayWatcher = oldWatcher
	})

	appIndicatorSetMenu = func(_, menu ptr) { calls = append(calls, fmt.Sprintf("ind_set_menu %d", menu)) }
	gtkMenuNew = func() ptr {
		m := allocWidget()
		calls = append(calls, fmt.Sprintf("menu_new %d", m))
		return m
	}
	gtkWidgetShowAll = func(ptr) {}
	gtkMenuItemNewWithLabel = func(lbl *byte) ptr {
		it := allocWidget()
		calls = append(calls, fmt.Sprintf("new_item %d %q", it, str(lbl)))
		return it
	}
	gtkCheckMenuItemNewWithLabel = func(lbl *byte) ptr {
		it := allocWidget()
		calls = append(calls, fmt.Sprintf("new_check %d %q", it, str(lbl)))
		return it
	}
	gtkCheckMenuItemSetDrawAsRadio = func(ptr, bool) {}
	gtkCheckMenuItemSetActive = func(it ptr, active bool) {
		calls = append(calls, fmt.Sprintf("set_active %d %v", it, active))
	}
	gtkSeparatorMenuItemNew = func() ptr {
		it := allocWidget()
		calls = append(calls, fmt.Sprintf("new_sep %d", it))
		return it
	}
	gtkMenuItemSetLabel = func(it ptr, lbl *byte) {
		calls = append(calls, fmt.Sprintf("set_label %d %q", it, str(lbl)))
	}
	gtkMenuItemSetSubmenu = func(it, sub ptr) {
		calls = append(calls, fmt.Sprintf("set_sub %d %d", it, sub))
	}
	gtkMenuShellAppend = func(shell, child ptr) {
		shells[shell] = append(shells[shell], child)
		calls = append(calls, fmt.Sprintf("append %d %d", shell, child))
	}
	gtkMenuShellInsert = func(shell, child ptr, pos int32) {
		s := shells[shell]
		i := int(pos)
		if i < 0 {
			i = 0
		}
		if i > len(s) {
			i = len(s)
		}
		s = slices.Insert(s, i, child)
		shells[shell] = s
		calls = append(calls, fmt.Sprintf("insert %d %d @%d", shell, child, pos))
	}
	gtkContainerRemove = func(shell, child ptr) {
		s := shells[shell]
		if idx := slices.Index(s, child); idx >= 0 {
			shells[shell] = slices.Delete(s, idx, idx+1)
		}
		calls = append(calls, fmt.Sprintf("remove %d %d", shell, child))
	}
	gtkWidgetSetSensitive = func(it ptr, sens bool) {
		calls = append(calls, fmt.Sprintf("set_sens %d %v", it, sens))
	}
	gtkWidgetSetNoShowAll = func(ptr, bool) {}
	gtkWidgetSetVisible = func(it ptr, vis bool) {
		calls = append(calls, fmt.Sprintf("set_vis %d %v", it, vis))
	}
	gtkWidgetSetTooltipText = func(ptr, *byte) {}
	cachedTrayMenuItem = func(menu ptr) ptr { return menu + 1000 }
	trayConnect = func(obj ptr, sig string, _ ptr, _ ptr) uint64 {
		calls = append(calls, fmt.Sprintf("connect %d %s", obj, sig))
		return uint64(obj)
	}
	trayDisconnect = func(obj ptr, id uint64) {
		calls = append(calls, fmt.Sprintf("disconnect %d %d", obj, id))
	}
	trayWatcher = struct{ subscribed, seen, away bool }{subscribed: true}
	return &calls, shells
}

func TestTrayMenuKeyedDiffAndAboutToShow(t *testing.T) {
	calls, shells := trayMenuTestCalls(t)
	oldBackend := theBackend
	oldItems, oldRoles, oldSlots := itemsByID, itemRoles, traySlotUID
	itemsByID = map[int][]nativeItem{}
	itemRoles = map[int]string{}
	traySlotUID = map[int]int{}
	defer func() {
		theBackend = oldBackend
		itemsByID, itemRoles, traySlotUID = oldItems, oldRoles, oldSlots
	}()

	bh := &testMenuBackendHandler{}
	b := &Backend{h: bh, trays: map[int]*tray{}}
	theBackend = b
	th := &testMenuHarnessHandler{}
	tr := &tray{b: b, h: th, id: 1, ind: 1, iconDir: t.TempDir()}
	b.trays[tr.id] = tr
	b.watcherChanged(true)

	menu1 := &platform.Menu{Items: []*platform.MenuItem{
		{ID: 1, Key: "head", Label: "Disconnected", Enabled: false, Visible: true},
		{ID: 2, Key: "sub", Label: "Fastest", Enabled: true, Visible: true, Submenu: &platform.Menu{Items: []*platform.MenuItem{
			{ID: 3, Key: "n1", Label: "Node A (50ms)", Type: platform.MenuItemCheckbox, Checked: true, Enabled: true, Visible: true},
			{ID: 4, Key: "n2", Label: "Node B (80ms)", Type: platform.MenuItemCheckbox, Enabled: true, Visible: true},
			{ID: 7, Key: "n3", Label: "Node C (90ms)", Type: platform.MenuItemCheckbox, Enabled: true, Visible: true},
		}}},
		{ID: 5, Type: platform.MenuItemSeparator, Visible: true},
		{ID: 6, Key: "quit", Label: "Quit", Enabled: true, Visible: true},
	}}
	tr.SetMenu(menu1)
	rootMenu := tr.menu
	if rootMenu == 0 || len(tr.nodes) != 4 {
		t.Fatalf("initial menu not built: menu=%d nodes=%d", rootMenu, len(tr.nodes))
	}
	subShell := tr.nodes[1].subShell
	if subShell == 0 {
		t.Fatal("missing submenu shell")
	}
	if got := len(shells[rootMenu]); got != 4 {
		t.Fatalf("root shell child count=%d, want 4", got)
	}
	if !slices.Contains(*calls, fmt.Sprintf("connect %d about-to-show", rootMenu+1000)) {
		t.Fatalf("did not connect about-to-show on cached root item: %q", *calls)
	}
	nodeN1 := tr.nodes[1].children[0]
	nodeN2 := tr.nodes[1].children[1]
	nodeN3 := tr.nodes[1].children[2]
	quitItem := tr.nodes[3].item

	// 1) Value-only update with fresh MenuItem uids: zero structural mutations, zero ind_set_menu, and slot uid rebinds.
	*calls = nil
	menu2 := &platform.Menu{Items: []*platform.MenuItem{
		{ID: 11, Key: "head", Label: "Connected", Enabled: true, Visible: true},
		{ID: 12, Key: "sub", Label: "Fastest", Enabled: true, Visible: true, Submenu: &platform.Menu{Items: []*platform.MenuItem{
			{ID: 13, Key: "n1", Label: "Node A (42ms)", Type: platform.MenuItemCheckbox, Checked: false, Enabled: true, Visible: true},
			{ID: 14, Key: "n2", Label: "Node B (65ms)", Type: platform.MenuItemCheckbox, Checked: true, Enabled: true, Visible: true},
			{ID: 17, Key: "n3", Label: "Node C (90ms)", Type: platform.MenuItemCheckbox, Enabled: true, Visible: true},
		}}},
		{ID: 15, Type: platform.MenuItemSeparator, Visible: true},
		{ID: 16, Key: "quit", Label: "Quit", Enabled: true, Visible: true},
	}}
	tr.SetMenu(menu2)
	for _, c := range *calls {
		for _, bad := range []string{"ind_set_menu", "menu_new", "new_item", "new_check", "insert", "remove"} {
			if len(c) >= len(bad) && c[:len(bad)] == bad {
				t.Fatalf("value-only update made structural call %q in %q", c, *calls)
			}
		}
	}
	if tr.nodes[1].children[0] != nodeN1 || tr.nodes[1].children[1] != nodeN2 || tr.nodes[1].children[2] != nodeN3 || tr.nodes[3].item != quitItem {
		t.Fatal("value-only update replaced matched widgets")
	}
	if traySlotUID[tr.nodes[3].slot] != 16 {
		t.Fatalf("slot uid not rebound: got %d, want 16", traySlotUID[tr.nodes[3].slot])
	}
	handleTrayMenuActivate(quitItem, ptr(tr.nodes[3].slot))
	if !slices.Equal(bh.clicks, []int{16}) {
		t.Fatalf("rebound item activation delivered %v, want [16]", bh.clicks)
	}

	// 2) Out-of-order reorder + insertion/removal: stationary LIS keeps n1 and n3 in place and moves n2.
	*calls = nil
	th.willOpen = func() {
		tr.SetMenu(&platform.Menu{Items: []*platform.MenuItem{
			{ID: 21, Key: "head", Label: "Connected", Enabled: true, Visible: true},
			{ID: 22, Key: "sub", Label: "Fastest", Enabled: true, Visible: true, Submenu: &platform.Menu{Items: []*platform.MenuItem{
				{ID: 24, Key: "n2", Label: "Node B (30ms)", Type: platform.MenuItemCheckbox, Checked: true, Enabled: true, Visible: true},
				{ID: 23, Key: "n1", Label: "Node A (42ms)", Type: platform.MenuItemCheckbox, Checked: false, Enabled: true, Visible: true},
				{ID: 25, Key: "n3", Label: "Node C (90ms)", Type: platform.MenuItemCheckbox, Enabled: true, Visible: true},
				{ID: 28, Key: "n4", Label: "Node D (120ms)", Type: platform.MenuItemCheckbox, Enabled: true, Visible: true},
			}}},
			{ID: 26, Type: platform.MenuItemSeparator, Visible: true},
			{ID: 27, Key: "quit", Label: "Quit", Enabled: true, Visible: true},
		}})
	}
	handleTrayMenuAboutToShow(tr.rootMI, ptr(tr.id))
	if tr.nodes[1].subShell != subShell {
		t.Fatal("submenu shell replaced during child diff")
	}
	// Stationary LIS [n1, n3] must remain the exact same widgets while displaced n2 moves to index 0.
	if tr.nodes[1].children[0] == nodeN2 || tr.nodes[1].children[1] != nodeN1 || tr.nodes[1].children[2] != nodeN3 {
		t.Fatalf("expected n1 and n3 kept in place while n2 moved; children=%+v", tr.nodes[1].children)
	}
	if len(shells[subShell]) != 4 {
		t.Fatalf("subShell len=%d, want 4", len(shells[subShell]))
	}
	for _, c := range *calls {
		if len(c) >= len("ind_set_menu") && c[:len("ind_set_menu")] == "ind_set_menu" {
			t.Fatalf("structural patch replaced indicator menu: %q", *calls)
		}
	}

	// 3) Value-only update inside AboutToShow updates labels in place without structural calls.
	*calls = nil
	th.willOpen = func() {
		tr.SetMenu(&platform.Menu{Items: []*platform.MenuItem{
			{ID: 31, Key: "head", Label: "Connected · HK", Enabled: true, Visible: true},
			{ID: 32, Key: "sub", Label: "Fastest", Enabled: true, Visible: true, Submenu: &platform.Menu{Items: []*platform.MenuItem{
				{ID: 34, Key: "n2", Label: "Node B (28ms)", Type: platform.MenuItemCheckbox, Checked: true, Enabled: true, Visible: true},
				{ID: 33, Key: "n1", Label: "Node A (42ms)", Type: platform.MenuItemCheckbox, Enabled: true, Visible: true},
				{ID: 35, Key: "n3", Label: "Node C (90ms)", Type: platform.MenuItemCheckbox, Enabled: true, Visible: true},
				{ID: 38, Key: "n4", Label: "Node D (115ms)", Type: platform.MenuItemCheckbox, Enabled: true, Visible: true},
			}}},
			{ID: 36, Type: platform.MenuItemSeparator, Visible: true},
			{ID: 37, Key: "quit", Label: "Quit", Enabled: true, Visible: true},
		}})
	}
	handleTrayMenuAboutToShow(tr.rootMI, ptr(tr.id))
	for _, c := range *calls {
		for _, bad := range []string{"ind_set_menu", "menu_new", "new_item", "new_check", "insert", "remove"} {
			if len(c) >= len(bad) && c[:len(bad)] == bad {
				t.Fatalf("value-only AboutToShow made structural call %q in %q", c, *calls)
			}
		}
	}

	// 4) Activating a check item marks it dirtyCheck so next AboutToShow restores active state even if Checked stays true.
	curN2 := tr.nodes[1].children[0]
	handleTrayMenuActivate(curN2.item, ptr(curN2.slot))
	*calls = nil
	handleTrayMenuAboutToShow(tr.rootMI, ptr(tr.id))
	if want := fmt.Sprintf("set_active %d true", curN2.item); !slices.Contains(*calls, want) {
		t.Fatalf("expected %q after check click rollback, got %q", want, *calls)
	}
}
