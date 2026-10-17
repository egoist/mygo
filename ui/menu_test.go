package ui

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestContextMenu(t *testing.T) {
	var (
		chosen       []string
		pinned       bool
		locked       = true
		rightClicked int
	)
	view := func(c *context) {
		coreColumn(c).Fill().Padding(20).Gap(10).Children(func() {
			row := coreRow(c).Padding(8).Children(func() {
				coreText(c, "Report.pdf")
			})
			row.ContextMenu(func(m *Menu) {
				if m.Item("Open").Shortcut(Cmd, KeyO).Chosen() {
					chosen = append(chosen, "Open")
				}
				if m.Item("Pinned").Checked(pinned).Chosen() {
					pinned = !pinned
				}
				m.Separator()
				m.Submenu("Move to", func(m *Menu) {
					for _, folder := range []string{"Desktop", "Documents"} {
						if m.Item(folder).Chosen() {
							chosen = append(chosen, "Move to "+folder)
						}
					}
				})
				if m.Item("Delete").Disabled(locked).Chosen() {
					chosen = append(chosen, "Delete")
				}
			})
			// Without a context menu, a right-click is the box's.
			if coreBox(c).Size(60, 30).Label("plain").RightClicked() {
				rightClicked++
			}
			coreText(c, map[bool]string{false: "loose", true: "pinned"}[pinned])
		})
	}
	tt := coreNewTester(view, 400, 300)
	if tt.Menu() != nil {
		t.Fatalf("a menu shows before any click: %q", tt.Menu())
	}
	if err := tt.RightClick("Report.pdf"); err != nil {
		t.Fatal(err)
	}
	if got, want := tt.Menu(), []string{"Open", "Pinned", "-", "Move to", "Delete"}; !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
	items := tt.h.menu.Items
	if items[0].Accelerator != accelerator(Cmd, KeyO) || items[0].Accelerator == "" {
		t.Errorf("Open shows the shortcut %q", items[0].Accelerator)
	}
	if items[1].Type != platform.MenuItemCheckbox || items[1].Checked {
		t.Errorf("Pinned is %+v, an unchecked check box item", items[1])
	}
	if items[4].Enabled {
		t.Error("Delete is enabled while locked")
	}
	if err := tt.ChooseMenuItem("Delete"); err == nil {
		t.Error("chose a disabled item")
	}
	if err := tt.ChooseMenuItem("Pinned"); err != nil {
		t.Fatal(err)
	}
	if tt.Menu() != nil {
		t.Error("the menu stays after a choice")
	}
	if !pinned || !tt.HasText("pinned") {
		t.Errorf("choosing Pinned left pinned %v, texts %q", pinned, tt.Texts())
	}

	tt.RightClick("Report.pdf")
	if !tt.h.menu.Items[1].Checked {
		t.Error("Pinned has no check mark once pinned")
	}
	if err := tt.ChooseMenuItem("Move to", "Documents"); err != nil {
		t.Fatal(err)
	}
	locked = false
	tt.Frame()
	tt.RightClick("Report.pdf")
	if err := tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Move to Documents", "Delete"}; !slices.Equal(chosen, want) {
		t.Errorf("chosen %q, want %q", chosen, want)
	}

	// A menu closed without a choice reports nothing.
	tt.RightClick("Report.pdf")
	tt.CloseMenu()
	tt.Frame()
	if len(chosen) != 2 {
		t.Errorf("closing the menu chose %q", chosen[2:])
	}

	tt.RightClick("plain")
	if tt.Menu() != nil || rightClicked != 1 {
		t.Errorf("a right-click on an element without a menu: menu %q, RightClicked %d times", tt.Menu(), rightClicked)
	}
}

func TestInnermostContextMenu(t *testing.T) {
	var got string
	view := func(c *context) {
		card := coreColumn(c).Padding(20).Children(func() {
			coreText(c, "Card")
			coreButton(c, "Share").ContextMenu(func(m *Menu) {
				if m.Item("Copy Link").Chosen() {
					got = "Copy Link"
				}
			})
		})
		card.ContextMenu(func(m *Menu) {
			if m.Item("Remove Card").Chosen() {
				got = "Remove Card"
			}
		})
	}
	tt := coreNewTester(view, 400, 300)
	tt.RightClick("Share")
	if m := tt.Menu(); !slices.Equal(m, []string{"Copy Link"}) {
		t.Fatalf("the button's menu is %q", m)
	}
	tt.CloseMenu()
	tt.RightClick("Card")
	if err := tt.ChooseMenuItem("Remove Card"); err != nil {
		t.Fatal(err)
	}
	if got != "Remove Card" {
		t.Errorf("chose %q", got)
	}
}

