package ui

import (
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/scene"
)

func contrastSample() ContrastColors {
	return ContrastColors{
		Window: Hex("#101030"), WindowText: Hex("#ffff00"),
		ButtonFace: Hex("#303050"), ButtonText: Hex("#ffffff"),
		Highlight: Hex("#ffff00"), HighlightText: Hex("#101030"),
		GrayText: Hex("#b0b0c0"), Hotlight: Hex("#00ffff"),
	}
}

func TestPreferenceSnapshotAndLiveReset(t *testing.T) {
	var got Preferences
	var theme Theme
	tt := NewTester(func(c *Context) { got, theme = c.Preferences(), *c.Theme() }, 200, 100)
	want := Preferences{
		Accent: Hex("#ff0000"), HighContrast: true, ReduceMotion: true,
		TextScale: 1.25, ScrollbarVisibility: ScrollbarAlways, ReduceTransparency: true,
		ContrastColors: contrastSample(),
	}
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.SetPreferences(want)
		if got != want {
			t.Fatalf("preference snapshot: %+v, want %+v", got, want)
		}
		c := want.ContrastColors
		if theme.Background != c.Window || theme.Text != c.WindowText || theme.Surface != c.ButtonFace ||
			theme.SurfaceText != c.ButtonText || theme.TextMuted != c.GrayText ||
			theme.Accent != c.Highlight || theme.AccentText != c.HighlightText ||
			theme.Selection != c.Highlight || theme.SelectionText != c.HighlightText ||
			theme.Link != c.Hotlight || theme.Focus != c.WindowText {
			t.Fatalf("contrast palette lost in theme: %+v", theme)
		}
		if !theme.ReduceTransparency || theme.ScrollbarVisibility != ScrollbarAlways {
			t.Fatalf("theme did not follow preferences: %+v", theme)
		}
		tt.SetPreferences(Preferences{})
		base := LightTheme()
		if dark {
			base = DarkTheme()
		}
		if theme != *base || got.TextScale != 1 || got.ContrastColors != (ContrastColors{}) {
			t.Fatalf("live reset retained preferences: %+v, %+v", got, theme)
		}
	}
}

func TestContrastSelectionForeground(t *testing.T) {
	for _, kind := range []string{"input", "area", "text"} {
		t.Run(kind, func(t *testing.T) {
			value := "Selected words"
			var e *Element
			tt := NewTester(func(c *Context) {
				switch kind {
				case "input":
					e = TextInput(c, &value)
				case "area":
					e = TextArea(c, &value)
				case "text":
					e = Text(c, value).Selectable()
				}
				e.Absolute().Left(20).Top(20).Size(220, 80).FontSize(24)
			}, 280, 130)
			tt.SetPreferences(Preferences{HighContrast: true, ContrastColors: contrastSample()})
			tt.ClickAt(35, 35)
			tt.Key(Cmd, KeyA)
			a, b := e.st.editor.selection()
			if a != 0 || b != len(value) {
				t.Fatalf("selection %d:%d", a, b)
			}
			// WindowText equals Highlight in this palette. Leaving the
			// ordinary foreground in place would erase the selected text.
			fg := contrastSample().HighlightText
			var selectedBox scene.Rect
			for _, op := range tt.h.last.Ops {
				if op.Kind == scene.OpFill && op.Color == contrastSample().Highlight.scene() && op.Rect.W > 30 {
					selectedBox = op.Rect
					break
				}
			}
			pixels := 0
			for y := int(selectedBox.Y) + 1; y < int(selectedBox.Y+selectedBox.H)-1; y++ {
				for x := int(selectedBox.X) + 1; x < int(selectedBox.X+selectedBox.W)-1; x++ {
					if tt.Image().RGBAAt(x, y) == (color.RGBA{fg.R, fg.G, fg.B, fg.A}) {
						pixels++
					}
				}
			}
			if pixels < 10 {
				t.Fatalf("selected text lost its contrasting foreground (%d pixels)", pixels)
			}
			found := false
			for _, g := range tt.h.last.Glyphs {
				found = found || g.Color == fg.scene()
			}
			if !found {
				t.Fatal("selection foreground missing from text drawing")
			}
		})
	}
}

