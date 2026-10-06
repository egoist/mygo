//go:build darwin

package darwin

// TestNativeViewFrame reads the actual AppKit geometry and focus of a
// hosted control, on the main thread.
func TestNativeViewFrame(parent uintptr) (bounds, clip [4]float64, visible, focused bool) {
	n := hostedViews[id(parent)]
	if n == nil || n.closed {
		return
	}
	c, v := msgRect(n.clip, sel("frame")), msgRect(n.view, sel("frame"))
	clip = [4]float64{c.Origin.X, c.Origin.Y, c.Size.Width, c.Size.Height}
	bounds = [4]float64{c.Origin.X + v.Origin.X, c.Origin.Y + v.Origin.Y, v.Size.Width, v.Size.Height}
	return bounds, clip, !sendBool(n.clip, "isHidden"), n.hasFocus()
}

// TestNativeViewTab delivers a real NSEvent through the NSWindow's
// normal event dispatch while a hosted control owns the first responder.
func TestNativeViewTab(parent uintptr, backward bool) bool {
	n := hostedViews[id(parent)]
	if n == nil || n.closed || !n.hasFocus() {
		return false
	}
	withPool(func() {
		var mods uint
		if backward {
			mods = 1 << 17
		}
		chars := nsString("\t")
		number := sendInt(n.s.w.win, "windowNumber")
		for _, typ := range []uint{10, 11} {
			ev := msgKeyEvent(class("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"), typ, NSPoint{}, mods, 0, number, 0, chars, chars, false, 48)
			send(n.s.w.win, "sendEvent:", uintptr(ev))
		}
	})
	return true
}

// TestNativeViewText commits text through the hosted field editor's
// NSTextInputClient, as an input method does, independent of input source.
func TestNativeViewText(parent uintptr, text string) bool {
	n := hostedViews[id(parent)]
	if n == nil || n.closed {
		return false
	}
	n.Focus(false)
	r := send(n.s.w.win, "firstResponder")
	if !n.hasFocus() || !respondsTo(r, "insertText:replacementRange:") {
		return false
	}
	withPool(func() {
		msgInsertText(r, sel("insertText:replacementRange:"), nsString(text), nsRange{Location: nsNotFound})
	})
	return true
}

// TestNativeViewAboveScene checks actual layer ordering, including when
// Metal creates its layer after the native subview has been attached.
func TestNativeViewAboveScene(parent uintptr) bool {
	n := hostedViews[id(parent)]
	if n == nil || n.closed {
		return false
	}
	nativeLayer := send(n.clip, "layer")
	z := msgFloat(nativeLayer, sel("zPosition"))
	for _, layer := range arrayItems(send(send(n.s.view, "layer"), "sublayers")) {
		if layer != nativeLayer && msgFloat(layer, sel("zPosition")) >= z {
			return false
		}
	}
	return z > 0
}
