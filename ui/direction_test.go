package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/text"
)

func TestDirectionForLocale(t *testing.T) {
	for tag, want := range map[string]LayoutDirection{
		"": LTR, "en-US": LTR, "ar-EG": RTL, "HE_il.UTF-8": RTL, "iw": RTL,
		"fa": RTL, "ur-PK": RTL, "yi": RTL, "dv": RTL, "ckb": RTL,
		"ar-Latn": LTR, "he-Latn": LTR, "az-Arab": RTL, "az-Latn": LTR,
		"ff-Adlm": RTL, "pa-Arab-PK": RTL, "pa-Guru": LTR, "ku": LTR,
		"en-u-nu-arab": LTR, "ar-x-latn": RTL, "unknown": LTR,
	} {
		if got := DirectionForLocale(tag); got != want {
			t.Errorf("%s: %v, want %v", tag, got, want)
		}
	}
}

func TestDirectionInheritance(t *testing.T) {
	var directions map[string]LayoutDirection
	override := AutoDirection
	tt := NewTester(func(c *Context) {
		c.SetDirection(override)
		directions = map[string]LayoutDirection{"root": c.Direction()}
		Row(c).FillWidth().Children(func() {
			directions["inherited"] = Box(c).LayoutDirection()
			Column(c).Direction(LTR).Children(func() {
				directions["explicit"] = Box(c).LayoutDirection()
				Column(c).LayoutLocale("he-IL").Direction(AutoDirection).Children(func() {
					directions["nested auto"] = Box(c).LayoutDirection()
					directions["nested RTL"] = Box(c).Direction(RTL).LayoutDirection()
				})
			})
		})
	}, 200, 100)
	tt.SetLayoutLocale("ar-EG")
	for name, want := range map[string]LayoutDirection{"root": RTL, "inherited": RTL, "explicit": LTR, "nested auto": RTL, "nested RTL": RTL} {
		if directions[name] != want {
			t.Errorf("%s: %v, want %v", name, directions[name], want)
		}
	}
	override = LTR
	tt.Frame()
	if directions["root"] != LTR || directions["inherited"] != LTR || directions["nested auto"] != RTL {
		t.Fatalf("explicit override: %v", directions)
	}
	override = AutoDirection
	tt.SetLayoutLocale("en-US")
	if directions["root"] != LTR {
		t.Fatal("automatic direction did not follow host locale")
	}
}

