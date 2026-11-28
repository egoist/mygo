//go:build linux && !android && (amd64 || arm64)

package linux

import (
	"bytes"
	"sync"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/platform"
)

// trayNode mirrors one GTK menu item inside an exported AppIndicator menu so
// subsequent SetMenu calls patch only changed properties and items in place
// instead of replacing the exported DBusMenu root.
type trayNode struct {
	spec       *platform.MenuItem
	item       ptr
	subShell   ptr
	children   []*trayNode
	uid        int
	slot       int
	check      bool
	dirtyCheck bool
	icon       *platform.MenuIcon
}

var (
	trayMenuOnce          sync.Once
	cbTrayMenuActivate    ptr
	cbTrayMenuAboutToShow ptr

	traySlotNext int
	traySlotUID  = map[int]int{}

	cachedTrayMenuItem = lookupTrayDbusmenuItem
	trayConnect        = connect
	trayDisconnect     = func(obj ptr, id uint64) { gSignalHandlerDisconnect(obj, id) }
)

func handleTrayMenuActivate(item, data ptr) {
	if settingState {
		return
	}
	if uid, ok := traySlotUID[int(data)]; ok {
		for i := range itemsByID[uid] {
			if n := itemsByID[uid][i].node; itemsByID[uid][i].item == item && n != nil && n.check {
				n.dirtyCheck = true
			}
		}
		activateMenuItem(item, uid)
	}
}

// handleTrayMenuAboutToShow handles DbusmenuMenuitem::about-to-show. The
// signal prototype returns gboolean, though libdbusmenu ignores it and syncs
// structural and property updates from libdbusmenu-gtk3's GTK listeners.
func handleTrayMenuAboutToShow(mi, data ptr) bool {
	if theBackend == nil {
		return false
	}
	t := theBackend.trays[int(data)]
	if t == nil || t.rootMI != mi {
		return false
	}
	if t.h != nil {
		t.h.MenuWillOpen()
	}
	return false
}

func ensureTrayMenuCallbacks() {
	trayMenuOnce.Do(func() {
		cbTrayMenuActivate = purego.NewCallback(handleTrayMenuActivate)
		cbTrayMenuAboutToShow = purego.NewCallback(handleTrayMenuAboutToShow)
	})
}

func lookupTrayDbusmenuItem(widget ptr) ptr {
	if widget == 0 {
		return 0
	}
	// dbusmenu_gtk_parse_get_cached_item checks GTK_IS_MENU_ITEM and returns NULL
	// for the root GtkMenu, and menuicon's lazy binding is unloaded when items
	// have no icons; read libdbusmenu-gtk3's CACHED_MENUITEM data key directly.
	return gObjectGetData(widget, cs("dbusmenu-gtk-parser-cached-item"))
}

func (t *tray) setMenu(m *platform.Menu) {
	if t.spec = m; t.held() {
		return
	}
	if m == nil {
		m = &platform.Menu{}
	}
	if t.menu != 0 {
		patchTrayShell(t.menu, &t.nodes, m.Items, t.owner)
		return
	}
	t.owner = newOwner()
	menu := gtkMenuNew()
	t.nodes = buildTrayNodes(menu, m.Items, t.owner)
	gtkWidgetShowAll(menu)
	appIndicatorSetMenu(t.ind, menu)
	t.menu = menu
	applyTrayNodesIcons(t.nodes)
	t.attachAboutToShow()
}

func (t *tray) attachAboutToShow() {
	if t.menu == 0 {
		return
	}
	mi := cachedTrayMenuItem(t.menu)
	if mi == 0 {
		return
	}
	ensureTrayMenuCallbacks()
	t.rootMI = mi
	t.aboutToShowID = trayConnect(mi, "about-to-show", cbTrayMenuAboutToShow, ptr(t.id))
}

func (t *tray) resetMenu() {
	if t.rootMI != 0 && t.aboutToShowID != 0 {
		trayDisconnect(t.rootMI, t.aboutToShowID)
	}
	t.rootMI = 0
	t.aboutToShowID = 0
	dropTrayNodes(t.nodes)
	t.nodes = nil
	dropOwner(t.owner)
	t.owner = 0
	t.menu = 0
}

func buildTrayNodes(shell ptr, items []*platform.MenuItem, owner int) []*trayNode {
	nodes := make([]*trayNode, len(items))
	for i, it := range items {
		node := newTrayNode(it, owner)
		gtkMenuShellAppend(shell, node.item)
		nodes[i] = node
	}
	return nodes
}

