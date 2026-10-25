//go:build darwin

package darwin

import (
	"strconv"
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/accelerator"
	"github.com/egoist/mygo/internal/platform"
)

// Native menu items are tracked per owner (the app menu, a tray, a popup)
// so they can be updated in place and released when the owner rebuilds.
// badgeTab is the column its badge ends at, kept so updates keep the
// alignment the menu was built with.
type nativeItem struct {
	owner    int
	item     id
	badgeTab float64
}

const ownerAppMenu = 0

var (
	menuOwners    = 1
	itemsByID     = map[int][]nativeItem{}
	nativeRoleSel = map[string]string{
		"undo":               "undo:",
		"redo":               "redo:",
		"cut":                "cut:",
		"copy":               "copy:",
		"paste":              "paste:",
		"pasteAndMatchStyle": "pasteAsPlainText:",
		"delete":             "delete:",
		"selectAll":          "selectAll:",
		"minimize":           "performMiniaturize:",
		"zoom":               "performZoom:",
		"close":              "performClose:",
		"front":              "arrangeInFront:",
		"togglefullscreen":   "toggleFullScreen:",
		"about":              "orderFrontStandardAboutPanel:",
		"hide":               "hide:",
		"hideOthers":         "hideOtherApplications:",
		"unhide":             "unhideAllApplications:",
		"quit":               "terminate:",
		"startSpeaking":      "startSpeaking:",
		"stopSpeaking":       "stopSpeaking:",
	}
)

func newOwner() int {
	menuOwners++
	return menuOwners
}

func track(owner, itemID int, item id, badgeTab float64) {
	itemsByID[itemID] = append(itemsByID[itemID], nativeItem{owner, retain(item), badgeTab})
}

func dropOwner(owner int) {
	for itemID, list := range itemsByID {
		kept := list[:0]
		for _, n := range list {
			if n.owner == owner {
				release(n.item)
			} else {
				kept = append(kept, n)
			}
		}
		if len(kept) == 0 {
			delete(itemsByID, itemID)
		} else {
			itemsByID[itemID] = kept
		}
	}
}

func registerMenuTarget() {
	classDef("MyGoMenuTarget", "NSObject", nil, []objc.MethodDef{
		method("mygoMenuItemClicked:", func(self id, _ objc.SEL, item id) {
			theBackend.h.MenuItemClicked(sendInt(item, "tag"))
		}),
	})
	// Items that keep their menu open show a view MyGo draws: the view
	// handles the click itself, so NSMenu does not end its tracking and
	// the item's menu and the one above it stay up.
	classDef("MyGoMenuKeepOpenView", "NSView", nil, []objc.MethodDef{
		method("mouseUp:", func(self id, _ objc.SEL, _ id) {
			item := send(self, "enclosingMenuItem")
			if item == 0 || !sendBool(item, "isEnabled") {
				return
			}
			theBackend.h.MenuItemClicked(sendInt(send(self, "identifier"), "integerValue"))
		}),
		method("mouseEntered:", func(self id, _ objc.SEL, _ id) {
			setMenuViewHighlighted(self, true)
		}),
		method("mouseExited:", func(self id, _ objc.SEL, _ id) {
			setMenuViewHighlighted(self, false)
		}),
	})
}

func (b *Backend) SetApplicationMenu(m *platform.Menu) {
	withPool(func() {
		dropOwner(ownerAppMenu)
		if m == nil {
			send(b.app, "setMainMenu:", 0)
			return
		}
		send(b.app, "setMainMenu:", uintptr(b.buildMenu(m, "", ownerAppMenu)))
	})
}

// menuMetrics are the columns a menu's items share: the x badges end at
// and the width of the views of its KeepOpen items.
type menuMetrics struct {
	badgeTab      float64
	keepOpenTab   float64
	keepOpenWidth float64
}