func TestRTLFlexAndGrid(t *testing.T) {
	tests := map[string]func(*Context){
		"wrapped row": func(c *Context) {
			Row(c).Wrap().FillWidth().Height(80).Gap(7).AlignItems(End).AlignContent(SpaceBetween).Children(func() {
				for i := range 5 {
					Box(c).Size(70, float32(10+i*3)).MarginStart(float32(i)).Shrink(0).Label(fmt.Sprint(i))
				}
			})
		},
		"reverse row and wrap": func(c *Context) {
			Row(c).Reverse().WrapReverse().FillWidth().Height(100).Gap(6).AlignItems(Start).Children(func() {
				for i := range 5 {
					Box(c).Size(70, 15).Shrink(0).Label(fmt.Sprint(i))
				}
			})
		},
		"wrapped column": func(c *Context) {
			Column(c).Wrap().FillWidth().Height(80).Gap(7).AlignItems(Start).AlignContent(SpaceAround).Children(func() {
				for i := range 5 {
					Box(c).Size(float32(30+i*3), 30).Shrink(0).Label(fmt.Sprint(i))
				}
			})
		},
		"reverse column and wrap": func(c *Context) {
			Column(c).Reverse().WrapReverse().FillWidth().Height(80).Gap(7).AlignItems(End).Children(func() {
				for i := range 5 {
					Box(c).Size(40, 30).Shrink(0).Label(fmt.Sprint(i))
				}
			})
		},
		"column alignment and auto margins": func(c *Context) {
			Column(c).FillWidth().Gap(4).AlignItems(Start).Children(func() {
				Box(c).Size(40, 10).Label("0")
				Box(c).Size(40, 10).AlignSelf(End).Label("1")
				Box(c).Size(40, 10).MarginStart(Auto).Label("2")
				Box(c).Size(40, 10).MarginEnd(Auto).Label("3")
				Box(c).Size(40, 10).MarginX(Auto).Label("4")
			})
		},
		"grid tracks and spans": func(c *Context) {
			Grid(c).FillWidth().ColumnTracks(Fixed(50), Fr(1), Fr(2)).Gap(6).JustifyItems(Start).Children(func() {
				Box(c).Size(25, 10).Label("0").MarginStart(3)
				Box(c).Height(14).Label("1").ColumnSpan(2).JustifySelf(Stretch)
				Box(c).Size(30, 20).Label("2").ColumnStart(3).RowStart(2).MarginEnd(4).JustifySelf(End)
				Box(c).Size(25, 10).Label("3").MarginStart(Auto)
				Box(c).Height(10).Label("4").ColumnSpan(-1)
			})
		},
		"grid fit tracks and distribution": func(c *Context) {
			Grid(c).FillWidth().ColumnTracks(Fixed(45), Fixed(45)).Justify(SpaceBetween).GapY(5).Children(func() {
				for i := range 5 {
					Box(c).Size(20, 10).JustifySelf(End).Label(fmt.Sprint(i))
				}
			})
		},
	}
	for name, view := range tests {
		t.Run(name, func(t *testing.T) {
			build := func(dir LayoutDirection) map[string]Rect {
				return boxes(t, func(c *Context) { c.SetDirection(dir); view(c) }, 240, 180, "0", "1", "2", "3", "4")
			}
			l, r := build(LTR), build(RTL)
			for name, left := range l {
				want := Rect{240 - left.X - left.W, left.Y, left.W, left.H}
				if !nearRect(r[name], want) {
					t.Errorf("%s: LTR %v, RTL %v, want %v", name, left, r[name], want)
				}
			}
		})
	}
}

func TestRTLLogicalEdges(t *testing.T) {
	for _, dir := range []LayoutDirection{LTR, RTL} {
		t.Run(fmt.Sprint(dir), func(t *testing.T) {
			var parent, content, relative *Element
			tt := NewTester(func(c *Context) {
				c.SetDirection(dir)
				parent = Box(c).FillWidth().Height(140).Padding(2).PaddingStart(11).PaddingEnd(7).Border(1, RGB(0, 0, 0)).BorderStart(3).BorderEnd(5)
				parent.Children(func() {
					content = Box(c).Size(20, 10).MarginStart(9).MarginEnd(4).Label("content")
					Box(c).Absolute().Size(30, 10).InsetStart(15).Top(30).Label("absolute")
					Box(c).Absolute().Size(30, 10).InsetEndPercent(10).Top(50).Label("percent")
					Box(c).Absolute().Height(10).InsetStart(8).InsetEnd(12).Top(70).Label("stretched")
					relative = Box(c).Size(20, 10).InsetStart(6).Label("relative")
				})
			}, 200, 160)
			if parent.padX() != 26 || content.marginX() != 13 {
				t.Fatalf("edge sums: padding/border %v, margins %v", parent.padX(), content.marginX())
			}
			abs, _ := tt.Find("absolute")
			percent, _ := tt.Find("percent")
			stretch, _ := tt.Find("stretched")
			if dir == LTR {
				if abs.X != 18 || !near(percent.X, 145.8) || stretch.X != 11 || stretch.W != 172 || content.x != 23 || relative.x != 20 {
					t.Fatalf("LTR: absolute %v, percent %v, stretch %v, content %v, relative %v", abs, percent, stretch, content.x, relative.x)
				}
			} else {
				if abs.X != 152 || !near(percent.X, 24.2) || stretch.X != 17 || stretch.W != 172 || content.x != 157 || relative.x != 160 {
					t.Fatalf("RTL: absolute %v, percent %v, stretch %v, content %v, relative %v", abs, percent, stretch, content.x, relative.x)
				}
			}
		})
	}
	// Physical edges remain physical inside an RTL layout.
	b := boxes(t, func(c *Context) {
		c.SetDirection(RTL)
		Box(c).Fill().Children(func() { Box(c).Absolute().Size(20, 20).Left(12).Top(8).Label("physical") })
	}, 200, 100, "physical")
	if b["physical"].X != 12 {
		t.Fatalf("physical Left moved: %v", b)
	}
}

