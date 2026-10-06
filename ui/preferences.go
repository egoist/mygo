package ui

import (
	"math"

	"github.com/egoist/mygo/internal/platform"
)

// Preferences are settings of the desktop that the system's own controls
// follow. The default theme follows them, and Animate follows
// ReduceMotion; a theme of your own (Context.SetTheme) and drawing of your
// own may read them with Context.Preferences.
type Preferences struct {
	// Accent is the accent color the user chose, which the default theme
	// takes; A is 0 where the desktop has none.
	Accent Color
	// ReduceMotion asks for less motion: macOS's Reduce Motion, Windows's
	// animation effects and GNOME's animations turned off. Animate then
	// goes to its target at once; Loop, which shows that something is
	// going on, goes on.
	ReduceMotion bool
	// HighContrast asks for more contrast: macOS's Increase Contrast,
	// Windows's contrast themes, the desktop portal's higher contrast. The
	// default theme uses paired selection colors, stronger borders and
	// secondary text, and an opaque focus ring with a contrasting halo.
	HighContrast bool
	// TextScale is how many times larger than usual text should be, as
	// Windows's and GNOME's text size settings say, 1 for usual: the
	// default theme's FontSize is that much larger.
	TextScale float32
	// ScrollbarVisibility is Auto for indicators shown while hovered or
	// dragged, Always for permanent bars, or OnScroll for macOS's explicit
	// "When scrolling" preference.
	ScrollbarVisibility ScrollbarVisibility
	// ReduceTransparency asks materials to use an opaque background:
	// macOS's Reduce Transparency and Windows's transparency effects off.
	// It is false on desktops without an available setting.
	ReduceTransparency bool
	// ContrastColors is the actual Windows contrast-theme palette while
	// HighContrast is true. Window.A is zero when none is available.
	ContrastColors ContrastColors
}

// ScrollbarVisibility controls scrollbars in overflowing native UI containers.
type ScrollbarVisibility uint8

const (
	// ScrollbarAuto shows overlay scrollbars while hovered or dragged.
	ScrollbarAuto ScrollbarVisibility = iota
	// ScrollbarAlways keeps overlay scrollbars visible, including at idle.
	ScrollbarAlways
	// ScrollbarOnScroll shows bars during and briefly after scrolling.
	ScrollbarOnScroll
	// ScrollbarNever hides scrollbars and their pointer targets. Scrolling
	// by the wheel, keyboard or app remains available.
	ScrollbarNever
)

// ContrastColors contains Windows's paired system colors. Apps drawing their
// own controls can use these without assuming a black or white contrast theme.
// All fields are opaque when available; otherwise Window.A is zero.
type ContrastColors struct {
	Window, WindowText, ButtonFace, ButtonText   Color
	Highlight, HighlightText, GrayText, Hotlight Color
}

// Preferences returns the settings of the desktop that controls follow.
// A frame follows their changes.
func (c *Context) Preferences() Preferences { return c.rt.preferences() }

func fromPlatformColor(c platform.Color) Color { return Color{R: c.R, G: c.G, B: c.B, A: c.A} }
func toPlatformColor(c Color) platform.Color   { return platform.Color{R: c.R, G: c.G, B: c.B, A: c.A} }

// preferences returns the desktop's settings, read once until they change.
func (rt *engine) preferences() Preferences {
	if !rt.prefsKnown {
		p := rt.host.preferences()
		rt.prefs = Preferences{
			Accent:              Color{R: p.Accent.R, G: p.Accent.G, B: p.Accent.B, A: p.Accent.A},
			ReduceMotion:        p.ReduceMotion,
			HighContrast:        p.HighContrast,
			TextScale:           float32(p.TextScale),
			ScrollbarVisibility: ScrollbarVisibility(p.ScrollbarVisibility),
			ReduceTransparency:  p.ReduceTransparency,
			ContrastColors: ContrastColors{
				Window: fromPlatformColor(p.ContrastColors.Window), WindowText: fromPlatformColor(p.ContrastColors.WindowText),
				ButtonFace: fromPlatformColor(p.ContrastColors.ButtonFace), ButtonText: fromPlatformColor(p.ContrastColors.ButtonText),
				Highlight: fromPlatformColor(p.ContrastColors.Highlight), HighlightText: fromPlatformColor(p.ContrastColors.HighlightText),
				GrayText: fromPlatformColor(p.ContrastColors.GrayText), Hotlight: fromPlatformColor(p.ContrastColors.Hotlight),
			},
		}
		if rt.prefs.TextScale <= 0 {
			rt.prefs.TextScale = 1
		}
		rt.prefsKnown = true
	}
	return rt.prefs
}

// follow makes the theme follow the desktop's settings.
func (t *Theme) follow(p Preferences) {
	t.ScrollbarVisibility = p.ScrollbarVisibility
	t.HighContrast = p.HighContrast
	t.ReduceTransparency = p.ReduceTransparency || p.HighContrast
	if a := p.Accent; a.A > 0 {
		a.A = 255
		black, white := Color{A: 255}, Color{R: 255, G: 255, B: 255, A: 255}
		t.Accent = a
		if t.Dark {
			t.AccentHover, t.AccentPressed = a.Mix(white, 0.2), a.Mix(black, 0.15)
			t.Selection, t.Focus = a.Alpha(0.4), a.Mix(white, 0.2).Alpha(0.6)
		} else {
			t.AccentHover, t.AccentPressed = a.Mix(black, 0.12), a.Mix(black, 0.25)
			t.Selection, t.Focus = a.Alpha(0.25), a.Alpha(0.55)
		}
		// Text on the accent is black where white would not stand out, as
		// on yellow.
		t.AccentText = white
		if l := a.gray().R; l > 165 {
			t.AccentText = Color{R: 24, G: 24, B: 27, A: 255}
		}
	}
	if p.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus = t.Text
		t.Scrollbar = t.Text
		t.ScrollbarTrack = t.Background
		t.AccentText = contrastForeground(t.Accent)
		t.Selection, t.SelectionText = t.Accent, t.AccentText
		t.Selection.A = 255
		if c := p.ContrastColors; c.Window.A != 0 {
			t.Background, t.Text, t.TextMuted = c.Window, c.WindowText, c.GrayText
			t.Surface, t.SurfaceHover, t.SurfacePressed = c.ButtonFace, c.ButtonFace, c.ButtonFace
			t.SurfaceText, t.Border = c.ButtonText, c.ButtonText
			t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = c.Highlight, c.Highlight, c.Highlight, c.HighlightText
			t.Selection, t.SelectionText, t.Focus = c.Highlight, c.HighlightText, c.WindowText
			t.Scrollbar, t.Link = c.WindowText, c.Hotlight
			t.ScrollbarTrack = c.Window
			t.Inverse, t.InverseText = c.WindowText, c.Window
			t.Danger, t.Warning, t.Success = c.WindowText, c.WindowText, c.WindowText
		}
	}
	if s := p.TextScale; s > 0 && s != 1 {
		t.FontSize *= s
	}
}

// contrastForeground chooses the stronger black/white text pair using sRGB
// relative luminance. Windows's user-selected contrast pairs bypass it.
func contrastForeground(c Color) Color {
	linear := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	l := 0.2126*linear(c.R) + 0.7152*linear(c.G) + 0.0722*linear(c.B)
	if (l+0.05)/0.05 >= 1.05/(l+0.05) {
		return RGB(0, 0, 0)
	}
	return RGB(255, 255, 255)
}