// buildMenu creates an autoreleased NSMenu.
func (b *Backend) buildMenu(m *platform.Menu, title string, owner int) id {
	menu := autorelease(send(send(class("NSMenu"), "alloc"), "initWithTitle:", uintptr(nsString(title))))
	send(menu, "setAutoenablesItems:", 0)
	keepOpenTab, keepOpenWidth := keepOpenMetrics(m)
	metrics := menuMetrics{badgeTab: badgeTab(m), keepOpenTab: keepOpenTab, keepOpenWidth: keepOpenWidth}
	for _, it := range m.Items {
		if it.Type == platform.MenuItemSeparator {
			item := send(class("NSMenuItem"), "separatorItem")
			send(item, "setHidden:", boolArg(!it.Visible))
			send(menu, "addItem:", uintptr(item))
			track(owner, it.ID, item, 0)
			continue
		}
		item := b.buildItem(it, owner, metrics)
		send(menu, "addItem:", uintptr(item))
		if it.Role == "zoomIn" {
			// Also accept Cmd+= (no shift) like other macOS apps.
			alt := b.buildItem(&platform.MenuItem{ID: it.ID, Label: it.Label, Role: it.Role, Accelerator: "CmdOrCtrl+=", Enabled: it.Enabled}, owner, menuMetrics{})
			send(alt, "setHidden:", 1)
			if respondsTo(alt, "setAllowsKeyEquivalentWhenHidden:") {
				send(alt, "setAllowsKeyEquivalentWhenHidden:", 1)
			}
			send(menu, "addItem:", uintptr(alt))
		}
	}
	return menu
}

func (b *Backend) buildItem(it *platform.MenuItem, owner int, metrics menuMetrics) id {
	action := sel("mygoMenuItemClicked:")
	target := b.menuTarget
	if s, ok := nativeRoleSel[it.Role]; ok {
		action, target = sel(s), 0
	}
	if it.Submenu != nil {
		action, target = 0, 0
	}
	key, mods := keyEquivalent(it.Accelerator)
	item := autorelease(send(send(class("NSMenuItem"), "alloc"), "initWithTitle:action:keyEquivalent:",
		uintptr(nsString(it.Label)), uintptr(action), uintptr(nsString(key))))
	if key != "" {
		send(item, "setKeyEquivalentModifierMask:", uintptr(mods))
	}
	send(item, "setTarget:", uintptr(target))
	send(item, "setTag:", uintptr(it.ID))
	applyItemState(item, it)
	if it.Submenu != nil {
		sub := b.buildMenu(it.Submenu, it.Label, owner)
		send(item, "setSubmenu:", uintptr(sub))
		switch it.Role {
		case "services":
			send(b.app, "setServicesMenu:", uintptr(sub))
		case "window", "windowMenu":
			send(b.app, "setWindowsMenu:", uintptr(sub))
		case "help":
			send(b.app, "setHelpMenu:", uintptr(sub))
		}
	}
	tab := metrics.badgeTab
	if it.KeepOpen && it.Submenu == nil {
		view := buildKeepOpenView(it, metrics)
		send(item, "setView:", uintptr(view))
		updateKeepOpenView(view, it, metrics.keepOpenTab)
		tab = metrics.keepOpenTab
	} else {
		setItemTitle(item, it, metrics.badgeTab)
	}
	track(owner, it.ID, item, tab)
	return item
}

// menuBadgeGap is the least space between the longest label of a menu and
// its badges' left edge.
const menuBadgeGap = 24

// badgeTab returns the x at which the badges of m end: past the menu's
// longest label and wide enough for its widest badge. Zero when no item
// has one.
func badgeTab(m *platform.Menu) float64 {
	found := false
	for _, it := range m.Items {
		if it.Visible && it.Badge != "" && it.Type != platform.MenuItemSeparator {
			found = true
			break
		}
	}
	if !found {
		return 0
	}
	labelFont, badgeFont := menuFont(), menuBadgeFont()
	var maxLabel, maxBadge float64
	for _, it := range m.Items {
		if !it.Visible || it.Type == platform.MenuItemSeparator {
			continue
		}
		maxLabel = max(maxLabel, textWidth(it.Label, labelFont))
		maxBadge = max(maxBadge, textWidth(it.Badge, badgeFont))
	}
	return maxLabel + menuBadgeGap + maxBadge
}

// menuFont is the font AppKit draws menu item titles in, autoreleased.
func menuFont() id {
	return msgFloatID(class("NSFont"), sel("menuFontOfSize:"), 0)
}