func TestRTLTextAndImagesStayUpright(t *testing.T) {
	for _, label := range []string{"مرحبا بالعالم", "שלום עולם", "שלום Go 123 مرحبا"} {
		t.Run(label, func(t *testing.T) {
			var a, b *text.Layout
			for _, dir := range []LayoutDirection{LTR, RTL} {
				var element *Element
				NewTester(func(c *Context) { c.SetDirection(dir); element = Text(c, label).FillWidth() }, 240, 80)
				if dir == LTR {
					a = element.tl
				} else {
					b = element.tl
				}
			}
			if len(a.Lines) != len(b.Lines) {
				t.Fatal("interface direction changed text line breaking")
			}
			for i, line := range a.Lines {
				other := b.Lines[i]
				if line.RTL != other.RTL || len(line.Glyphs) != len(other.Glyphs) {
					t.Fatal("interface direction changed shaping")
				}
				for j, g := range line.Glyphs {
					if g != other.Glyphs[j] {
						t.Fatalf("glyph %d changed orientation or position", j)
					}
				}
			}
		})
	}
	// Logical text alignment moves runs; it does not reflect glyphs or carets.
	var e *Element
	NewTester(func(c *Context) { c.SetDirection(RTL); e = Text(c, "ABC 123").FillWidth().TextAlignInline(Start) }, 240, 80)
	line := e.tl.Lines[0]
	x0, _, _ := e.tl.Caret(0)
	x3, _, _ := e.tl.Caret(3)
	if !near(line.X, 240-line.Width) || x0 >= x3 {
		t.Fatalf("logical alignment damaged Latin carets: %+v", line)
	}
	// An asymmetric drawing and image retain their left and right pixels.
	source := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := range 20 {
		for x := range 40 {
			pixel := color.RGBA{255, 0, 0, 255}
			if x >= 20 {
				pixel = color.RGBA{0, 0, 255, 255}
			}
			source.SetRGBA(x, y, pixel)
		}
	}
	var picture, drawing *Element
	tt := NewTester(func(c *Context) {
		c.SetDirection(RTL)
		Row(c).FillWidth().Children(func() {
			picture = Image(c, NewBitmap(source)).Size(40, 20)
			drawing = Box(c).Size(40, 20).Draw(func(p *Painter, r Rect) { p.Fill(Rect{r.X, r.Y, 10, r.H}, RGB(0, 255, 0), 0) })
		})
	}, 140, 50)
	pix := tt.Image()
	if pix.RGBAAt(int(picture.x+5), int(picture.y+5)) != (color.RGBA{255, 0, 0, 255}) || pix.RGBAAt(int(picture.x+35), int(picture.y+5)) != (color.RGBA{0, 0, 255, 255}) || pix.RGBAAt(int(drawing.x+5), int(drawing.y+5)) != (color.RGBA{0, 255, 0, 255}) {
		t.Fatal("RTL reflected image or custom drawing pixels")
	}
}