func TestExplicitElementPreferenceStyles(t *testing.T) {
	var input, button *Element
	value := "App styled"
	fg, bg := Hex("#ff00ff"), Hex("#003300")
	tt := NewTester(func(c *Context) {
		input = TextInput(c, &value).TextColor(fg).Background(bg).Opacity(0.7)
		button = Button(c, "Disabled app style").Disabled(true).TextColor(fg).Background(bg)
	}, 300, 140)
	tt.SetPreferences(Preferences{HighContrast: true, ReduceTransparency: true, ContrastColors: contrastSample()})
	if input.resolvedText().color != fg || input.bg != bg || input.opacity != 0.7 ||
		button.resolvedText().color != fg || button.bg != bg {
		t.Fatal("preferences rewrote explicit element styling")
	}
}

func TestContrastFocusAndDisabledControls(t *testing.T) {
	var button *Element
	tt := NewTester(func(c *Context) {
		Column(c).Padding(20).Gap(16).Children(func() {
			button = Button(c, "Focus")
			Button(c, "Disabled").Disabled(true)
		})
	}, 220, 150)
	tt.SetPreferences(Preferences{HighContrast: true, ContrastColors: contrastSample()})
	tt.Key(0, KeyTab)
	if !button.FocusVisible() {
		t.Fatal("Tab did not show keyboard focus")
	}
	ring, halo, disabledText := false, false, false
	for _, op := range tt.h.last.Ops {
		if op.Kind == scene.OpFill && op.Border[0] > 0 {
			ring = ring || op.BorderColor == contrastSample().WindowText.scene()
			halo = halo || op.BorderColor == contrastSample().Window.scene()
		}
	}
	for _, g := range tt.h.last.Glyphs {
		disabledText = disabledText || g.Color == contrastSample().GrayText.scene()
	}
	if !ring || !halo || !disabledText {
		t.Fatalf("ring=%v halo=%v disabled foreground=%v", ring, halo, disabledText)
	}
}

func TestContrastGridSelection(t *testing.T) {
	selected := 0
	state := GridState{Selected: &selected}
	var grid *Element
	tt := NewTester(func(c *Context) {
		grid = GridView(c, &state, 2, 100, 70, func(i int) { Textf(c, "Item %d", i) }).Fill()
	}, 220, 150)
	tt.SetPreferences(Preferences{HighContrast: true, ContrastColors: contrastSample()})
	tt.Key(0, KeyTab)
	cell := grid.first.first.first
	if cell.bg != contrastSample().Highlight || cell.resolvedText().color != contrastSample().HighlightText {
		t.Fatalf("selected grid item lost its opaque color pair: %v / %v", cell.bg, cell.resolvedText().color)
	}
}

func TestContrastSidebarSelectionWithoutFocus(t *testing.T) {
	selected := "chosen"
	var item *Element
	tt := NewTester(func(c *Context) {
		Row(c).Fill().Children(func() {
			Sidebar(c, &selected, func() { item = SidebarItem(c, "chosen", nil, "Chosen") }).Width(180)
			Button(c, "Other")
		})
	}, 320, 200)
	tt.SetPreferences(Preferences{HighContrast: true, ContrastColors: contrastSample()})
	tt.Click("Chosen")
	tt.Click("Other")
	if item.bg != contrastSample().Highlight || item.resolvedText().color != contrastSample().HighlightText {
		t.Fatal("unfocused sidebar selection disappeared into its control face")
	}
	tt.SetPreferences(Preferences{})
	if item.bg != LightTheme().SurfacePressed {
		t.Fatal("normal inactive sidebar styling did not return")
	}
}

func TestHighContrastSelectionChoosesReadableForeground(t *testing.T) {
	var theme Theme
	tt := NewTester(func(c *Context) { theme = *c.Theme() }, 200, 100)
	for _, sample := range []struct{ accent, foreground Color }{
		{Hex("#007aff"), RGB(0, 0, 0)}, {Hex("#ffff00"), RGB(0, 0, 0)},
		{Hex("#111166"), RGB(255, 255, 255)},
	} {
		tt.SetPreferences(Preferences{HighContrast: true, Accent: sample.accent})
		if theme.Selection != sample.accent || theme.SelectionText != sample.foreground || theme.Focus.A != 255 {
			t.Fatalf("unreadable high contrast selection: %v / %v", theme.Selection, theme.SelectionText)
		}
	}
}

