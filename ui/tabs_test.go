package ui

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// moveTab moves names[from] before names[to], as an app's Reorder does.
func moveTab(names []string, from, to int) []string {
	name := names[from]
	names = slices.Delete(names, from, from+1)
	if to > from {
		to--
	}
	return slices.Insert(names, to, name)
}

// tabsListPaints counts the paints of what tabsWith draws over its list,
// which the line showing where a tab goes draws after.
var tabsListPaints int

// tabsWith builds a tab list of names that reorders and closes them, its
// tabs each with a close button labeled "Close <name>", in a Row, or a
// Column when column is set.
func tabsWith(c *context, names *[]string, tab *int, column bool, changes *int) {
	tabs := coreTabsBase(c, tab, len(*names))
	tabs.Reorder(func(from, to int) { *names = moveTab(*names, from, to) })
	tabs.OnClose(func(i int) { *names = slices.Delete(*names, i, i+1) })
	tabs.List.DrawOver(func(*Painter, Rect) { tabsListPaints++ })
	if column {
		tabs.List.Column()
	}
	tabs.List.Gap(8).Children(func() {
		for i, name := range *names {
			c.nextKey, c.keySet = name, true // Context.Key: what a tab keeps moves with it
			tabs.Tab(i).Padding(4, 8).Gap(4).Children(func() {
				coreText(c, name)
				tabs.CloseButton(i).Label("Close " + name).Children(func() { coreText(c, "×") })
			})
		}
	})
	if tabs.List.Changed() {
		*changes++
	}
}