func TestRTLKeyboardNavigation(t *testing.T) {
	selected := 0
	tt := NewTester(func(c *Context) {
		c.SetLayoutLocale("ar")
		Tabs(c, &selected, "العربية", "עברית", "English")
		ButtonBase(c).Size(70, 20).Label("after")
	}, 400, 130)
	tt.Key(0, KeyTab)
	if !tt.Focused("العربية") {
		t.Fatal("Tab did not enter the source-first tab")
	}
	tt.Key(0, KeyLeft)
	if selected != 1 || !tt.Focused("עברית") {
		t.Fatalf("Left chose %d", selected)
	}
	tt.Key(0, KeyRight)
	if selected != 0 {
		t.Fatalf("Right chose %d", selected)
	}
	tt.Key(0, KeyEnd)
	if selected != 2 || !tt.Focused("English") {
		t.Fatal("End did not choose source-last tab")
	}
	tt.Key(0, KeyTab)
	if !tt.Focused("after") {
		t.Fatal("Tab did not leave the RTL group")
	}
	tt.Key(Shift, KeyTab)
	if !tt.Focused("English") {
		t.Fatal("Shift+Tab lost the remembered tab")
	}
	// Reverse is relative to RTL, and an explicit LTR island keeps LTR keys.
	for _, dir := range []LayoutDirection{LTR, RTL} {
		selected = 0
		rt := NewTester(func(c *Context) {
			c.SetDirection(RTL)
			Column(c).Direction(dir).Children(func() {
				p := TabsBase(c, &selected, 3)
				p.List.Reverse().Children(func() {
					for i := range 3 {
						p.Tab(i).Size(50, 20).Label(fmt.Sprint(i))
					}
				})
			})
		}, 240, 100)
		rt.Key(0, KeyTab)
		key := KeyLeft
		if dir == RTL {
			key = KeyRight
		}
		rt.Key(0, key)
		if selected != 1 {
			t.Errorf("reversed %v tabs chose %d", dir, selected)
		}
	}
	toolbar := NewTester(func(c *Context) {
		c.SetDirection(RTL)
		Toolbar(c, func() { Button(c, "One"); Button(c, "Two"); Button(c, "Three") })
		Button(c, "Next group")
	}, 400, 150)
	toolbar.Key(0, KeyTab)
	toolbar.Key(0, KeyLeft)
	if !toolbar.Focused("Two") {
		t.Fatal("toolbar Left did not move visually left")
	}
	toolbar.Key(0, KeyTab)
	if !toolbar.Focused("Next group") {
		t.Fatal("toolbar did not remain one Tab stop")
	}
}

func TestRTLSliderInputAndAccessibility(t *testing.T) {
	value := 25.0
	tt := NewTester(func(c *Context) {
		c.SetDirection(RTL)
		SliderBase(c, &value, 0, 100).Step(5).Size(120, 20).PaddingX(10).Label("volume")
	}, 200, 100)
	r, _ := tt.Find("volume")
	for _, tc := range []struct {
		x    float32
		want float64
	}{{r.X + 10, 100}, {r.X + 60, 50}, {r.X + 110, 0}} {
		tt.Press(tc.x, r.Y+10)
		tt.Release(tc.x, r.Y+10)
		if value != tc.want {
			t.Errorf("press at %v: %v, want %v", tc.x, value, tc.want)
		}
	}
	tt.Key(0, KeyLeft)
	if value != 5 {
		t.Fatalf("Left: %v", value)
	}
	tt.Key(0, KeyRight)
	if value != 0 {
		t.Fatalf("Right: %v", value)
	}
	tt.Key(0, KeyUp)
	if value != 5 {
		t.Fatalf("Up: %v", value)
	}
	tt.Key(0, KeyHome)
	if value != 0 {
		t.Fatal("Home")
	}
	tt.Key(0, KeyEnd)
	if value != 100 {
		t.Fatal("End")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	n := node(t, tt.h.access, platform.RoleSlider, "volume")
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: n.ID, Action: platform.AccessDecrement})
	if value != 95 {
		t.Fatalf("accessible decrement: %v", value)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: n.ID, Action: platform.AccessIncrement})
	if value != 100 {
		t.Fatalf("accessible increment: %v", value)
	}
	low, high := 20.0, 70.0
	rangeTest := NewTester(func(c *Context) {
		c.SetDirection(RTL)
		RangeSlider(c, &low, &high, 0, 100, 5).Width(200).Label("price")
	}, 260, 90)
	rangeTest.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	min := node(t, rangeTest.h.access, platform.RoleSlider, "price minimum")
	max := node(t, rangeTest.h.access, platform.RoleSlider, "price maximum")
	if min.Bounds.X <= max.Bounds.X {
		t.Fatalf("range knobs not mirrored: %v %v", min.Bounds, max.Bounds)
	}
	rangeTest.ClickAt(float32(min.Bounds.X+min.Bounds.W/2), float32(min.Bounds.Y+min.Bounds.H/2))
	rangeTest.Key(0, KeyLeft)
	if math.Abs(low-25) > 0.001 {
		t.Fatalf("minimum Left: %v", low)
	}
}