// menuBadgeFont is the monospaced-digit font of a badge, so that latencies
// and counts keep their width as their digits change, autoreleased.
func menuBadgeFont() id {
	return msgFloat2ID(class("NSFont"), sel("monospacedDigitSystemFontOfSize:weight:"), 12, 0)
}

// textWidth measures s in font, in points.
func textWidth(s string, font id) float64 {
	if s == "" || font == 0 {
		return 0
	}
	attrs := send(class("NSMutableDictionary"), "dictionary")
	send(attrs, "setObject:forKey:", uintptr(font), uintptr(nsString("NSFont")))
	str := autorelease(send(send(class("NSAttributedString"), "alloc"), "initWithString:attributes:",
		uintptr(nsString(s)), uintptr(attrs)))
	return msgSize(str, sel("size")).Width
}

// menuTitle returns the attributed title of a label and, at the
// right-aligned tab stop tab, its badge in a monospaced-digit font.
func menuTitle(label, badge string, tab float64) id {
	label = strings.ReplaceAll(label, "\t", " ")
	badge = strings.ReplaceAll(badge, "\t", " ")
	attrs := send(class("NSMutableDictionary"), "dictionary")
	if font := menuFont(); font != 0 {
		send(attrs, "setObject:forKey:", uintptr(font), uintptr(nsString("NSFont")))
	}
	if badge == "" || tab <= 0 {
		return autorelease(send(send(class("NSAttributedString"), "alloc"), "initWithString:attributes:",
			uintptr(nsString(label)), uintptr(attrs)))
	}
	style := autorelease(send(send(class("NSMutableParagraphStyle"), "alloc"), "init"))
	stop := autorelease(msgInitTextTab(send(class("NSTextTab"), "alloc"),
		sel("initWithTextAlignment:location:options:"), 2 /* right */, tab, 0))
	send(style, "setTabStops:", uintptr(nsArray(stop)))
	send(attrs, "setObject:forKey:", uintptr(style), uintptr(nsString("NSParagraphStyle")))
	title := autorelease(send(send(class("NSMutableAttributedString"), "alloc"), "initWithString:attributes:",
		uintptr(nsString(label+"\t"+badge)), uintptr(attrs)))
	if font := menuBadgeFont(); font != 0 {
		send(title, "addAttribute:value:range:",
			uintptr(nsString("NSFont")), uintptr(font),
			uintptr(units([]rune(label+"\t"))), uintptr(units([]rune(badge))))
	}
	return title
}

// setItemTitle shows the label of it and, at the right-aligned tab stop
// tab, its badge in a monospaced-digit font.
func setItemTitle(item id, it *platform.MenuItem, tab float64) {
	label := strings.ReplaceAll(it.Label, "\t", " ")
	if it.Badge == "" || tab <= 0 {
		send(item, "setAttributedTitle:", 0)
		send(item, "setTitle:", uintptr(nsString(label)))
		return
	}
	send(item, "setAttributedTitle:", uintptr(menuTitle(it.Label, it.Badge, tab)))
}

// The layout of the view of an item that keeps its menu open: a check
// mark column, the label and its badge, and room after them.
const (
	menuKeepOpenHeight = 22
	menuKeepOpenLabelX = 26
	menuKeepOpenRight  = 10
	menuViewBackdrop   = 1
	menuViewCheck      = 2
	menuViewLabel      = 3
)

// keepOpenMetrics returns the x badges end at in the views of m's KeepOpen
// items and the width those views share, zero when it has none.
func keepOpenMetrics(m *platform.Menu) (tab, width float64) {
	found := false
	for _, it := range m.Items {
		if it.Visible && it.KeepOpen && it.Submenu == nil && it.Type != platform.MenuItemSeparator {
			found = true
			break
		}
	}
	if !found {
		return 0, 0
	}
	labelFont := menuFont()
	hasBadge := false
	var maxLabel, maxBadge float64
	for _, it := range m.Items {
		if !it.Visible || !it.KeepOpen || it.Submenu != nil || it.Type == platform.MenuItemSeparator {
			continue
		}
		maxLabel = max(maxLabel, textWidth(it.Label, labelFont))
		if it.Badge != "" {
			hasBadge = true
			maxBadge = max(maxBadge, textWidth(it.Badge, menuBadgeFont()))
		}
	}
	tab = maxLabel
	if hasBadge {
		tab = maxLabel + menuBadgeGap + maxBadge
	}
	return tab, menuKeepOpenLabelX + tab + menuKeepOpenRight
}

