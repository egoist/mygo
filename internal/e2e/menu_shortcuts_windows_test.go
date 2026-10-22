//go:build windows

package e2e

import (
	"strings"
	"testing"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// The shortcuts of the app's menu items work in a window of native UI, as
// in a page: before the content, which gets the rest. The roles' keys, as
// Edit › Copy's Ctrl+C, go to the content first, as a terminal's Ctrl+C.
func TestNativeMenuShortcuts(t *testing.T) {
	var settings, split, copied, typed int
	prev := mygo.App.Menu()
	defer mygo.App.SetMenu(prev)
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "App", Submenu: []*mygo.MenuItem{
			{Label: "Settings", Accelerator: "Ctrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { settings++ }},
			{Label: "Split", Accelerator: "Alt+Shift+=", Click: func(*mygo.MenuItem, *mygo.Window) { split++ }},
		}},
		{Role: mygo.RoleEditMenu},
	}))
	w := newWindow(t, mygo.WindowOptions{Width: 300, Height: 200, Content: ui.View(func(c *ui.Context) {
		c.OnShortcut(ui.Ctrl, ui.KeyC, func() { copied++ })
		c.OnShortcut(ui.Ctrl, ui.KeyComma, func() { typed++ })
		ui.Text(c, "shortcuts")
	})})
	const vkControl, vkShift, vkMenu, vkComma, vkEqual = 0x11, 0x10, 0x12, 0xBC, 0xBB
	key := func(vk byte, mods ...byte) {
		t.Helper()
		var ok bool
		mygo.RunOnMain(func() { ok = win.TestSurfaceKey(w.NativeHandle(), vk, mods...) })
		if !ok {
			t.Fatal("no window of native UI to press keys in")
		}
	}
	counts := func() (s, p, c, ty int) {
		mygo.RunOnMain(func() { s, p, c, ty = settings, split, copied, typed })
		return
	}
	key(vkComma, vkControl)
	key(vkEqual, vkMenu, vkShift)
	key('C', vkControl)
	eventually(t, "the content's Ctrl+C", func() bool { _, _, c, _ := counts(); return c == 1 })
	if s, p, _, ty := counts(); s != 1 || p != 1 || ty != 0 {
		t.Errorf("Ctrl+, ran Settings %d times and reached the content %d, Alt+Shift+= ran Split %d times; want once, never, once", s, ty, p)
	}
}

// A text field takes the edit keys it handles, as Ctrl+V: Edit › Paste,
// whose key it is too, doesn't paste again after it.
func TestNativeTextEditKeys(t *testing.T) {
	preserveClipboard(t)
	mygo.Clipboard.WriteText("X")
	prev := mygo.App.Menu()
	defer mygo.App.SetMenu(prev)
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleEditMenu}}))
	var text string
	var focused bool
	w := newWindow(t, mygo.WindowOptions{Width: 300, Height: 200, Content: ui.View(func(c *ui.Context) {
		focused = ui.TextInput(c, &text).AutoFocus().Focused()
	})})
	got := func() (s string, f bool) {
		mygo.RunOnMain(func() { s, f = text, focused })
		return
	}
	var ok bool
	mygo.RunOnMain(func() { ok = win.TestForeground(w.NativeHandle()) })
	if !ok {
		t.Fatal("the window did not come to the front")
	}
	// The key goes to the field once it has the keyboard.
	eventually(t, "the field to have the keyboard", func() bool { _, f := got(); return f })
	// The key and the item's command, had it run, both come before the
	// frame that applies them.
	mygo.RunOnMain(func() { ok = win.TestSurfaceKey(w.NativeHandle(), 'V', 0x11) }) // VK_CONTROL
	if !ok {
		t.Fatal("no window of native UI to press keys in")
	}
	eventually(t, "Ctrl+V to paste", func() bool { s, _ := got(); return s != "" })
	if s, _ := got(); s != "X" {
		t.Errorf("Ctrl+V made the text %q, want X", s)
	}
}

// In a page, the edit roles' keys are the page's, as in a browser and as in
// native UI: WebView2 pastes with the page's own paste event and every
// format of the clipboard, where Edit › Paste would type its text alone.
func TestPageEditKeys(t *testing.T) {
	prev := mygo.App.Menu()
	defer mygo.App.SetMenu(prev)
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleEditMenu}}))
	w := newWindow(t, mygo.WindowOptions{Width: 400, Height: 300})
	w.Page().LoadHTML(`<textarea id=t></textarea><script>
addEventListener("paste", (e) => { window.pasted = e.clipboardData.types.join(" ") })
</script>`, "")
	waitFor(t, w, "document.getElementById('t') !== null")
	if err := mygo.Clipboard.Write(transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.Text, []byte("MyGo")), transfer.Bytes(transfer.HTML, []byte("<b>MyGo</b>"))))); err != nil {
		t.Fatal(err)
	}
	var front bool
	mygo.RunOnMain(func() { front = win.TestForeground(w.NativeHandle()) })
	if !front {
		t.Fatal("the window did not come to the front")
	}
	if _, err := w.Page().Eval("document.getElementById('t').focus()"); err != nil {
		t.Fatal(err)
	}
	mygo.RunOnMain(func() { win.TestPressKeys(0x11, 'V') }) // VK_CONTROL
	waitFor(t, w, "window.pasted !== undefined")
	if types, _ := mygo.EvalAs[string](w.Page(), "window.pasted"); !strings.Contains(types, "text/html") {
		t.Errorf("the page's paste had %q, want text/html among them", types)
	}
}