func TestRTLPopupAnchors(t *testing.T) {
	open := true
	var anchor, panel, physical *Element
	tt := NewTester(func(c *Context) {
		c.SetDirection(LTR)
		Box(c).Fill().Children(func() {
			Column(c).Direction(RTL).Absolute().Left(160).Top(40).Size(60, 20).Children(func() { anchor = ButtonBase(c).Size(60, 20).Label("anchor") })
			panel = PopoverBase(c, anchor, &open, func(panel *Element) {
				panel.Size(100, 30).Label("popup")
				if c.Direction() != RTL {
					t.Error("popup lost anchor direction")
				}
			})
			Overlay(c, func() {
				physical = Box(c).Size(100, 20).AttachTo(anchor, AnchorBottomLeft, AnchorTopLeft).Label("physical")
			})
		})
	}, 320, 200)
	if panel.x != 120 || panel.y != 60 || physical.x != 160 {
		t.Fatalf("popup %v, physical %v", Rect{panel.x, panel.y, panel.w, panel.h}, Rect{physical.x, physical.y, physical.w, physical.h})
	}
	// Nested direction overrides inside the popup survive anchor inheritance.
	if panel.LayoutDirection() != RTL {
		t.Fatal("overlay did not inherit direction")
	}
	tt.SetSize(200, 70)
	if panel.y != 10 || panel.x < windowMargin || panel.x+panel.w > 196 {
		t.Fatalf("popup did not flip and clamp: %v", Rect{panel.x, panel.y, panel.w, panel.h})
	}
}

func TestRTLScrollGeometry(t *testing.T) {
	var scroll ScrollState
	var container *Element
	reveal := false
	open := true
	var popup *Element
	tt := NewTester(func(c *Context) {
		c.SetDirection(RTL)
		container = ScrollHorizontal(c).FillWidth().Height(80).TrackScroll(&scroll)
		container.Children(func() {
			for i := range 4 {
				b := ButtonBase(c).Width(100).Height(20).Label(fmt.Sprint(i))
				if i == 0 {
					popup = PopoverBase(c, b, &open, func(panel *Element) { panel.Size(30, 20).Label("popup") })
				}
				if i == 3 && reveal {
					b.ScrollIntoView()
				}
			}
		})
	}, 200, 120)
	r0, _ := tt.Find("0")
	if scroll.X != 0 || scroll.MaxX != 200 || r0.X != 100 || popup.x != 100 {
		t.Fatalf("initial RTL scroll: %+v first %v popup %v", scroll, r0, popup.x)
	}
	// Physical Left scrolls left, increasing distance from the right edge.
	tt.Key(0, KeyTab)
	tt.Key(0, KeyLeft)
	if scroll.X != 40 {
		t.Fatalf("Left offset %v", scroll.X)
	}
	r0, _ = tt.Find("0")
	if r0.X != 140 || popup.x != 140 {
		t.Fatalf("scrolling did not move anchor/popup together: %v %v", r0, popup.x)
	}
	tt.Scroll(60, 30, 25, 0)
	if scroll.X != 15 {
		t.Fatalf("physical positive wheel offset %v", scroll.X)
	}
	reveal = true
	tt.Frame()
	reveal = false
	if scroll.X != 200 {
		t.Fatalf("revealed last item: %+v", scroll)
	}
	r3, _ := tt.Find("3")
	if r3.X != 0 {
		t.Fatalf("revealed box %v", r3)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	n := node(t, tt.h.access, platform.RoleButton, "3")
	if n.Bounds.X != float64(r3.X) || n.States&platform.AccessOffscreen != 0 {
		t.Fatalf("accessibility disagrees with visible box: %+v", n)
	}
	// Logical offsets survive direction changes and clamp when content shrinks.
	scroll.X = 0
	tt.Frame()
	tt.Key(0, KeyEnd)
	if scroll.X != 200 {
		t.Fatal("horizontal End")
	}
	tt.Key(0, KeyHome)
	if scroll.X != 0 {
		t.Fatal("horizontal Home")
	}
	tt.Move(100, 60)
	st := container.st
	g := scrollBars(Rect{st.x, st.y, st.w, st.h}, st.barInset, float32(st.contentW), float32(st.contentH), float32(st.physicalScrollX()), 0, st.flags, 6, true)
	tt.Press(g.h.X+g.h.W/2, g.h.Y+2)
	tt.Move(g.hTrack.X+2, g.h.Y+2)
	tt.Release(g.hTrack.X+2, g.h.Y+2)
	if scroll.X < scroll.MaxX-1 {
		t.Fatalf("dragging thumb left did not reach end: %+v", scroll)
	}
}

func TestRTLControlsAndCalendar(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		img := Render(func(c *Context) {
			c.SetDirection(RTL)
			bar := Progress(c, 0.25).Height(10)
			if reverse {
				bar.Reverse()
			}
		}, 200, 30, 1)
		accent := LightTheme().Accent
		filled := func(x int) bool { p := img.RGBAAt(x, 5); return p.R == accent.R && p.G == accent.G && p.B == accent.B }
		if filled(190) == reverse || filled(10) != reverse {
			t.Errorf("RTL progress reverse %v", reverse)
		}
	}
	date := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	tt := NewTester(func(c *Context) { c.SetDirection(RTL); Calendar(c, &date) }, 400, 300)
	tt.Key(0, KeyTab)
	tt.Key(0, KeyLeft)
	if date.Day() != 8 {
		t.Fatalf("calendar Left: %v", date)
	}
	tt.Key(0, KeyRight)
	if date.Day() != 7 {
		t.Fatal("calendar Right")
	}
	tt.Key(0, KeyDown)
	if date.Day() != 14 {
		t.Fatal("calendar Down")
	}
}