// buildKeepOpenView returns the view of an item that keeps its menu open:
// MyGo draws the check mark, label and badge, and handles the mouse itself
// so NSMenu does not end its tracking when the item is clicked.
func buildKeepOpenView(it *platform.MenuItem, metrics menuMetrics) id {
	width := max(metrics.keepOpenWidth, menuKeepOpenLabelX+120)
	view := autorelease(msgInitRect(send(class("MyGoMenuKeepOpenView"), "alloc"),
		sel("initWithFrame:"), NSRect{Size: NSSize{width, menuKeepOpenHeight}}))
	send(view, "setIdentifier:", uintptr(nsString(strconv.Itoa(it.ID))))
	send(view, "setWantsLayer:", 1)
	send(view, "setAutoresizingMask:", 2) // NSViewWidthSizable
	layer := send(view, "layer")
	if layer != 0 {
		backdrop := autorelease(send(send(class("CALayer"), "alloc"), "init"))
		msgSetRect(backdrop, sel("setFrame:"), NSRect{
			Origin: NSPoint{X: 4, Y: 1},
			Size:   NSSize{Width: width - 8, Height: menuKeepOpenHeight - 2},
		})
		msgSetFloat(backdrop, sel("setCornerRadius:"), 4)
		send(backdrop, "setHidden:", 1)
		send(layer, "addSublayer:", uintptr(backdrop))
	}

	check := autorelease(msgInitRect(send(class("NSImageView"), "alloc"), sel("initWithFrame:"),
		NSRect{Origin: NSPoint{X: 6, Y: (menuKeepOpenHeight - 14) / 2}, Size: NSSize{Width: 14, Height: 14}}))
	send(check, "setImage:", uintptr(send(class("NSImage"), "imageNamed:", uintptr(nsString("NSMenuOnStateTemplate")))))
	send(check, "setTag:", menuViewCheck)
	send(view, "addSubview:", uintptr(check))

	label := autorelease(msgInitRect(send(class("NSTextField"), "alloc"), sel("initWithFrame:"),
		NSRect{Origin: NSPoint{X: menuKeepOpenLabelX, Y: 2},
			Size: NSSize{Width: width - menuKeepOpenLabelX - 4, Height: menuKeepOpenHeight - 4}}))
	send(label, "setBezeled:", 0)
	send(label, "setEditable:", 0)
	send(label, "setSelectable:", 0)
	send(label, "setDrawsBackground:", 0)
	send(label, "setAlignment:", 0)
	send(label, "setFont:", uintptr(menuFont()))
	send(label, "setTag:", menuViewLabel)
	send(label, "setAutoresizingMask:", 2) // NSViewWidthSizable
	send(label, "setAttributedStringValue:", uintptr(menuTitle(it.Label, it.Badge, metrics.keepOpenTab)))
	send(view, "addSubview:", uintptr(label))

	const options = 0x01 | 0x80 | 0x200 // entered/exited, active always, visible rect
	tracking := msgInitMenuTrack(send(class("NSTrackingArea"), "alloc"),
		sel("initWithRect:options:owner:userInfo:"), NSRect{}, options, view, 0)
	send(view, "addTrackingArea:", uintptr(tracking))
	release(tracking)
	return view
}

// updateKeepOpenView follows the state of it in the view of a KeepOpen item.
func updateKeepOpenView(view id, it *platform.MenuItem, tab float64) {
	send(view, "setIdentifier:", uintptr(nsString(strconv.Itoa(it.ID))))
	if label := send(view, "viewWithTag:", menuViewLabel); label != 0 {
		send(label, "setAttributedStringValue:", uintptr(menuTitle(it.Label, it.Badge, tab)))
	}
	if check := send(view, "viewWithTag:", menuViewCheck); check != 0 {
		checked := it.Checked && (it.Type == platform.MenuItemCheckbox || it.Type == platform.MenuItemRadio)
		send(check, "setHidden:", boolArg(!checked))
	}
	alpha := 1.0
	if !it.Enabled {
		alpha = 0.45
	}
	msgSetFloat(view, sel("setAlphaValue:"), alpha)
	setMenuViewColors(view)
}

