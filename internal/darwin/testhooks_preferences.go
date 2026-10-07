//go:build darwin

package darwin

// TestScrollbarPreference sets AppKit's scroller preference in the process's
// volatile argument domain. It never writes the user's defaults. Main thread
// only; the returned function restores the domain and posts native changes.
func TestScrollbarPreference(always bool) func() {
	var defaults, saved id
	withPool(func() {
		defaults = send(class("NSUserDefaults"), "standardUserDefaults")
		saved = retain(send(defaults, "volatileDomainForName:", uintptr(nsString("NSArgumentDomain"))))
		next := autorelease(send(saved, "mutableCopy"))
		if next == 0 {
			next = send(class("NSMutableDictionary"), "dictionary")
		}
		style := "WhenScrolling"
		if always {
			style = "Always"
		}
		send(next, "setObject:forKey:", uintptr(nsString(style)), uintptr(nsString("AppleShowScrollBars")))
		send(defaults, "setVolatileDomain:forName:", uintptr(next), uintptr(nsString("NSArgumentDomain")))
		postScrollerDefaultsChanged(defaults)
	})
	return func() {
		withPool(func() {
			send(defaults, "setVolatileDomain:forName:", uintptr(saved), uintptr(nsString("NSArgumentDomain")))
			release(saved)
			postScrollerDefaultsChanged(defaults)
		})
	}
}

func postScrollerDefaultsChanged(defaults id) {
	center := send(class("NSNotificationCenter"), "defaultCenter")
	send(center, "postNotificationName:object:", uintptr(nsString("NSUserDefaultsDidChangeNotification")), uintptr(defaults))
	send(center, "postNotificationName:object:", uintptr(nsString("NSPreferredScrollerStyleDidChangeNotification")), 0)
}

// TestNotifyDisplayPreferences exercises NSWorkspace's real notification
// route without altering the desktop's accessibility settings.
func TestNotifyDisplayPreferences() {
	withPool(func() {
		send(send(workspace(), "notificationCenter"), "postNotificationName:object:",
			uintptr(nsString("NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification")), uintptr(workspace()))
	})
}