// Dragging a tab over another's far half puts it after that tab, and over
// its near half before it, along the list: across a row, right to left,
// and down a column. The tab dragged is chosen, and the choice follows
// it; Changed reports a choice of another tab.
func TestTabsBaseReorder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		column bool
		rtl    bool
	}{{"row", false, false}, {"right to left", false, true}, {"column", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			names := []string{"One", "Two", "Three", "Four"}
			tab, changes := 0, 0
			tt := coreNewTester(func(c *context) { tabsWith(c, &names, &tab, tc.column, &changes) }, 600, 400)
			if tc.rtl {
				tt.SetDirection(RightToLeft)
			}
			tt.UseScreenReader()
			var accent Color
			tt.Frame()
			tabBox := func(s string) Rect {
				info, ok := tt.AccessByRole(RoleTab, s)
				if !ok {
					t.Fatalf("no tab %s", s)
				}
				return info.Bounds
			}
			// far returns a point past the middle of tab s, along the
			// list, and near one short of it.
			far := func(s string) (float32, float32) {
				r := tabBox(s)
				switch {
				case tc.column:
					return r.X + 4, r.Y + r.H*3/4
				case tc.rtl:
					return r.X + r.W/4, r.Y + r.H/2
				}
				return r.X + r.W*3/4, r.Y + r.H/2
			}
			near := func(s string) (float32, float32) {
				r := tabBox(s)
				switch {
				case tc.column:
					return r.X + 4, r.Y + r.H/4
				case tc.rtl:
					return r.X + r.W*3/4, r.Y + r.H/2
				}
				return r.X + r.W/4, r.Y + r.H/2
			}
			dragTab := func(from string, x, y float32) {
				r := tabBox(from)
				drag(tt, r.X+r.W/2, r.Y+r.H/2, x, y)
				tt.Frame()
			}

			// While One is dragged past Three's middle, a line shows
			// beside Three's far end, half the gap past it.
			three := tabBox("Three")
			lx, ly := three.X+three.W+4, three.Y+three.H/2
			switch {
			case tc.column:
				lx, ly = three.X+three.W/2, three.Y+three.H+4
			case tc.rtl:
				lx = three.X - 4
			}
			isAccent := func() bool {
				c := tt.Image().RGBAAt(int(lx), int(ly))
				near := func(a, b uint8) bool { return a+12 >= b && b+12 >= a }
				return near(c.R, accent.R) && near(c.G, accent.G) && near(c.B, accent.B)
			}
			accent = tt.rt.c.theme.Accent
			if isAccent() {
				t.Fatalf("a line beside Three before any drag")
			}
			one := tabBox("One")
			x, y := far("Three")
			tt.Press(one.X+one.W/2, one.Y+one.H/2)
			for k := 1; k <= 4; k++ {
				f := float32(k) / 4
				tt.Move(one.X+one.W/2+(x-one.X-one.W/2)*f, one.Y+one.H/2+(y-one.Y-one.H/2)*f)
			}
			paints := tabsListPaints
			tt.Frame()
			if tabsListPaints == paints {
				t.Errorf("what the app draws over the list did not paint while a tab was dragged")
			}
			if !isAccent() {
				t.Errorf("no line beside Three while One is dragged past its middle: %v at %v, %v", tt.Image().RGBAAt(int(lx), int(ly)), lx, ly)
			}
			tt.Key(0, KeyEscape)
			tt.Release(x, y)
			tt.Frame()
			if isAccent() || !slices.Equal(names, []string{"One", "Two", "Three", "Four"}) {
				t.Fatalf("a drag given up with Escape: line %v, tabs %v", isAccent(), names)
			}

			// One, chosen, after Three.
			x, y = far("Three")
			dragTab("One", x, y)
			if want := []string{"Two", "Three", "One", "Four"}; !slices.Equal(names, want) || tab != 2 || changes != 0 {
				t.Fatalf("One past Three's middle: %v, tab %d, %d changes; want %v, tab 2, none", names, tab, changes, want)
			}
			// Four before Two, and chosen.
			x, y = near("Two")
			dragTab("Four", x, y)
			if want := []string{"Four", "Two", "Three", "One"}; !slices.Equal(names, want) || tab != 0 || changes != 1 {
				t.Fatalf("Four short of Two's middle: %v, tab %d, %d changes; want %v, tab 0, one", names, tab, changes, want)
			}
			// Three past the end, after One.
			x, y = far("One")
			dragTab("Three", x, y)
			if want := []string{"Four", "Two", "One", "Three"}; !slices.Equal(names, want) || tab != 3 || changes != 2 {
				t.Fatalf("Three past One, the last: %v, tab %d, %d changes; want %v, tab 3, two", names, tab, changes, want)
			}
			// Two back where it was, short of One's middle: it stays, and
			// is chosen.
			x, y = near("One")
			dragTab("Two", x, y)
			if want := []string{"Four", "Two", "One", "Three"}; !slices.Equal(names, want) || tab != 1 || changes != 3 {
				t.Fatalf("Two dropped where it was: %v, tab %d, %d changes; want %v, tab 1, three", names, tab, changes, want)
			}
		})
	}
}

// Only a list's own tabs drop on it: a tab of another list does not, and
// a value of the app's dropped on the list is the app's Drop's.
func TestTabsBaseReorderTakesOnlyItsTabs(t *testing.T) {
	a, b := []string{"A1", "A2"}, []string{"B1", "B2"}
	ta, tb, changes := 0, 0, 0
	var dropped []string
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Gap(40).Padding(20).Children(func() {
			tabsWith(c, &a, &ta, false, &changes)
			tabs := coreTabsBase(c, &tb, len(b))
			tabs.Reorder(func(from, to int) { b = moveTab(b, from, to) })
			tabs.List.Gap(8).Children(func() {
				for i, name := range b {
					tabs.Tab(i).Padding(4, 8).Children(func() { coreText(c, name) })
				}
			})
			if s, ok := coreDrop[string](tabs.List); ok {
				dropped = append(dropped, s)
			}
			coreBox(c).Size(80, 30).Label("Note").Drag("note")
		})
	}, 600, 400)
	a1, _ := tt.Find("A1")
	b2, _ := tt.Find("B2")
	drag(tt, a1.X+a1.W/2, a1.Y+a1.H/2, b2.X+b2.W*3/4, b2.Y+b2.H/2)
	tt.Frame()
	if !slices.Equal(a, []string{"A1", "A2"}) || !slices.Equal(b, []string{"B1", "B2"}) || ta != 0 || tb != 0 {
		t.Fatalf("A1 dropped on the other list: %v %v, tabs %d %d", a, b, ta, tb)
	}
	note, _ := tt.Find("Note")
	drag(tt, note.X+10, note.Y+10, b2.X+b2.W/2, b2.Y+b2.H/2)
	tt.Frame()
	if !slices.Equal(dropped, []string{"note"}) || !slices.Equal(b, []string{"B1", "B2"}) {
		t.Fatalf("a note dropped on the list: the app's Drop took %v, tabs %v", dropped, b)
	}
}

