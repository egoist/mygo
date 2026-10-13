package mygo

import (
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

// The labels menu items take from their role (MenuItem.Role) are MyGo's
// own strings: they say the language the app shows (App.Locale), and an
// item whose label is its role's says the new language's when it changes.

// menuText returns the label of a role in the language the app shows: the
// framework's string of key (menuStrings).
func menuText(key, english string) string {
	locale := ""
	if s := locales.shown.Load(); s != nil {
		locale = s.locale
	}
	return menuStrings(locale, key, english)
}

// menuStrings returns MyGo's own string of key in locale, whose English is
// english: English until MyGo's catalog of its strings translates them.
var menuStrings = func(locale, key, english string) string { return english }

// menuTextApp returns the label of a role that names the app, as "Quit
// {app}", with name in place of {app}.
func menuTextApp(key, english, name string) string {
	return strings.ReplaceAll(menuText(key, english), "{app}", name)
}

// relabelMenus gives the items whose label is their role's the role's
// label now, as the language the app shows changed, and shows the menus
// in use with them: the menus of the app and of windows anew, and the
// others, as those of trays and the Dock, item by item. Main thread only.
func relabelMenus() {
	menuMu.Lock()
	var changed []*platform.MenuItem
	for _, wp := range menuItems.byID {
		it := wp.Value()
		if it == nil || it.Role == "" || it.roleLabel == "" || it.Label != it.roleLabel {
			continue
		}
		if label, _ := roleDefaults(it.Role); label != it.Label {
			it.Label, it.roleLabel = label, label
			changed = append(changed, it.snapshotLocked())
		}
	}
	menuMu.Unlock()
	if len(changed) == 0 || !App.IsReady() {
		return // menus take their labels as Run installs them
	}
	if m := App.Menu(); m != nil {
		backend().SetApplicationMenu(m.snapshot())
	}
	for _, w := range Windows() {
		if w.menu != nil && w.native != nil {
			w.native.SetMenu(w.menu.snapshot())
		}
	}
	for _, it := range changed {
		backend().UpdateMenuItem(it)
	}
}
