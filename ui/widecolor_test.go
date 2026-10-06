package ui

import (
	"math"
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

var wideGreen = Oklch(0.85, 0.3, 145)

func TestOklchInsideSRGBHasNoWideColor(t *testing.T) {
	g := Oklch(0.6, 0, 0)
	if _, ok := g.wideRGBA(); ok || g.A != 255 || g.R != g.G || g.G != g.B {
		t.Errorf("gray = %+v", g)
	}
	if got := Oklch(1, 0.2, 30); got != RGB(255, 255, 255) {
		t.Errorf("white = %+v", got)
	}
}

func TestOklchOutsideSRGBKeepsWideColor(t *testing.T) {
	a, ok := wideGreen.wideRGBA()
	if !ok {
		t.Fatal("vivid green has no wide color")
	}
	if !(a[0] < 0 || a[1] > 1 || a[2] < 0 || a[0] > 1 || a[1] < 0 || a[2] > 1) {
		t.Errorf("wide = %v, want a component outside 0 to 1", a)
	}
	if wideGreen.G <= wideGreen.R || wideGreen.G <= wideGreen.B {
		t.Errorf("sRGB fallback = %+v", wideGreen)
	}
	if wideGreen.SRGB().wide.ok {
		t.Error("SRGB keeps the wide color")
	}
}

func TestOklchAlphaAndNaN(t *testing.T) {
	c := Oklch(0.85, 0.3, 145).Alpha(0.5)
	if a, ok := c.wideRGBA(); !ok || a[3] < 0.49 || a[3] > 0.51 || c.A != 128 {
		t.Errorf("alpha: %v %v A=%d", a, ok, c.A)
	}
	nan := float32(math.NaN())
	if n := Oklch(nan, 0.3, 145); n.wide.ok || n != n {
		t.Errorf("NaN color keeps a wide color: %+v", n)
	}
}

func TestMixKeepsWideColor(t *testing.T) {
	m := wideGreen.Mix(RGB(0, 0, 0), 0.5)
	if !m.wide.ok {
		t.Error("Mix drops the wide color")
	}
	if RGB(1, 2, 3).Mix(RGB(4, 5, 6), 0.5).wide.ok {
		t.Error("Mix of sRGB colors has a wide color")
	}
	p := mixPremultiplied(Transparent, wideGreen, 0.5)
	if !p.wide.ok {
		t.Error("mixPremultiplied drops the wide color")
	}
}

func TestBackgroundDrawsNearestSRGB(t *testing.T) {
	tt := NewTester(func(c *Context) {
		Box(c).Size(20, 20).Background(wideGreen)
	}, 20, 20)
	want, got := wideGreen, tt.Image().RGBAAt(10, 10)
	d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
	if d(got.R, want.R) > 1 || d(got.G, want.G) > 1 || d(got.B, want.B) > 1 {
		t.Errorf("drew %v, want %v", got, want)
	}
}

func TestWideShadowStripesGradientAndText(t *testing.T) {
	w := wideGreen
	wa, _ := w.wideRGBA()
	tt := NewTester(func(c *Context) {
		Column(c).Gap(10).Padding(20).Children(func() {
			Box(c).Size(40, 20).Shadow(0, 4, 8, 0, RGB(0, 0, 0)).Shadow(0, 4, 8, 0, w).Background(RGB(255, 255, 255))
			Box(c).Size(40, 20).Background(RGB(255, 255, 255)).Stripes(w, 4, 4, 0)
			Box(c).Size(40, 20).Gradient(w, RGB(0, 0, 0), 90)
			Text(c, "wide").TextColor(w)
			RichText(c, Span{Text: "span", Weight: 700, Color: w}, Span{Text: " plain"})
		})
	}, 200, 250)
	s := tt.h.last
	var shadows, stripes, gradients int
	for _, op := range s.Ops {
		switch {
		case op.Kind == scene.OpShadow && op.Wide.Set&scene.WideColor != 0:
			shadows++
			if op.Wide.Color != wa {
				t.Errorf("shadow wide color = %v, want %v", op.Wide.Color, wa)
			}
		case op.Kind == scene.OpShadow:
			if op.Wide.Set != 0 {
				t.Errorf("plain shadow has wide colors: %+v", op.Wide)
			}
		case op.Paint == scene.PaintStripes && op.Wide.Set&scene.WideColor != 0:
			stripes++
			if op.Wide.Color != wa || op.Color != w.scene() {
				t.Errorf("stripes: wide %v, color %v", op.Wide.Color, op.Color)
			}
		case op.Paint == scene.PaintLinear && op.Wide.Set&scene.WideColor != 0:
			gradients++
			if op.Wide.Set&scene.WideColor2 != 0 {
				t.Error("the black end of the gradient is wide")
			}
		}
	}
	if shadows != 1 || stripes != 1 || gradients != 1 {
		t.Errorf("wide shadows %d, stripes %d, gradients %d, want 1 each", shadows, stripes, gradients)
	}
	var wideGlyphs, plainGlyphs int
	for _, g := range s.Glyphs {
		if g.HasWide {
			wideGlyphs++
			if g.Wide != wa {
				t.Fatalf("glyph wide color = %v, want %v", g.Wide, wa)
			}
			if g.Color != w.scene() {
				t.Errorf("glyph fallback color = %v", g.Color)
			}
		} else {
			plainGlyphs++
		}
	}
	if wideGlyphs < 8 || plainGlyphs == 0 {
		t.Errorf("wide glyphs %d, plain glyphs %d", wideGlyphs, plainGlyphs)
	}
	if !s.HasWide() {
		t.Error("HasWide is false")
	}
}

func TestTextColorOverridesWide(t *testing.T) {
	tt := NewTester(func(c *Context) {
		Text(c, "plain").TextColor(wideGreen).TextColor(RGB(10, 20, 30))
	}, 100, 40)
	if tt.h.last.HasWide() {
		t.Error("TextColor after a wide TextColor still draws the wide color")
	}
}

func TestWideDecorationsAndTextBackground(t *testing.T) {
	wa, _ := wideGreen.wideRGBA()
	tt := NewTester(func(c *Context) {
		RichText(c, Span{Text: "marked", Underline: true, DecorationColor: wideGreen}, Span{Text: "bg", Background: wideGreen})
	}, 200, 60)
	var wide int
	for _, op := range tt.h.last.Ops {
		if op.Kind == scene.OpFill && op.Wide.Set&scene.WideColor != 0 {
			wide++
			if op.Wide.Color != wa {
				t.Errorf("wide color = %v, want %v", op.Wide.Color, wa)
			}
		}
	}
	if wide < 2 {
		t.Errorf("wide fills = %d, want at least 2 (underline, background)", wide)
	}
}

func TestThemeAccentTakesOklch(t *testing.T) {
	th := *LightTheme()
	th.Accent = wideGreen
	if !th.Accent.wide.ok {
		t.Error("Theme.Accent lost the wide color")
	}
}