// menuBackdrop returns the selection layer of a KeepOpen view.
func menuBackdrop(view id) id {
	sublayers := send(send(view, "layer"), "sublayers")
	if sublayers == 0 {
		return 0
	}
	layers := arrayItems(sublayers)
	if len(layers) == 0 {
		return 0
	}
	return layers[0]
}

// setMenuViewHighlighted shows the selection of a KeepOpen view under the
// mouse, as NSMenu does for the items it draws itself.
func setMenuViewHighlighted(view id, on bool) {
	if backdrop := menuBackdrop(view); backdrop != 0 {
		if on {
			color := send(class("NSColor"), "selectedContentBackgroundColor")
			send(backdrop, "setBackgroundColor:", uintptr(send(color, "CGColor")))
		}
		send(backdrop, "setHidden:", boolArg(!on))
	}
	setMenuViewColors(view)
}

// setMenuViewColors colors the label of a KeepOpen view for its state.
func setMenuViewColors(view id) {
	label := send(view, "viewWithTag:", menuViewLabel)
	if label == 0 {
		return
	}
	name := "labelColor"
	if backdrop := menuBackdrop(view); backdrop != 0 && !sendBool(backdrop, "isHidden") {
		name = "alternateSelectedControlTextColor"
	} else if item := send(view, "enclosingMenuItem"); item != 0 && !sendBool(item, "isEnabled") {
		name = "disabledControlTextColor"
	}
	send(label, "setTextColor:", uintptr(send(class("NSColor"), name)))
}

func applyItemState(item id, it *platform.MenuItem) {
	send(item, "setEnabled:", boolArg(it.Enabled))
	send(item, "setHidden:", boolArg(!it.Visible))
	state := uintptr(0)
	if it.Checked && (it.Type == platform.MenuItemCheckbox || it.Type == platform.MenuItemRadio) {
		state = 1
	}
	send(item, "setState:", state)
	if it.ToolTip != "" {
		send(item, "setToolTip:", uintptr(nsString(it.ToolTip)))
	}
}

func (b *Backend) UpdateMenuItem(it *platform.MenuItem) {
	withPool(func() {
		key, mods := keyEquivalent(it.Accelerator)
		for _, n := range itemsByID[it.ID] {
			// The hidden Cmd+= twin of "Zoom In" only follows the enabled state.
			if it.Role == "zoomIn" && sendBool(n.item, "isHidden") && respondsTo(n.item, "allowsKeyEquivalentWhenHidden") &&
				sendBool(n.item, "allowsKeyEquivalentWhenHidden") {
				send(n.item, "setEnabled:", boolArg(it.Enabled))
				continue
			}
			var view id
			if it.Type != platform.MenuItemSeparator {
				view = send(n.item, "view")
				if view == 0 {
					setItemTitle(n.item, it, n.badgeTab)
				}
				if sub := send(n.item, "submenu"); sub != 0 {
					send(sub, "setTitle:", uintptr(nsString(it.Label)))
				}
				send(n.item, "setKeyEquivalent:", uintptr(nsString(key)))
				send(n.item, "setKeyEquivalentModifierMask:", uintptr(mods))
			}
			applyItemState(n.item, it)
			if view != 0 {
				updateKeepOpenView(view, it, n.badgeTab)
			}
		}
	})
}

