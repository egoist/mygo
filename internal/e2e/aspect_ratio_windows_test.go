//go:build windows && (amd64 || arm64)

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
)

// TestWindowAspectRatio checks that the resize loop's WM_SIZING keeps the
// content at the ratio SetAspectRatio set, with the frame's room around it,
// and that clearing the ratio frees the resize again.
func TestWindowAspectRatio(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{X: 80, Y: 80, Width: 400, Height: 300})
	sizing := func(dw, dh int32) (int32, int32) {
		var cw, ch int32
		mygo.RunOnMain(func() { cw, ch = win.TestSizingAspect(w.NativeHandle(), 8 /* WMSZ_BOTTOMRIGHT */, dw, dh) })
		return cw, ch
	}
	cw, ch := sizing(200, 0)
	baseW, baseH := cw-200, ch
	w.SetAspectRatio(2)
	cw, ch = sizing(200, 0)
	if cw != baseW+200 || ch < (baseW+200)/2-1 || ch > (baseW+200)/2+1 {
		t.Errorf("ratio 2: content %dx%d, want %dx%d", cw, ch, baseW+200, (baseW+200)/2)
	}
	w.SetAspectRatio(0)
	cw, ch = sizing(200, 0)
	if cw != baseW+200 || ch != baseH {
		t.Errorf("ratio cleared: content %dx%d, want %dx%d", cw, ch, baseW+200, baseH)
	}
}