func TestExplicitThemeKeepsPreferenceOverrides(t *testing.T) {
	own := LightTheme()
	own.ScrollbarVisibility = ScrollbarNever
	own.Selection, own.SelectionText = Hex("#663399"), Hex("#ffffaa")
	var got Theme
	tt := NewTester(func(c *Context) { c.SetTheme(own); got = *c.Theme() }, 200, 100)
	tt.SetPreferences(Preferences{HighContrast: true, ReduceTransparency: true, ScrollbarVisibility: ScrollbarAlways, ContrastColors: contrastSample()})
	if got != *own {
		t.Fatal("OS preferences rewrote an explicit app theme")
	}
}

func TestThemeFollowsTheAccent(t *testing.T) {
	var theme *Theme
	tt := NewTester(func(c *Context) { theme = c.Theme() }, 200, 100)
	if theme.Accent != LightTheme().Accent {
		t.Fatalf("without an accent of the desktop: %v", theme.Accent)
	}
	pink := Color{R: 219, G: 39, B: 119, A: 255}
	tt.SetPreferences(Preferences{Accent: pink})
	if theme.Accent != pink || theme.AccentText != (Color{R: 255, G: 255, B: 255, A: 255}) || theme.AccentHover == pink {
		t.Errorf("pink: accent %v, text %v, hover %v", theme.Accent, theme.AccentText, theme.AccentHover)
	}
	if theme.Focus.A == 0 || theme.Selection.A == 0 || theme.Focus.R != pink.R {
		t.Errorf("pink: focus %v, selection %v", theme.Focus, theme.Selection)
	}
	// Text on a light accent is dark.
	tt.SetPreferences(Preferences{Accent: Color{R: 250, G: 204, B: 21, A: 255}})
	if l := theme.AccentText.gray().R; l > 100 {
		t.Errorf("text on yellow: %v", theme.AccentText)
	}
	tt.SetDark(true)
	if !theme.Dark || theme.Accent != (Color{R: 250, G: 204, B: 21, A: 255}) {
		t.Errorf("dark: %+v", theme)
	}
}

func TestThemeFollowsContrastAndTextSize(t *testing.T) {
	var theme *Theme
	var prefs Preferences
	tt := NewTester(func(c *Context) { theme, prefs = c.Theme(), c.Preferences() }, 200, 100)
	if prefs.TextScale != 1 || prefs.HighContrast || prefs.ReduceMotion {
		t.Fatalf("by default: %+v", prefs)
	}
	base := LightTheme()
	tt.SetPreferences(Preferences{HighContrast: true, TextScale: 1.5})
	if theme.FontSize != base.FontSize*1.5 {
		t.Errorf("font size %v, not %v", theme.FontSize, base.FontSize*1.5)
	}
	if theme.Border.gray().R >= base.Border.gray().R || theme.TextMuted.gray().R >= base.TextMuted.gray().R || theme.Focus.A != 255 {
		t.Errorf("high contrast: border %v, muted %v, focus %v", theme.Border, theme.TextMuted, theme.Focus)
	}
}

func TestAnimateWithoutMotion(t *testing.T) {
	target := float32(0)
	var got float32
	tt := NewTester(func(c *Context) {
		got = Box(c).Size(10, 10).Animate("x", target, time.Second)
	}, 200, 100)
	target = 1
	tt.Frame()
	if got == 1 {
		t.Fatal("Animate jumped with motion")
	}
	tt.SetPreferences(Preferences{ReduceMotion: true})
	target = 2
	tt.Frame()
	if got != 2 {
		t.Errorf("without motion, Animate is at %v", got)
	}
}

func TestOwnThemeIgnoresPreferences(t *testing.T) {
	var accent Color
	tt := NewTester(func(c *Context) {
		c.SetTheme(LightTheme())
		accent = c.Theme().Accent
	}, 200, 100)
	tt.SetPreferences(Preferences{Accent: Color{R: 219, G: 39, B: 119, A: 255}})
	if accent != LightTheme().Accent {
		t.Errorf("a theme of the app's took the desktop's accent: %v", accent)
	}
}