func TestContextMenuFromTheKeyboard(t *testing.T) {
	var chosen int
	view := func(c *context) {
		coreColumn(c).Padding(20).Gap(8).Children(func() {
			coreButton(c, "First")
			coreButton(c, "Second").ContextMenu(func(m *Menu) {
				if m.Item("Duplicate").Chosen() {
					chosen++
				}
			})
		})
	}
	tt := coreNewTester(view, 400, 300)
	tt.Key(0, KeyTab)
	tt.Key(0, KeyContextMenu)
	if tt.Menu() != nil {
		t.Fatalf("the menu key opened %q over a button without a menu", tt.Menu())
	}
	tt.Key(0, KeyTab)
	tt.Key(0, KeyContextMenu)
	if !slices.Equal(tt.Menu(), []string{"Duplicate"}) {
		t.Fatalf("the menu key opened %q", tt.Menu())
	}
	tt.CloseMenu()
	tt.Key(Shift, KeyF10)
	if err := tt.ChooseMenuItem("Duplicate"); err != nil {
		t.Fatalf("Shift+F10: %v", err)
	}
	if chosen != 1 {
		t.Errorf("chosen %d times", chosen)
	}
}

func TestTextInputContextMenu(t *testing.T) {
	value := "hello world"
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Padding(20).Children(func() {
			coreTextInput(c, &value).Label("Name")
		})
	}, 400, 200)
	r, ok := tt.Find("Name")
	if !ok {
		t.Fatal("no text input")
	}
	// A right-click focuses the input and puts the caret where it is.
	tt.RightClickAt(r.X+r.W-4, r.Y+r.H/2)
	if !tt.Focused("Name") {
		t.Error("the right-click left the input without the focus")
	}
	menu := tt.Menu()
	for _, label := range []string{"Cut", "Copy", "Paste", "Select All"} {
		if !slices.Contains(menu, label) {
			t.Fatalf("the menu %q has no %s", menu, label)
		}
	}
	if err := tt.ChooseMenuItem("Copy"); err == nil {
		t.Error("copied without a selection")
	}
	if err := tt.ChooseMenuItem("Select All"); err != nil {
		t.Fatal(err)
	}
	// In the selection, a right-click keeps it.
	tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	if err := tt.ChooseMenuItem("Copy"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "hello world" {
		t.Errorf("copied %q", tt.Clipboard())
	}
	tt.SetClipboard("bye")
	tt.Key(0, KeyContextMenu)
	if err := tt.ChooseMenuItem("Paste"); err != nil {
		t.Fatal(err)
	}
	if value != "bye" {
		t.Errorf("pasting over the selection left %q", value)
	}
}

func TestAccelerator(t *testing.T) {
	for _, c := range []struct {
		mods Modifiers
		key  Key
		want string
	}{
		{Super, KeyC, "Super+c"},
		{Ctrl | Shift, KeyZ, "Ctrl+Shift+z"},
		{Alt, KeyF4, "Alt+F4"},
		{0, KeyDelete, "Delete"},
		{Ctrl, KeyComma, "Ctrl+,"},
		{Ctrl, KeyContextMenu, ""},
	} {
		if got := accelerator(c.mods, c.key); got != c.want {
			t.Errorf("accelerator(%v, %v) = %q, want %q", c.mods, c.key, got, c.want)
		}
	}
}

// sortView is a menu button choosing an order, with a button after it.
type sortView struct {
	sort     string
	disabled bool
}

func (s *sortView) view(c *context) {
	coreColumn(c).Padding(20).Gap(10).AlignItems(Start).Children(func() {
		coreMenuButton(c, "Sort by", func(m *Menu) {
			for _, by := range []string{"Name", "Date", "Size"} {
				if m.Item(by).Checked(s.sort == by).Chosen() {
					s.sort = by
				}
			}
		}).Disabled(s.disabled)
		coreButton(c, "Other")
	})
}