func newTrayNode(it *platform.MenuItem, owner int) *trayNode {
	var item ptr
	check := false
	switch it.Type {
	case platform.MenuItemSeparator:
		item = gtkSeparatorMenuItemNew()
	case platform.MenuItemCheckbox, platform.MenuItemRadio:
		item = gtkCheckMenuItemNewWithLabel(cs(it.LabelWithSubtitleAndBadge()))
		gtkCheckMenuItemSetDrawAsRadio(item, it.Type == platform.MenuItemRadio)
		check = true
	default:
		item = gtkMenuItemNewWithLabel(cs(it.LabelWithSubtitleAndBadge()))
	}
	node := &trayNode{
		spec:  it,
		item:  item,
		uid:   it.ID,
		check: check,
		icon:  it.Icon,
	}
	if it.Submenu != nil {
		sub := gtkMenuNew()
		node.subShell = sub
		node.children = buildTrayNodes(sub, it.Submenu.Items, owner)
		gtkWidgetShowAll(sub)
		gtkMenuItemSetSubmenu(item, sub)
	} else if it.Type != platform.MenuItemSeparator {
		ensureTrayMenuCallbacks()
		traySlotNext++
		node.slot = traySlotNext
		traySlotUID[node.slot] = it.ID
		trayConnect(item, "activate", cbTrayMenuActivate, ptr(node.slot))
		itemRoles[it.ID] = it.Role
	}
	applyState(item, it, check)
	n := nativeItem{owner: owner, item: item, check: check, disabled: !it.Enabled, icon: it.Icon, node: node}
	itemsByID[it.ID] = append(itemsByID[it.ID], n)
	return node
}

func applyTrayNodesIcons(nodes []*trayNode) {
	for _, node := range nodes {
		if node.icon != nil && node.spec.Type != platform.MenuItemSeparator && setMenuItemIcon != nil {
			n := nativeItem{item: node.item, check: node.check, icon: node.icon, node: node}
			setMenuItemIcon(&n, node.icon)
		}
		if len(node.children) > 0 {
			applyTrayNodesIcons(node.children)
		}
	}
}

func dropTrayNodes(nodes []*trayNode) {
	for _, node := range nodes {
		dropTrayNode(node)
	}
}

func dropTrayNode(node *trayNode) {
	dropTrayNodes(node.children)
	if node.slot != 0 {
		delete(traySlotUID, node.slot)
	}
	removeTrackedItem(node.uid, node.item)
}

func removeTrackedItem(uid int, item ptr) {
	list := itemsByID[uid]
	kept := list[:0]
	for _, n := range list {
		if n.item != item {
			kept = append(kept, n)
		}
	}
	if len(kept) == 0 {
		delete(itemsByID, uid)
		delete(itemRoles, uid)
		return
	}
	itemsByID[uid] = kept
}

func compatibleTrayItem(a, b *platform.MenuItem) bool {
	return a.Type == b.Type && (a.Submenu != nil) == (b.Submenu != nil)
}

// patchTrayShell reconciles shell's GTK children from *oldPtr to newItems in
// place, preserving DBusMenu item IDs for matched stationary nodes.
func patchTrayShell(shell ptr, oldPtr *[]*trayNode, newItems []*platform.MenuItem, owner int) {
	oldNodes := *oldPtr
	matchedOld := matchTrayItems(oldNodes, newItems)
	stayOld, keepNew := stationaryTrayMatches(len(oldNodes), matchedOld)

	for j, old := range oldNodes {
		if !stayOld[j] {
			dropTrayNode(old)
			gtkContainerRemove(shell, old.item)
		}
	}

	newNodes := make([]*trayNode, len(newItems))
	for i, it := range newItems {
		if keepNew[i] {
			node := oldNodes[matchedOld[i]]
			updateTrayNode(node, it, owner)
			if it.Submenu != nil && node.subShell != 0 {
				patchTrayShell(node.subShell, &node.children, it.Submenu.Items, owner)
			}
			newNodes[i] = node
			continue
		}
		node := newTrayNode(it, owner)
		gtkWidgetShowAll(node.item)
		gtkMenuShellInsert(shell, node.item, int32(i))
		applyTrayNodesIcons([]*trayNode{node})
		newNodes[i] = node
	}
	*oldPtr = newNodes
}