func TestRTLCollectionsAndSplit(t *testing.T) {
	t.Run("virtual grid keys", func(t *testing.T) {
		selected := 0
		s := GridState{Selected: &selected}
		tt := NewTester(func(c *Context) {
			c.SetDirection(RTL)
			GridView(c, &s, 100, 80, 35, func(i int) { Textf(c, "Item %d", i) }).Grow(1)
		}, 280, 180)
		tt.Click("Item 0")
		tt.Key(0, KeyLeft)
		if selected != 1 {
			t.Fatalf("grid Left selected %d", selected)
		}
		tt.Key(0, KeyDown)
		if selected != 1+s.cols {
			t.Fatalf("grid Down selected %d", selected)
		}
		tt.Key(0, KeyRight)
		if selected != s.cols {
			t.Fatalf("grid Right selected %d", selected)
		}
		tt.Key(0, KeyEnd)
		if selected != 99 || !tt.HasText("Item 99") {
			t.Fatal("grid End did not reveal the final item")
		}
	})
	t.Run("outline expand and parent", func(t *testing.T) {
		selected := 0
		var s OutlineState[string]
		s.List.Selected = &selected
		tt := NewTester(func(c *Context) {
			c.SetDirection(RTL)
			Outline(c, &s, []string{"src", "README.md"}, children, func(path string) { Text(c, baseName(path)) }).Grow(1)
		}, 300, 180)
		tt.Click("src")
		tt.Key(0, KeyLeft)
		if !s.Open.Has("src") {
			t.Fatal("Left did not expand RTL outline")
		}
		tt.Key(0, KeyLeft)
		if s.Item(selected) != "src/ui" {
			t.Fatalf("Left did not enter child: %s", s.Item(selected))
		}
		tt.Key(0, KeyRight)
		if s.Item(selected) != "src" {
			t.Fatal("Right did not go to parent")
		}
		tt.Key(0, KeyRight)
		if s.Open.Has("src") {
			t.Fatal("Right did not collapse")
		}
		tt.Key(0, KeyDown)
		if s.Item(selected) != "README.md" {
			t.Fatal("Down changed vertical order")
		}
	})
	t.Run("split drag and keys", func(t *testing.T) {
		size := float32(100)
		tt := NewTester(func(c *Context) {
			c.SetDirection(RTL)
			Split(c, &size, func() { Box(c).Fill().Label("first pane") }, func() { Box(c).Fill().Label("second pane") }).Fill()
		}, 320, 150)
		a, _ := tt.Find("first pane")
		b, _ := tt.Find("second pane")
		if a.X != 220 || b.X != 0 {
			t.Fatalf("RTL split panes %v %v", a, b)
		}
		tt.Press(220, 75)
		tt.Move(200, 75)
		tt.Release(200, 75)
		if size != 120 {
			t.Fatalf("RTL split drag size %v", size)
		}
		tt.Key(0, KeyLeft)
		if size != 130 {
			t.Fatalf("RTL split Left size %v", size)
		}
		tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
		n := node(t, tt.h.access, platform.RoleSplitter, "Divider")
		tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: n.ID, Action: platform.AccessIncrement})
		if size != 140 {
			t.Fatalf("RTL splitter accessibility increment size %v", size)
		}
	})
}