func TestMenuButton(t *testing.T) {
	s := &sortView{sort: "Name"}
	tt := coreNewTester(s.view, 400, 300)
	// The menu opens as the button goes down, below it.
	r, _ := tt.Find("Sort by")
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if got, want := tt.Menu(), []string{"Name", "Date", "Size"}; !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
	if at := tt.h.menuAt; at[0] > r.X || at[1] < r.Y+r.H || at[1] > r.Y+r.H+20 {
		t.Errorf("the menu shows at %v, the label being at %v", at, r)
	}
	if !tt.h.menu.Items[0].Checked {
		t.Error("Name is not checked")
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if err := tt.ChooseMenuItem("Date"); err != nil {
		t.Fatal(err)
	}
	if s.sort != "Date" || !tt.Focused("Sort by") {
		t.Fatalf("chose %q; focused %v", s.sort, tt.Focused("Sort by"))
	}
	// The keys open it while it has the focus.
	for _, k := range []Key{KeyEnter, KeySpace, KeyDown} {
		tt.CloseMenu()
		tt.Key(0, k)
		if tt.Menu() == nil {
			t.Errorf("%v does not open the menu", k)
		}
	}
	tt.CloseMenu()
	tt.Key(0, KeyTab)
	tt.Key(0, KeyEnter)
	if tt.Menu() != nil {
		t.Error("Enter on the next button opens the menu")
	}
	s.disabled = true
	tt.Frame()
	tt.Click("Sort by")
	if tt.Menu() != nil {
		t.Error("a disabled menu button opens its menu")
	}
}

func TestMenuButtonAccessibility(t *testing.T) {
	s := &sortView{sort: "Name"}
	tt := coreNewTester(s.view, 400, 300)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	n := accessNode(t, tt.h.access, platform.RoleMenuButton, "Sort by")
	if n.Actions&platform.ActionPress == 0 || n.States&platform.AccessFocusable == 0 {
		t.Errorf("the menu button: %+v", n)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: n.ID, Action: platform.AccessPress})
	if tt.Menu() == nil {
		t.Error("pressing it does not open the menu")
	}
}

func TestMenuAndContextMenuOfOneElement(t *testing.T) {
	var chosen []string
	tt := coreNewTester(func(c *context) {
		coreBox(c).Size(100, 40).Label("Both").Menu(func(m *Menu) {
			if m.Item("From the menu").Chosen() {
				chosen = append(chosen, "menu")
			}
		}).ContextMenu(func(m *Menu) {
			if m.Item("From the context menu").Chosen() {
				chosen = append(chosen, "context")
			}
		})
	}, 300, 200)
	tt.Click("Both")
	if got := tt.Menu(); !slices.Equal(got, []string{"From the menu"}) {
		t.Fatalf("a click shows %q", got)
	}
	tt.ChooseMenuItem("From the menu")
	tt.RightClick("Both")
	if got := tt.Menu(); !slices.Equal(got, []string{"From the context menu"}) {
		t.Fatalf("a right-click shows %q", got)
	}
	tt.ChooseMenuItem("From the context menu")
	if !slices.Equal(chosen, []string{"menu", "context"}) {
		t.Errorf("chose %q", chosen)
	}
}