func (b *Backend) PopupMenu(m *platform.Menu, pw platform.Window, pos *platform.Point) {
	withPool(func() {
		owner := newOwner()
		defer dropOwner(owner)
		menu := b.buildMenu(m, "", owner)
		var view id
		var loc NSPoint
		if w, ok := pw.(*window); ok && w != nil && !w.closed {
			view = w.web
			if w.surface != nil {
				view = w.surface.view
			}
			if pos != nil {
				loc = NSPoint{float64(pos.X), float64(pos.Y)}
				if !sendBool(view, "isFlipped") {
					loc.Y = msgRect(view, sel("bounds")).Size.Height - loc.Y
				}
			} else {
				p := msgPoint(w.win, sel("mouseLocationOutsideOfEventStream"))
				loc = msgPointFromView(view, sel("convertPoint:fromView:"), p, 0)
			}
		} else {
			loc = msgPoint(class("NSEvent"), sel("mouseLocation"))
		}
		if m.RightToLeft {
			// NSUserInterfaceLayoutDirectionRightToLeft lays the items out
			// from the right and opens submenus to the left. A menu popped
			// up in a view takes the view's direction, which the surface
			// has only while the menu shows.
			send(menu, "setUserInterfaceLayoutDirection:", 1)
			if view != 0 {
				was := sendInt(view, "userInterfaceLayoutDirection")
				send(view, "setUserInterfaceLayoutDirection:", 1)
				defer send(view, "setUserInterfaceLayoutDirection:", uintptr(was))
			}
		}
		msgPopUpMenu(menu, sel("popUpMenuPositioningItem:atLocation:inView:"), 0, loc, view)
	})
}

// keyEquivalent converts an accelerator to an NSMenuItem key equivalent and
// modifier mask.
func keyEquivalent(acc string) (string, uint) {
	if acc == "" {
		return "", 0
	}
	a, err := accelerator.Parse(acc, "darwin")
	if err != nil {
		return "", 0
	}
	var mods uint
	if a.Has(accelerator.Super) {
		mods |= 1 << 20
	}
	if a.Has(accelerator.Ctrl) {
		mods |= 1 << 18
	}
	if a.Has(accelerator.Alt) {
		mods |= 1 << 19
	}
	if a.Has(accelerator.Shift) {
		mods |= 1 << 17
	}
	if len(a.Key) == 1 {
		return a.Key, mods
	}
	named := map[string]rune{
		"Enter": '\r', "Tab": '\t', "Space": ' ', "Backspace": 0x08, "Delete": 0xF728, "Escape": 0x1b,
		"Up": 0xF700, "Down": 0xF701, "Left": 0xF702, "Right": 0xF703, "Home": 0xF729, "End": 0xF72B,
		"PageUp": 0xF72C, "PageDown": 0xF72D, "Insert": 0xF727, "PrintScreen": 0xF72E,
	}
	if r, ok := named[a.Key]; ok {
		return string(r), mods
	}
	var n int
	if len(a.Key) > 1 && a.Key[0] == 'F' {
		for _, c := range a.Key[1:] {
			n = n*10 + int(c-'0')
		}
		return string(rune(0xF704 + n - 1)), mods
	}
	return "", 0
}

// Tray icons (NSStatusItem).

type tray struct {
	b      *Backend
	h      platform.TrayHandler
	item   id
	button id
	owner  int
	// dark is the menu bar's appearance, as last told.
	dark bool
}

func registerTrayTarget() {
	classDef("MyGoTrayTarget", "NSObject", nil, []objc.MethodDef{
		method("mygoTrayClicked:", func(self id, _ objc.SEL, sender id) {
			t := theBackend.trays[sender]
			if t == nil {
				return
			}
			ev := send(theBackend.app, "currentEvent")
			if ev != 0 && sendInt(ev, "type") == 4 { // NSEventTypeRightMouseUp
				t.h.RightClicked()
			} else {
				t.h.Clicked()
			}
		}),
		// The menu bar under the button turned dark or light: with the
		// system's appearance, or the wallpaper under a translucent menu
		// bar.
		method("observeValueForKeyPath:ofObject:change:context:", func(self id, _ objc.SEL, keyPath, object, change id, ctx uintptr) {
			t := theBackend.trays[object]
			if t == nil {
				return
			}
			if dark := t.Dark(); dark != t.dark {
				t.dark = dark
				t.h.AppearanceChanged()
			}
		}),
	})
}

var trayTarget id

