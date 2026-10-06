//go:build darwin

package darwin

// TestAccessText is text read through the native provider, with native
// UTF-16 offsets on macOS. It is used by the GUI tests.
type TestAccessText struct {
	Content, Selected, FirstLine string
	Start, End                   int
	Bounds                       [4]float64
	Visible, Selectable          bool
}

func TestAccessibilityText(handle uintptr, label string) (out TestAccessText, ok bool) {
	for _, n := range TestAccessibility(handle) {
		if n.Label != label {
			continue
		}
		withPool(func() {
			count := sendInt(n.obj, "accessibilityNumberOfCharacters")
			if count == 0 && n.Subrole == "AXSecureTextField" {
				return
			}
			r := rangeValue(nsRange{Length: uint(count)})
			query := func(attr string, param id) id {
				return send(n.obj, "accessibilityAttributeValue:forParameter:", uintptr(nsString(attr)), uintptr(param))
			}
			out.Content = goString(query("AXStringForRange", r))
			out.Selected = goString(send(n.obj, "accessibilityAttributeValue:", uintptr(nsString("AXSelectedText"))))
			selectedRangeValue := send(n.obj, "accessibilityAttributeValue:", uintptr(nsString("AXSelectedTextRange")))
			sr := rangeOfValue(selectedRangeValue)
			out.Start, out.End = int(sr.Location), int(sr.Location+sr.Length)
			lr := query("AXRangeForLine", nsNumberInt(0))
			out.FirstLine = goString(query("AXStringForRange", lr))
			if v := query("AXBoundsForRange", rangeValue(nsRange{Length: 1})); v != 0 {
				b := msgRect(v, sel("rectValue"))
				out.Bounds = [4]float64{b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height}
			}
			vr := rangeOfValue(send(n.obj, "accessibilityAttributeValue:", uintptr(nsString("AXVisibleCharacterRange"))))
			out.Visible = vr.Length > 0
			out.Selectable = sendBool(n.obj, "accessibilityIsAttributeSettable:", uintptr(nsString("AXSelectedTextRange")))
			ok = true
		})
		return
	}
	return
}

func TestAccessibilitySelectText(handle uintptr, label string, start, end int) bool {
	for _, n := range TestAccessibility(handle) {
		if n.Label == label {
			allowed := false
			withPool(func() {
				allowed = sendBool(n.obj, "accessibilityIsAttributeSettable:", uintptr(nsString("AXSelectedTextRange")))
				if allowed {
					count := sendInt(n.obj, "accessibilityNumberOfCharacters")
					text := []rune(goString(send(n.obj, "accessibilityStringForRange:", 0, uintptr(count))))
					a, b := units(text[:max(0, min(start, len(text)))]), units(text[:max(0, min(end, len(text)))])
					send(n.obj, "setAccessibilitySelectedTextRange:", uintptr(a), uintptr(max(0, b-a)))
				}
			})
			return allowed
		}
	}
	return false
}