func TestRTLTableHeadersFollowScroll(t *testing.T) {
	s := ListState{}
	cols := []TableColumn{{Title: "Name", Width: 200}, {Title: "Kind", Width: 200}, {Title: "Size", Width: 200}}
	var cells [3]*Element
	tt := NewTester(func(c *Context) {
		c.SetDirection(RTL)
		Table(c, &s, cols, 2, func(row, col int) {
			if row == 0 {
				cells[col] = c.parent
			}
			Textf(c, "cell %d/%d", row, col)
		}).Fill()
	}, 400, 200)
	check := func() {
		t.Helper()
		tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
		for j, col := range cols {
			h := node(t, tt.h.access, platform.RoleColumnHeader, col.Title).Bounds
			if h.X != float64(cells[j].x) || h.W != float64(cells[j].w) {
				t.Fatalf("%s header %v, cell %v", col.Title, h, Rect{cells[j].x, cells[j].y, cells[j].w, cells[j].h})
			}
		}
	}
	check()
	if cells[0].x != 200 || cells[2].x != -200 {
		t.Fatalf("initial RTL cells %v %v", cells[0].x, cells[2].x)
	}
	tt.Scroll(200, 100, -200, 0)
	check()
	if cells[2].x != 0 {
		t.Fatalf("final column not revealed: %v", cells[2].x)
	}
	// The final column's inline end (left edge) resizes toward the left.
	h := header(t, tt, "Size")
	tt.Press(float32(h.X+2), float32(h.Y+h.H/2))
	tt.Move(float32(h.X-28), float32(h.Y+h.H/2))
	tt.Release(float32(h.X-28), float32(h.Y+h.H/2))
	if s.Columns.Widths["Size"] != 230 {
		t.Fatalf("RTL resize %v", s.Columns.Widths)
	}
}

func TestRTLScrollDirectionChangesAndShortContent(t *testing.T) {
	count := 4
	direction := RTL
	var scroll ScrollState
	tt := NewTester(func(c *Context) {
		c.SetDirection(direction)
		ScrollHorizontal(c).FillWidth().Height(40).TrackScroll(&scroll).Children(func() {
			for i := range count {
				Box(c).Size(80, 20).Label(fmt.Sprint(i)).Shrink(0)
			}
		})
	}, 200, 100)
	scroll.X = 60
	tt.Frame()
	direction = LTR
	tt.Frame()
	first, _ := tt.Find("0")
	if scroll.X != 60 || first.X != 0 || first.W != 20 {
		t.Fatalf("direction change: offset %v first %v", scroll.X, first)
	}
	direction = RTL
	count = 1
	tt.Frame()
	first, _ = tt.Find("0")
	if scroll.X != 0 || scroll.MaxX != 0 || first.X != 120 {
		t.Fatalf("short RTL content: %+v first %v", scroll, first)
	}
}