// A tab's close button closes it, without choosing it, and Delete closes
// the tab with the focus, whose focus goes to the tab chosen next: the
// one after it, or the one before it for the last. The choice follows the
// tab chosen as tabs before it close.
func TestTabsBaseClose(t *testing.T) {
	names := []string{"One", "Two", "Three", "Four", "Five"}
	tab, changes := 1, 0
	tt := coreNewTester(func(c *context) { tabsWith(c, &names, &tab, false, &changes) }, 600, 200)
	tt.Click("Close Four")
	if want := []string{"One", "Two", "Three", "Five"}; !slices.Equal(names, want) || tab != 1 || changes != 0 {
		t.Fatalf("closing Four, after the chosen Two: %v, tab %d, %d changes; want %v, tab 1, none", names, tab, changes, want)
	}
	tt.Click("Close One")
	if want := []string{"Two", "Three", "Five"}; !slices.Equal(names, want) || tab != 0 || changes != 0 {
		t.Fatalf("closing One, before the chosen Two: %v, tab %d, %d changes; want %v, tab 0, none", names, tab, changes, want)
	}
	tt.Click("Two")
	tt.Key(0, KeyDelete)
	tt.Frame()
	if want := []string{"Three", "Five"}; !slices.Equal(names, want) || tab != 0 || changes != 1 || !tt.Focused("Three") {
		t.Fatalf("Delete on the chosen Two: %v, tab %d, %d changes, Three focused %v; want %v, tab 0, one", names, tab, changes, tt.Focused("Three"), want)
	}
	tt.Key(0, KeyEnd)
	tt.Key(0, KeyBackspace)
	tt.Frame()
	if want := []string{"Three"}; !slices.Equal(names, want) || tab != 0 || changes != 3 || !tt.Focused("Three") {
		t.Fatalf("Backspace on the last tab, Five: %v, tab %d, %d changes, Three focused %v; want %v, tab 0, three", names, tab, changes, tt.Focused("Three"), want)
	}
	// A click on the close button of the tab with the focus gives the
	// focus to the tab chosen next; on another's, it leaves it there.
	names, tab, changes = []string{"A", "B", "C", "D"}, 0, 0
	tt = coreNewTester(func(c *context) { tabsWith(c, &names, &tab, false, &changes) }, 600, 200)
	tt.Click("B")
	tt.Click("Close D")
	if want := []string{"A", "B", "C"}; !slices.Equal(names, want) || tab != 1 || !tt.Focused("B") {
		t.Fatalf("closing D while B has the focus: %v, tab %d, B focused %v; want %v, tab 1", names, tab, tt.Focused("B"), want)
	}
	tt.Click("Close B")
	tt.Frame()
	if want := []string{"A", "C"}; !slices.Equal(names, want) || tab != 1 || !tt.Focused("C") {
		t.Fatalf("closing B, which has the focus: %v, tab %d, C focused %v; want %v, tab 1", names, tab, tt.Focused("C"), want)
	}
	// Without OnClose, the keys close nothing.
	open := []string{"A", "B"}
	other := 0
	tt = coreNewTester(func(c *context) {
		tabs := coreTabsBase(c, &other, len(open))
		tabs.List.Children(func() {
			for i, name := range open {
				tabs.Tab(i).Padding(4).Children(func() { coreText(c, name) })
			}
		})
	}, 300, 100)
	tt.Click("A")
	tt.Key(0, KeyDelete)
	if len(open) != 2 {
		t.Errorf("Delete without OnClose closed a tab: %v", open)
	}
}

