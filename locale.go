package mygo

// Locale returns the window's native UI locale, inheriting App.Locale
// when the window has no override. It is safe from any goroutine.
func (w *Window) Locale() string {
	return onMainValue(w.effectiveLocale)
}

func (w *Window) effectiveLocale() string {
	if w.locale != "" {
		return w.locale
	}
	return App.Locale()
}

// SetLocale changes the native UI locale and redraws its content without
// resetting state. Empty inherits the app/OS locale. It is safe from any
// goroutine and does nothing after the window closes.
func (w *Window) SetLocale(tag string) {
	onMain(func() {
		if w.native != nil && w.locale != tag {
			w.locale = tag
			w.contentChanged()
		}
	})
}