func TestRTLOverlayScopeAndVerticalSlider(t *testing.T) {
	open := true
	selected := 0
	value := 20.0
	tt := NewTester(func(c *Context) {
		c.SetDirection(LTR)
		Column(c).Direction(RTL).Children(func() {
			DialogBase(c, &open, func(back, panel *Element) {
				if c.Direction() != RTL {
					t.Error("dialog builder lost its source direction")
				}
				panel.Children(func() {
					Tabs(c, &selected, "First", "Second")
					SliderBase(c, &value, 0, 100).Step(5).Vertical().Size(20, 100).Label("vertical")
				})
			})
		})
	}, 300, 220)
	tt.Click("First")
	tt.Key(0, KeyLeft)
	if selected != 1 {
		t.Fatalf("RTL modal Left chose %d", selected)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	slider := node(t, tt.h.access, platform.RoleSlider, "vertical")
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: slider.ID, Action: platform.AccessIncrement})
	if value != 25 {
		t.Fatalf("RTL vertical increment: %v", value)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: slider.ID, Action: platform.AccessDecrement})
	if value != 20 {
		t.Fatalf("RTL vertical decrement: %v", value)
	}
}

func TestRTLLogicalFrameAllocations(t *testing.T) {
	tt := NewTester(func(c *Context) {
		c.SetLayoutLocale("ar-Arab")
		for range 100 {
			Box(c).Height(1).PaddingStart(2).PaddingEnd(3).MarginStart(1).BorderEnd(1)
		}
	}, 200, 200)
	if n := testing.AllocsPerRun(20, tt.Frame); n > 2 {
		t.Fatalf("steady RTL logical layout allocates %.0f times per frame", n)
	}
}

func TestRTLLogicalGridDropEdges(t *testing.T) {
	selected := 0
	s := GridState{Selected: &selected}
	tt := NewTester(func(c *Context) {
		c.SetDirection(RTL)
		GridView(c, &s, 6, 80, 35, func(i int) { Textf(c, "Photo %d", i) }).Grow(1)
	}, 280, 180)
	var st *state
	for id, i := range s.cells {
		if i == 0 {
			st = s.cellState(id)
		}
	}
	if st == nil {
		t.Fatal("no first grid item")
	}
	if to, _, after := s.dropAt(st.x+st.w-2, st.y+2, 6); to != 0 || after {
		t.Fatal("right edge is not before the first RTL item")
	}
	if to, _, after := s.dropAt(st.x+2, st.y+2, 6); to != 1 || !after {
		t.Fatal("left edge is not after the first RTL item")
	}
	_ = tt
}

func TestRTLTextAlignmentCompatibility(t *testing.T) {
	for _, dir := range []LayoutDirection{LTR, RTL} {
		var natural, logical *Element
		NewTester(func(c *Context) {
			c.SetDirection(dir)
			natural = Text(c, "مرحبا").FillWidth().TextAlign(End)
			logical = Text(c, "مرحبا").FillWidth().TextAlignInline(End)
		}, 200, 100)
		if natural.tl.Lines[0].X != 0 {
			t.Fatal("TextAlign changed existing RTL paragraph end alignment")
		}
		x := logical.tl.Lines[0].X
		if (dir == RTL && x != 0) || (dir == LTR && !near(x, 200-logical.tl.Width)) {
			t.Fatalf("logical end %v: x %v", dir, x)
		}
	}
}

func TestRTLWidgetDirectionOverride(t *testing.T) {
	value := 50.0
	selected := 0
	tt := NewTester(func(c *Context) {
		c.SetDirection(LTR)
		Tabs(c, &selected, "One", "Two").Direction(RTL)
		SliderBase(c, &value, 0, 100).Size(100, 20).Label("slider").Direction(RTL).Reverse()
	}, 240, 120)
	tt.Click("One")
	tt.Key(0, KeyLeft)
	if selected != 1 {
		t.Fatalf("tab direction override chose %d", selected)
	}
	tt.Click("slider")
	value = 50
	tt.Frame()
	tt.Key(0, KeyLeft)
	if value != 51 {
		t.Fatalf("numeric slider direction override: %v", value)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	n := node(t, tt.h.access, platform.RoleSlider, "slider")
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: n.ID, Action: platform.AccessIncrement})
	if value != 52 {
		t.Fatalf("numeric increment under a flow Reverse: %v", value)
	}
}