func (b *Backend) NewTray(h platform.TrayHandler) (platform.Tray, error) {
	if trayTarget == 0 {
		trayTarget = alloc("MyGoTrayTarget")
	}
	t := &tray{b: b, h: h, owner: newOwner()}
	withPool(func() {
		bar := send(class("NSStatusBar"), "systemStatusBar")
		t.item = retain(msgFloatID(bar, sel("statusItemWithLength:"), -1)) // NSVariableStatusItemLength
		t.button = send(t.item, "button")
		send(t.button, "setTarget:", uintptr(trayTarget))
		send(t.button, "setAction:", uintptr(sel("mygoTrayClicked:")))
		send(t.button, "sendActionOn:", 1<<2|1<<4) // left and right mouse up
		send(t.button, "addObserver:forKeyPath:options:context:", uintptr(trayTarget), uintptr(nsString("effectiveAppearance")), 0, 0)
	})
	b.trays[t.button] = t
	t.dark = t.Dark()
	return t, nil
}

// Dark reports whether the menu bar under the button is dark, as AppKit
// draws its own status items: by the button's appearance, which follows
// the menu bar's.
func (t *tray) Dark() bool {
	if t.item == 0 {
		return false
	}
	dark := false
	withPool(func() {
		names := nsArray(nsString("NSAppearanceNameAqua"), nsString("NSAppearanceNameDarkAqua"))
		match := send(send(t.button, "effectiveAppearance"), "bestMatchFromAppearancesWithNames:", uintptr(names))
		dark = match != 0 && goString(match) == "NSAppearanceNameDarkAqua"
	})
	return dark
}

// SetID gives the status item an autosave name: macOS keeps under it where
// the user dragged the item with ⌘, and puts it there as it is made again.
func (t *tray) SetID(id string) {
	withPool(func() { send(t.item, "setAutosaveName:", uintptr(nsString(id))) })
}

func (t *tray) SetImage(png []byte, template bool) error {
	var err error
	withPool(func() {
		img := autorelease(send(send(class("NSImage"), "alloc"), "initWithData:", uintptr(nsData(png))))
		if img == 0 {
			err = errInvalidImage
			return
		}
		// Menu bar icons are 18 points tall.
		size := msgSize(img, sel("size"))
		if size.Height > 0 {
			msgSetSize(img, sel("setSize:"), NSSize{size.Width * 18 / size.Height, 18})
		}
		send(img, "setTemplate:", boolArg(template))
		send(t.button, "setImage:", uintptr(img))
	})
	return err
}

func (t *tray) SetTitle(title string) {
	withPool(func() { send(t.button, "setTitle:", uintptr(nsString(title))) })
}

func (t *tray) SetToolTip(tip string) {
	withPool(func() { send(t.button, "setToolTip:", uintptr(nsString(tip))) })
}

func (t *tray) SetMenu(m *platform.Menu) {
	withPool(func() {
		dropOwner(t.owner)
		if m == nil {
			send(t.item, "setMenu:", 0)
			return
		}
		send(t.item, "setMenu:", uintptr(t.b.buildMenu(m, "", t.owner)))
	})
}

// SetActivatable does nothing: AppKit reports its clicks itself.
func (t *tray) SetActivatable(bool) {}

func (t *tray) PopUpMenu(m *platform.Menu) {
	if m == nil || t.item == 0 {
		return
	}
	withPool(func() {
		owner := newOwner()
		defer dropOwner(owner)
		menu := t.b.buildMenu(m, "", owner)
		// Let the status item place and track its menu, including the
		// button's highlight. Keep its previous menu alive while swapped.
		previous := retain(send(t.item, "menu"))
		defer release(previous)
		send(t.item, "setMenu:", uintptr(menu))
		send(t.button, "performClick:", 0)
		// Menu actions run in a nested event loop: they may destroy the
		// tray or install another menu, which we must leave intact.
		if t.item != 0 && send(t.item, "menu") == menu {
			send(t.item, "setMenu:", uintptr(previous))
		}
	})
}

func (t *tray) Bounds() platform.Rect {
	win := send(t.button, "window")
	if win == 0 {
		return platform.Rect{}
	}
	return rectFromMac(msgRect(win, sel("frame")))
}

func (t *tray) Destroy() {
	if t.item == 0 {
		return
	}
	delete(t.b.trays, t.button)
	send(t.button, "removeObserver:forKeyPath:", uintptr(trayTarget), uintptr(nsString("effectiveAppearance")))
	dropOwner(t.owner)
	send(send(class("NSStatusBar"), "systemStatusBar"), "removeStatusItem:", uintptr(t.item))
	release(t.item)
	t.item = 0
}