// A window whose app draws its menus (DrawnMenu) hands each one over, as
// the system's would show it: a view's at the point, a menu button's from
// the button, a text input's editing menu; the item chosen reaches the
// view as from the system's menu. A view that stops drawing them has the
// system's again.
func TestDrawnMenu(t *testing.T) {
	var (
		chosen []string
		shown  *ShownMenu
		text   = "hello"
		draws  = true
		button Rect
	)
	view := func(c *context) {
		cx := makeContext(c)
		coreColumn(c).Fill().Padding(20).Gap(10).Children(func() {
			row := coreRow(c).Padding(8).Children(func() { coreText(c, "Report.pdf") })
			row.ContextMenu(func(m *Menu) {
				if m.Item("Open").Shortcut(Ctrl, KeyO).Chosen() {
					chosen = append(chosen, "Open")
				}
				m.Separator()
				m.Submenu("Move to", func(m *Menu) {
					if m.Item("Desktop").Checked(true).Chosen() {
						chosen = append(chosen, "Desktop")
					}
				})
				m.Item("Delete").Disabled(true)
			})
			more := coreButton(c, "More").Menu(func(m *Menu) { m.Item("Rename") })
			button = more.Bounds()
			coreTextInput(c, &text).Label("name")
		})
		shown = nil
		if draws {
			shown, _ = DrawnMenu(cx)
		}
	}
	tt := coreNewTester(view, 400, 300)
	if err := tt.RightClick("Report.pdf"); err != nil {
		t.Fatal(err)
	}
	if tt.h.menu != nil || shown == nil {
		t.Fatalf("the system's menu %v, the app's %v", tt.h.menu, shown)
	}
	at, _ := tt.Find("Report.pdf")
	if a := shown.Anchor; a.W != 0 || a.H != 0 || a.X < at.X || a.X > at.X+at.W || a.Y < at.Y || a.Y > at.Y+at.H {
		t.Errorf("a context menu anchored at %+v, not at the click in %+v", a, at)
	}
	items := shown.Items
	if len(items) != 4 {
		t.Fatalf("items %+v", items)
	}
	if items[0].Kind != MenuEntryItem || items[0].Label != "Open" || items[0].Keys != "Ctrl+O" {
		t.Errorf("Open: %+v", items[0])
	}
	if items[1].Kind != MenuEntrySeparator || items[3].Kind != MenuEntryItem || !items[3].Disabled {
		t.Errorf("the separator %+v, Delete %+v", items[1], items[3])
	}
	desktop := items[2].Submenu[0]
	if items[2].Kind != MenuEntrySubmenu || desktop.Kind != MenuEntryCheckbox || !desktop.Checked {
		t.Errorf("Move to %+v", items[2])
	}
	shown.Choose(desktop.ID)
	tt.Frame()
	if !slices.Equal(chosen, []string{"Desktop"}) || shown != nil {
		t.Fatalf("chosen %q, still shown %v", chosen, shown)
	}

	// The tester's helpers choose in a drawn menu too.
	tt.RightClick("Report.pdf")
	if err := tt.ChooseMenuItem("Open"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(chosen, []string{"Desktop", "Open"}) {
		t.Fatalf("chosen %q", chosen)
	}

	// A menu button's menu opens from the button.
	tt.Click("More")
	if shown == nil || shown.Anchor != button {
		t.Fatalf("the button's menu anchored at %+v, the button at %+v", shown, button)
	}
	tt.CloseMenu()

	// A text input's editing menu.
	tt.RightClick("name")
	if shown == nil || !slices.ContainsFunc(shown.Items, func(e MenuEntry) bool { return e.Label == "Select All" }) {
		t.Fatalf("the input's menu %+v", shown)
	}
	tt.CloseMenu()
	if shown != nil {
		t.Fatal("CloseMenu left the menu shown")
	}

	// The view draws them no more: the system's menu again.
	draws = false
	tt.Frame()
	tt.RightClick("Report.pdf")
	if !slices.Equal(tt.Menu(), []string{"Open", "-", "Move to", "Delete"}) || tt.rt.drawn != nil {
		t.Fatalf("the system's menu %q, drawn %v", tt.Menu(), tt.rt.drawn != nil)
	}
}

// A menu handed to the app shows in the frame after the one that built
// it, not at the next frame something else asks for; the item chosen
// acts, and the menu closes, in the frame that chose it.
func TestDrawnMenuFrames(t *testing.T) {
	var (
		shown  *ShownMenu
		chosen int
		frames int
	)
	view := func(c *context) {
		frames++
		cx := makeContext(c)
		row := coreRow(c).Padding(8).Focusable().Label("row").Children(func() { coreText(c, "Report.pdf") })
		row.AutoFocus()
		row.ContextMenu(func(m *Menu) {
			if m.Item("Open").Chosen() {
				chosen++
			}
		})
		shown, _ = DrawnMenu(cx)
		if shown != nil && frames > 100 {
			t.Fatal("frames never settle")
		}
	}
	tt := coreNewTester(view, 400, 300)
	tt.Frame()
	// One event, the key going down: nothing else asks for a frame.
	tt.KeyDown(KeyContextMenu)
	if shown == nil {
		t.Fatal("the menu asked for by the keys did not show: no frame drew it")
	}
	m := shown
	frames = 0
	m.Choose(m.Items[0].ID)
	if tt.FramePending() {
		tt.Frame()
	}
	if chosen != 1 || shown != nil {
		t.Fatalf("chosen %d, still shown %v", chosen, shown != nil)
	}
}

// An app's menu chooses while its view builds, as a click on a row of it
// does: the element whose menu it is hears of it in the same frame.
func TestDrawnMenuChosenInTheView(t *testing.T) {
	var (
		chosen int
		choose bool
	)
	view := func(c *context) {
		row := coreRow(c).Padding(8).Children(func() { coreText(c, "Report.pdf") })
		row.ContextMenu(func(m *Menu) {
			if m.Item("Open").Chosen() {
				chosen++
			}
		})
		if m, ok := DrawnMenu(makeContext(c)); ok && choose {
			m.Choose(m.Items[0].ID)
		}
	}
	tt := coreNewTester(view, 400, 300)
	tt.RightClick("Report.pdf")
	choose = true
	tt.Frame()
	if chosen != 1 || tt.rt.drawn != nil {
		t.Fatalf("chosen %d, still shown %v", chosen, tt.rt.drawn != nil)
	}
}
