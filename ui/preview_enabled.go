//go:build !mygo_noinspector

package ui

// Content is a user interface for a window, the value of
// mygo.WindowOptions.Content. Create it with View.
type Content struct {
	view   func(*Context)
	attach func(*windowHost)
}

// Keep developer state in the inspector's build-specific storage. The
// production engine and Content retain their original shapes.
type previewRuntime struct {
	config      *PreviewConfig
	beforeFrame func()
	onClose     []func()
	closed      bool
}

func previewAttach(v *Content, h *windowHost) {
	if v.attach != nil {
		v.attach(h)
	}
}

func previewClosed(rt *engine) bool { return rt.insp.preview.closed }

func previewPrepareFrame(rt *engine) {
	if fn := rt.insp.preview.beforeFrame; fn != nil {
		fn()
	}
}

func previewPrepareSurface(rt *engine) bool {
	if previewClosed(rt) || rt.inFrame {
		return false
	}
	if rt.insp.preview.beforeFrame != nil {
		func() {
			rt.inFrame = true
			defer func() { rt.inFrame = false }()
			previewPrepareFrame(rt)
		}()
	}
	return !previewClosed(rt)
}

func previewAppearance(rt *engine, dark bool) bool {
	if p := rt.insp.preview.config; p != nil {
		switch p.Theme {
		case PreviewLight:
			return false
		case PreviewDark:
			return true
		}
	}
	return dark
}

func previewPreferences(rt *engine) (Preferences, bool) {
	if p := rt.insp.preview.config; p != nil && p.Preferences != nil {
		if !rt.prefsKnown {
			rt.prefs = *p.Preferences
			if rt.prefs.TextScale <= 0 {
				rt.prefs.TextScale = 1
			}
			rt.prefsKnown = true
		}
		return rt.prefs, true
	}
	return Preferences{}, false
}

func previewCloseBegin(rt *engine) bool {
	if previewClosed(rt) {
		return true
	}
	rt.insp.preview.closed = true
	return false
}

func previewCloseEnd(rt *engine) {
	for _, fn := range rt.insp.preview.onClose {
		fn()
	}
	rt.insp.preview.onClose = nil
}
