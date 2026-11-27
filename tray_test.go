//go:build !js

package mygo

import (
	"errors"
	"testing"
)

// NewTray tells the icon what it was made with: its image, title, tool
// tip and menu, and nothing it was made without.
func TestTrayOptions(t *testing.T) {
	before := len(fb.TrayStates())
	menu := NewMenu([]*MenuItem{{ID: "about", Label: "About"}})
	rate, err := NewTray(TrayOptions{
		ID: "rate", Icon: []byte("png"), IconIsTemplate: true,
		Title: "60K", MonospacedTitle: true, ToolTip: "the rate", Menu: menu,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rate.Destroy()
	quiet, err := NewTray(TrayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer quiet.Destroy()

	states := fb.TrayStates()
	if len(states) != before+2 {
		t.Fatalf("%d icons were told of, want %d", len(states), before+2)
	}
	st := states[before]
	if st.ID != "rate" || string(st.Image) != "png" || !st.ImageIsTemplate ||
		st.Title != "60K" || st.ToolTip != "the rate" || st.Menu == nil ||
		len(st.Menu.Items) != 1 || st.Menu.Items[0].Label != "About" {
		t.Fatalf("the icon was told %+v", st)
	}
	if got := fb.TrayStates()[before+1]; got.ID != "" || got.Title != "" || got.Menu != nil {
		t.Fatalf("an icon of no options was told %+v", got)
	}

	// An image the system refuses fails the icon, which is removed, and
	// nothing is told of after it.
	fb.TrayImageError = errors.New("no room")
	t.Cleanup(func() { fb.TrayImageError = nil })
	refused, err := NewTray(TrayOptions{Icon: []byte("png"), Title: "60K"})
	if err == nil || refused != nil {
		t.Fatalf("an icon the system refused: %v, %v", refused, err)
	}
	if got := fb.TrayStates()[before+2]; got.Title != "" || string(got.Image) != "" || !got.Destroyed {
		t.Fatalf("after the refused image the icon was told %+v", got)
	}
}

// The setters tell the icon of the change, SetMenu(nil) takes the menu
// away so clicks reach OnClick, and PopUpMenu shows it.
func TestTraySetters(t *testing.T) {
	rate, err := NewTray(TrayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer rate.Destroy()
	i := len(fb.TrayStates()) - 1

	if err := rate.SetIcon([]byte("new"), true); err != nil {
		t.Fatal(err)
	}
	rate.SetTitle("1.3M")
	rate.SetToolTip("faster")
	rate.SetMenu(NewMenu([]*MenuItem{{ID: "about", Label: "About"}}))
	st := fb.TrayStates()[i]
	if string(st.Image) != "new" || !st.ImageIsTemplate || st.Title != "1.3M" ||
		st.ToolTip != "faster" || st.Menu == nil || len(st.Menu.Items) != 1 {
		t.Fatalf("the icon was told %+v", st)
	}

	rate.SetMenu(nil)
	if st := fb.TrayStates()[i]; st.Menu != nil {
		t.Fatalf("the menu was taken away, the icon has %v", st.Menu)
	}
	rate.PopUpMenu()
	if got := fb.TrayStates()[i].PopUps; got != 1 {
		t.Fatalf("the menu was shown %d times", got)
	}
}

// Bounds is where the icon sits, clicks reach the listeners while they
// are on, and Destroy takes the icon away for good.
func TestTrayClicksAndBounds(t *testing.T) {
	rate, err := NewTray(TrayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	i := len(fb.TrayStates()) - 1
	if got := rate.Bounds(); got != (Rectangle{X: 1300, Width: 30, Height: 25}) {
		t.Fatalf("the icon is at %+v", got)
	}

	// A click has somewhere to go while a listener is on, and nowhere
	// once it is off.
	clicks := 0
	off := rate.OnClick(func() { clicks++ })
	onMain(func() {}) // the icon was told it answers activation
	if !fb.TrayStates()[i].Activatable {
		t.Error("an icon with a click listener answers no activation")
	}
	onMain(func() { fb.ClickTray(i) })
	if clicks != 1 {
		t.Fatalf("the icon was clicked %d times", clicks)
	}
	off()
	onMain(func() {})
	if fb.TrayStates()[i].Activatable {
		t.Error("an icon whose click listener is off still answers activation")
	}
	onMain(func() { fb.ClickTray(i) })
	if clicks != 1 {
		t.Fatalf("after off the icon was clicked %d times", clicks)
	}

	rights := 0
	rate.OnRightClick(func() { rights++ })
	onMain(func() { fb.RightClickTray(i) })
	if rights != 1 {
		t.Fatalf("the icon was right-clicked %d times", rights)
	}

	rate.Destroy()
	if st := fb.TrayStates()[i]; !st.Destroyed {
		t.Error("the destroyed icon is still there")
	}
	if got := rate.Bounds(); got != (Rectangle{}) {
		t.Fatalf("the destroyed icon is at %+v", got)
	}
	// Nothing is told of the icon after it is gone.
	rate.SetTitle("1.3M")
	if got := fb.TrayStates()[i].Title; got != "" {
		t.Fatalf("the destroyed icon was told of %q", got)
	}
}