// Assistive technology reads a tab by its name alone: its close button,
// hidden from it, is neither in the name nor a node of its own, and no
// stop of Tab.
func TestTabsBaseCloseButtonHidden(t *testing.T) {
	names := []string{"One", "Two"}
	tab, changes := 0, 0
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Children(func() {
			coreButton(c, "Before")
			tabsWith(c, &names, &tab, false, &changes)
			coreButton(c, "After")
		})
	}, 400, 200)
	tt.UseScreenReader()
	if _, ok := tt.AccessByRole(RoleTab, "One"); !ok {
		t.Errorf("no tab named One: %v", tt.AccessAll())
	}
	for _, n := range tt.AccessAll() {
		if strings.Contains(n.Name, "Close") || strings.Contains(n.Name, "×") {
			t.Errorf("assistive technology sees %q", n.Name)
		}
	}
	tt.Click("Before")
	tt.Key(0, KeyTab)
	tt.Key(0, KeyTab)
	if !tt.Focused("After") {
		t.Errorf("Tab from the tabs went elsewhere than After, past the close buttons")
	}
}

// A tab let go past the window's edge tells it with DragMissed on its Tab,
// as to open it in a window of its own, and stays where it was.
func TestTabsBaseDragMissed(t *testing.T) {
	names := []string{"One", "Two"}
	tab := 0
	var missed []string
	tt := coreNewTester(func(c *context) {
		tabs := coreTabsBase(c, &tab, len(names))
		tabs.Reorder(func(from, to int) { names = moveTab(names, from, to) })
		tabs.List.Gap(8).Children(func() {
			for i, name := range names {
				if _, ok := tabs.Tab(i).Padding(4, 8).Children(func() { coreText(c, name) }).DragMissed(); ok {
					missed = append(missed, name)
				}
			}
		})
	}, 400, 200)
	two, _ := tt.Find("Two")
	drag(tt, two.X+4, two.Y+4, 450, 300)
	tt.Frame()
	if !slices.Equal(missed, []string{"Two"}) || !slices.Equal(names, []string{"One", "Two"}) {
		t.Fatalf("Two let go past the edge: missed %v, tabs %v", missed, names)
	}
}

// Backspace in a text being edited in a tab, as its name renamed in
// place, deletes a character, and closes no tab: the text takes it.
func TestTabsBaseCloseLeavesKeysToTextInTab(t *testing.T) {
	names := []string{"Notes", "Plan"}
	tab := 0
	tt := coreNewTester(func(c *context) {
		tabs := coreTabsBase(c, &tab, len(names))
		tabs.OnClose(func(i int) { names = slices.Delete(names, i, i+1) })
		tabs.List.Gap(8).Children(func() {
			for i := range names {
				c.nextKey, c.keySet = i, true
				tabs.Tab(i).Padding(4, 8).Children(func() { coreEditableText(c, &names[i]) })
			}
		})
	}, 400, 200)
	r, _ := tt.Find("Notes")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(time.Second)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if active, _ := tt.TextInputActive(); !active {
		t.Fatal("a double click on the name does not edit it")
	}
	tt.Key(Cmd, KeyA)
	tt.Type("Ideas")
	tt.Key(0, KeyBackspace)
	tt.Key(0, KeyEnter)
	if !slices.Equal(names, []string{"Idea", "Plan"}) {
		t.Fatalf("Backspace while renaming in a tab: %v, want [Idea Plan]", names)
	}
	tt.Key(0, KeyBackspace)
	tt.Frame()
	if !slices.Equal(names, []string{"Plan"}) {
		t.Errorf("Backspace on the tab once the text is no longer edited: %v, want [Plan]", names)
	}
}
