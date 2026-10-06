package glass

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMaterialsFollowReducedTransparency(t *testing.T) {
	for _, test := range []struct {
		name     string
		material ui.Material
		fallback ui.Color
	}{
		{"glass", Glass{}, ui.LightTheme().Surface},
		{"clear glass", Glass{Style: Clear}, ui.LightTheme().Surface},
		{"tinted glass", Glass{Tint: ui.RGB(90, 30, 160)}, ui.RGB(90, 30, 160)},
		{"blur", Blur{Radius: 8}, ui.LightTheme().Background},
		{"soft edge", ScrollEdge{}, ui.LightTheme().Background},
		{"hard edge", ScrollEdge{Hard: true}, ui.LightTheme().Background},
	} {
		t.Run(test.name, func(t *testing.T) {
			backdrop := ui.RGB(20, 80, 140)
			ownTheme := false
			var prefs ui.Preferences
			tt := ui.NewTester(func(c *ui.Context) {
				if ownTheme {
					c.SetTheme(ui.LightTheme())
				}
				ui.Box(c).Fill().Background(backdrop).Children(func() {
					ui.Box(c).Absolute().Left(20).Top(20).Size(160, 60).Radius(12).Material(test.material).
						DrawOver(func(p *ui.Painter, _ ui.Rect) { prefs = p.Preferences() })
				})
			}, 220, 120)
			normal := tt.Image().RGBAAt(90, 50)
			tt.SetPreferences(ui.Preferences{ReduceTransparency: true})
			want := color.RGBA{R: test.fallback.R, G: test.fallback.G, B: test.fallback.B, A: 255}
			if got := tt.Image().RGBAAt(90, 50); got != want || !prefs.ReduceTransparency {
				t.Fatalf("opaque fallback: %v, want %v; prefs %+v", got, want, prefs)
			}
			backdrop = ui.RGB(180, 20, 60)
			tt.Frame()
			if got := tt.Image().RGBAAt(90, 50); got != want {
				t.Fatalf("backdrop leaked through opaque material: %v", got)
			}
			backdrop = ui.RGB(20, 80, 140)
			tt.SetPreferences(ui.Preferences{})
			if got := tt.Image().RGBAAt(90, 50); got != normal {
				t.Fatalf("live change did not restore material: %v, want %v", got, normal)
			}
			tt.SetPreferences(ui.Preferences{HighContrast: true})
			contrastWant := want
			if test.name == "glass" || test.name == "clear glass" {
				bg := ui.LightTheme().Background
				contrastWant = color.RGBA{R: bg.R, G: bg.G, B: bg.B, A: 255}
			}
			if got := tt.Image().RGBAAt(90, 50); got != contrastWant {
				t.Fatalf("high contrast retained translucent material: %v", got)
			}
			ownTheme = true
			tt.SetPreferences(ui.Preferences{ReduceTransparency: true})
			if got := tt.Image().RGBAAt(90, 50); got != normal {
				t.Fatalf("explicit app theme was overridden: %v", got)
			}
		})
	}
}

func TestGlassContrastFallbackPairsWithInheritedText(t *testing.T) {
	window, foreground := ui.RGB(16, 16, 48), ui.RGB(255, 255, 0)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Absolute().Left(20).Top(20).Size(180, 70).Radius(12).Material(Glass{}).Center().
			Children(func() { ui.Text(c, "Readable").FontSize(24).Bold() })
	}, 220, 110)
	tt.SetPreferences(ui.Preferences{HighContrast: true, ContrastColors: ui.ContrastColors{
		Window: window, WindowText: foreground,
		ButtonFace: foreground, ButtonText: window,
		Highlight: ui.RGB(0, 255, 255), HighlightText: window,
		GrayText: ui.RGB(220, 220, 220), Hotlight: ui.RGB(0, 255, 255),
	}})
	background, ink := 0, 0
	for y := 30; y < 80; y++ {
		for x := 35; x < 185; x++ {
			got := tt.Image().RGBAAt(x, y)
			if got == (color.RGBA{window.R, window.G, window.B, 255}) {
				background++
			}
			if got == (color.RGBA{foreground.R, foreground.G, foreground.B, 255}) {
				ink++
			}
		}
	}
	if background < 1000 || ink < 10 {
		t.Fatalf("button colors obscured the inherited window text: background=%d ink=%d", background, ink)
	}
}