func matchTrayItems(oldNodes []*trayNode, newItems []*platform.MenuItem) []int {
	matched := make([]int, len(newItems))
	for i := range matched {
		matched[i] = -1
	}
	usedOld := make([]bool, len(oldNodes))

	oldByKey := map[string][]int{}
	oldByUID := map[int]int{}
	for j, old := range oldNodes {
		if old.spec.Key != "" {
			oldByKey[old.spec.Key] = append(oldByKey[old.spec.Key], j)
		} else if old.uid != 0 {
			oldByUID[old.uid] = j
		}
	}

	// Pass 1: explicit stable keys (MenuItem.ID).
	for i, it := range newItems {
		if it.Key == "" {
			continue
		}
		for _, j := range oldByKey[it.Key] {
			if !usedOld[j] && compatibleTrayItem(oldNodes[j].spec, it) {
				matched[i] = j
				usedOld[j] = true
				break
			}
		}
	}

	// Pass 2: identical uid for unkeyed items (e.g. Menu.Append/Insert/Remove).
	for i, it := range newItems {
		if matched[i] >= 0 || it.Key != "" || it.ID == 0 {
			continue
		}
		if j, ok := oldByUID[it.ID]; ok && !usedOld[j] && compatibleTrayItem(oldNodes[j].spec, it) {
			matched[i] = j
			usedOld[j] = true
		}
	}

	// Pass 3: positional fallback for unkeyed items (such as anonymous separators).
	for i, it := range newItems {
		if matched[i] >= 0 || it.Key != "" {
			continue
		}
		for j, old := range oldNodes {
			if !usedOld[j] && old.spec.Key == "" && compatibleTrayItem(old.spec, it) {
				matched[i] = j
				usedOld[j] = true
				break
			}
		}
	}
	return matched
}

// stationaryTrayMatches marks the longest increasing subsequence of matched old
// indices so out-of-order moves remove and re-insert only the displaced items.
func stationaryTrayMatches(oldLen int, matchedOld []int) (stayOld, keepNew []bool) {
	stayOld = make([]bool, oldLen)
	keepNew = make([]bool, len(matchedOld))
	if len(matchedOld) == 0 {
		return stayOld, keepNew
	}

	var tailOldIdx []int
	var tailNewIdx []int
	prevNew := make([]int, len(matchedOld))
	for i, oldIdx := range matchedOld {
		prevNew[i] = -1
		if oldIdx < 0 {
			continue
		}
		pos := 0
		for pos < len(tailOldIdx) && tailOldIdx[pos] < oldIdx {
			pos++
		}
		if pos > 0 {
			prevNew[i] = tailNewIdx[pos-1]
		}
		if pos == len(tailOldIdx) {
			tailOldIdx = append(tailOldIdx, oldIdx)
			tailNewIdx = append(tailNewIdx, i)
		} else {
			tailOldIdx[pos] = oldIdx
			tailNewIdx[pos] = i
		}
	}
	if len(tailNewIdx) == 0 {
		return stayOld, keepNew
	}
	for cur := tailNewIdx[len(tailNewIdx)-1]; cur >= 0; cur = prevNew[cur] {
		keepNew[cur] = true
		stayOld[matchedOld[cur]] = true
	}
	return stayOld, keepNew
}

func equalMenuIcon(a, b *platform.MenuIcon) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Template == b.Template && bytes.Equal(a.PNG, b.PNG)
}

func updateTrayNode(node *trayNode, it *platform.MenuItem, owner int) {
	old := node.spec
	if it.Type != platform.MenuItemSeparator {
		if lbl := it.LabelWithSubtitleAndBadge(); lbl != old.LabelWithSubtitleAndBadge() {
			gtkMenuItemSetLabel(node.item, cs(lbl))
		}
		if !equalMenuIcon(it.Icon, node.icon) && setMenuItemIcon != nil {
			node.icon = it.Icon
			n := nativeItem{owner: owner, item: node.item, check: node.check, icon: it.Icon, node: node}
			setMenuItemIcon(&n, it.Icon)
		}
	}
	if it.Enabled != old.Enabled {
		gtkWidgetSetSensitive(node.item, it.Enabled)
	}
	if it.Visible != old.Visible {
		gtkWidgetSetNoShowAll(node.item, !it.Visible)
		gtkWidgetSetVisible(node.item, it.Visible)
	}
	if node.check && (it.Checked != old.Checked || node.dirtyCheck) {
		settingState = true
		gtkCheckMenuItemSetActive(node.item, it.Checked)
		settingState = false
		node.dirtyCheck = false
	}
	if it.ToolTip != old.ToolTip {
		if it.ToolTip == "" {
			gtkWidgetSetTooltipText(node.item, nil)
		} else {
			gtkWidgetSetTooltipText(node.item, cs(it.ToolTip))
		}
	}

	if node.uid != it.ID {
		removeTrackedItem(node.uid, node.item)
		node.uid = it.ID
		if node.slot != 0 {
			traySlotUID[node.slot] = it.ID
		}
		if it.Type != platform.MenuItemSeparator && it.Submenu == nil {
			itemRoles[it.ID] = it.Role
		}
		itemsByID[it.ID] = append(itemsByID[it.ID], nativeItem{
			owner: owner, item: node.item, check: node.check, disabled: !it.Enabled, icon: node.icon, node: node,
		})
	} else {
		list := itemsByID[node.uid]
		for i := range list {
			if list[i].item == node.item {
				list[i].disabled = !it.Enabled
				list[i].icon = node.icon
			}
		}
	}
	node.spec = it
}
