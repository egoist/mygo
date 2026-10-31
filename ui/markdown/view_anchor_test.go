package markdown

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestViewAnchorInitialAndRepeated(t *testing.T) {
	var scroll ui.ScrollState
	o := Options{Anchor: "#target"}
	source := "# Intro\n\n" + strings.Repeat("A paragraph before the destination.\n\n", 30) + "# Target\n\nDestination.\n"
	tt := ui.NewTester(func(c *ui.Context) {
		c.DisableAnimations()
		ui.Scroll(c).Fill().TrackScroll(&scroll).Children(func() { View(c, source, &o) })
	}, 400, 160)
	if scroll.Y <= 0 {
		t.Fatal("initial anchor did not reveal the heading")
	}
	scroll.Y = 0
	tt.Frame()
	if scroll.Y != 0 {
		t.Fatalf("unchanged anchor kept forcing scroll: %g", scroll.Y)
	}
	o.AnchorVersion++
	tt.Frame()
	if scroll.Y <= 0 {
		t.Fatal("a new version did not repeat the same anchor jump")
	}
	scroll.Y = 0
	source = "# Another document\n\n" + strings.Repeat("Different text before the target.\n\n", 20) + "# Target\n\nNew destination.\n"
	tt.Frame()
	if scroll.Y <= 0 {
		t.Fatal("a replacement document did not apply the same anchor")
	}
}

func TestViewLocalAnchorPrecedesOnLink(t *testing.T) {
	var scroll ui.ScrollState
	var clicked []string
	o := Options{OnLink: func(dest string) { clicked = append(clicked, dest) }}
	source := "[Jump](#target) [File](next.md)\n\n" + strings.Repeat("A paragraph before the destination.\n\n", 30) + "# Target\n\nDestination.\n"
	tt := ui.NewTester(func(c *ui.Context) {
		c.DisableAnimations()
		ui.Scroll(c).Fill().TrackScroll(&scroll).Children(func() { View(c, source, &o) })
	}, 400, 160)
	if err := tt.Click("File"); err != nil {
		t.Fatal(err)
	}
	if len(clicked) != 1 || clicked[0] != "next.md" {
		t.Fatalf("file did not reach OnLink: %q", clicked)
	}
	if err := tt.Click("Jump"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(clicked) != 1 {
		t.Fatalf("local heading was intercepted by OnLink: %q", clicked)
	}
	if scroll.Y <= 0 {
		t.Fatal("local anchor did not reveal its heading")
	}
	if len(tt.OpenedURLs()) != 0 {
		t.Fatalf("links escaped to the system opener: %q", tt.OpenedURLs())
	}
}
