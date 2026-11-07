package ui

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/scene"
)

// TestSpanBlurMorph keeps a blurred span blurred while its text morphs:
// its glyphs, those that move and those that come, paint blurred.
func TestSpanBlurMorph(t *testing.T) {
	spans := []Span{{Text: "is "}, {Text: "HH", Blur: 4}}
	a := Animation{Duration: 100 * time.Millisecond, Ease: Linear}
	tt, now := clockTester(func(c *context) {
		coreRichText(c, spans...).FontSize(32).ContentTransition(ContentTransition{Morph: true, Animation: &a})
	}, 300, 100)
	spans = []Span{{Text: "was "}, {Text: "HHH", Blur: 4}}
	tt.Frame()
	*now = now.Add(50 * time.Millisecond)
	tt.Frame()
	// A blurred H spreads 3 sigmas each side of its 20 or so pixels.
	wide := 0
	for _, g := range tt.rt.scene.Glyphs {
		if g.W > 40 {
			wide++
		}
	}
	if wide != 3 {
		t.Errorf("halfway, %d glyphs paint blurred, want the three H", wide)
	}
}

// spanBlurView shows three runs of H on a line, the middle one blurred by
// blur DIPs, and notes the paragraph in *para.
func spanBlurView(blur float32, para **node) func(c *context) {
	return func(c *context) {
		coreBox(c).Fill().Background(RGB(255, 255, 255)).Children(func() {
			*para = coreRichText(c,
				Span{Text: "HH      "},
				Span{Text: "HH", Blur: blur},
				Span{Text: "      HH"},
			).FontSize(32).TextColor(RGB(0, 0, 0)).Margin(10)
		})
	}
}

// blurredRun returns the left and right, in pixels, of the glyphs of the
// middle span of spanBlurView's paragraph, at scale.
func blurredRun(t *testing.T, para *node, scale float32) (x0, x1 int) {
	t.Helper()
	l := para.tl
	if l == nil || len(l.Lines) != 1 {
		t.Fatal("the paragraph is not laid out on one line")
	}
	lo, hi := float32(1e9), float32(-1e9)
	for _, g := range l.Lines[0].Glyphs {
		if g.Cluster >= 8 && g.Cluster < 10 {
			lo, hi = min(lo, g.X), max(hi, g.X+g.Advance)
		}
	}
	ox := para.x + para.contentX()
	return int((ox + lo) * scale), int((ox + hi) * scale)
}

// TestSpanBlur blurs one run of a line: its glyphs lose their hard
// pixels and spread, while the runs beside it are as they are unblurred,
// pixel for pixel, beyond the blur's reach.
func TestSpanBlur(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		var para *node
		sharpT := coreNewTester(spanBlurView(0, &para), 400, 80)
		sharpT.SetScale(scale)
		sharp := sharpT.Image()
		x0, x1 := blurredRun(t, para, scale)
		softT := coreNewTester(spanBlurView(4, &para), 400, 80)
		softT.SetScale(scale)
		soft := softT.Image()
		if a, b := blurredRun(t, para, scale); a != x0 || b != x1 {
			t.Fatalf("the blurred run moved: %d–%d, sharp %d–%d", a, b, x0, x1)
		}
		// Three sigmas, and the pixels the blurred glyph's mask pads.
		reach := int(3*4*scale) + 2
		bounds := sharp.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				if x > x0-reach && x < x1+reach {
					continue
				}
				if sharp.RGBAAt(x, y) != soft.RGBAAt(x, y) {
					t.Fatalf("at %v: pixel %d,%d is %v, unblurred %v, beyond the blurred run %d–%d", scale, x, y, soft.RGBAAt(x, y), sharp.RGBAAt(x, y), x0, x1)
				}
			}
		}
		sharpInk, sharpDark := inkIn(sharp, x0-reach, x1+reach)
		softInk, softDark := inkIn(soft, x0-reach, x1+reach)
		if sharpDark == 0 || softDark != 0 {
			t.Errorf("at %v: %d black pixels sharp, %d blurred; want some, none", scale, sharpDark, softDark)
		}
		if softInk <= sharpInk {
			t.Errorf("at %v: the blur inks %d pixels, the sharp run %d", scale, softInk, sharpInk)
		}
	}
}

// inkIn counts the pixels of img between columns x0 and x1 that are not
// white, and those that are black, or nearly.
func inkIn(img *image.RGBA, x0, x1 int) (ink, dark int) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := max(x0, b.Min.X); x < min(x1, b.Max.X); x++ {
			c := img.RGBAAt(x, y)
			if c.R < 250 {
				ink++
			}
			if c.R < 40 {
				dark++
			}
		}
	}
	return ink, dark
}

// TestSpanBlurZero paints a span of Blur 0 as one with none.
func TestSpanBlurZero(t *testing.T) {
	view := func(blur bool) func(c *context) {
		return func(c *context) {
			s := Span{Text: "forty-two", Background: RGB(250, 204, 21), Underline: true}
			if blur {
				s.Blur = 0
			}
			coreRichText(c, Span{Text: "The answer is "}, s, Span{Text: "."}).TextColor(RGB(0, 0, 0))
		}
	}
	a, b := coreNewTester(view(false), 300, 60), coreNewTester(view(true), 300, 60)
	if !equalImages(a.Image(), b.Image()) {
		t.Error("a span of Blur 0 paints otherwise than one with none")
	}
	if n := countOps(a.h.last, scene.OpShadow); n != 0 {
		t.Errorf("a sharp span paints %d shadows", n)
	}
}

func equalImages(a, b *image.RGBA) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

func countOps(s *scene.Scene, k scene.Kind) int {
	n := 0
	for _, op := range s.Ops {
		if op.Kind == k {
			n++
		}
	}
	return n
}

// TestSpanBlurBackgroundAndLines blurs a span's background, underline and
// strikethrough with its glyphs: shadows that nothing casts, of twice the
// sigma's radius, rather than fills.
func TestSpanBlurBackgroundAndLines(t *testing.T) {
	yellow, red := RGB(250, 204, 21), RGB(255, 0, 0)
	tt := coreNewTester(func(c *context) {
		coreRichText(c,
			Span{Text: "The answer is "},
			Span{Text: "forty-two", Background: yellow, Underline: true, Strikethrough: true, DecorationColor: red, Blur: 3},
			Span{Text: "."},
		).TextColor(RGB(0, 0, 0))
	}, 300, 60)
	var backgrounds, lines int
	for _, op := range tt.h.last.Ops {
		switch {
		case op.Kind == scene.OpShadow && op.Color == yellow.scene():
			backgrounds++
			if op.Blur != 6 || !op.Cast.Empty() {
				t.Errorf("the background blurs by %v, cast by %v", op.Blur, op.Cast)
			}
		case op.Kind == scene.OpShadow && op.Color == red.scene():
			lines++
		case op.Kind == scene.OpFill && (op.Color == yellow.scene() || op.Color == red.scene()):
			t.Errorf("a blurred span fills %v sharp", op.Color)
		}
	}
	// The underline and the strikethrough: Core Text's underline skips the
	// ink of descenders, the y's here, in more than one stroke.
	if backgrounds != 1 || lines < 2 {
		t.Errorf("%d blurred backgrounds, %d blurred lines; want 1, at least 2", backgrounds, lines)
	}
}

// TestSpanBlurKeepsText keeps a blurred inline element's words as they
// are sharp: in the paragraph's text, read by assistive technology,
// selected, and clicked.
func TestSpanBlurKeepsText(t *testing.T) {
	clicks := 0
	var spoiler *node
	tt := coreNewTester(func(c *context) {
		coreRichText(c).Selectable().Children(func() {
			coreText(c, "The answer is ")
			spoiler = coreText(c, "forty-two").Blur(4)
			if spoiler.Clicked() {
				clicks++
			}
			coreText(c, ".")
		})
	}, 400, 100)
	const sentence = "The answer is forty-two."
	if !tt.HasText(sentence) {
		t.Errorf("the texts are %q", tt.Texts())
	}
	if _, ok := tt.Access(sentence); !ok {
		t.Error("assistive technology does not read the blurred words")
	}
	if src := tt.rt.states[spoiler.parent.id].editor.source; src != sentence {
		t.Errorf("the paragraph selects %q", src)
	}
	tt.ClickAt(center(spoiler.frags[0]))
	if clicks != 1 {
		t.Errorf("%d clicks on the blurred words", clicks)
	}
	// Painted blurred: its glyphs in a color glyph's or a mask of their
	// own, wider than they are, and no others sharp.
	var blurred int
	for _, op := range tt.h.last.Ops {
		if op.Kind == scene.OpGlyphs {
			for _, g := range tt.h.last.Glyphs[op.Start:op.End] {
				if g.W > 24 && g.H > 24 {
					blurred++
				}
			}
		}
	}
	if blurred < 8 { // the letters of forty-two, but the hyphen's thin
		t.Errorf("%d glyphs blurred, want those of forty-two", blurred)
	}
}

// BenchmarkRichTextSpans paints a frame of a paragraph of colored spans,
// none blurred, as code highlighted is: what Span.Blur must not slow.
func BenchmarkRichTextSpans(b *testing.B) {
	colors := []Color{RGB(207, 34, 46), RGB(5, 80, 174), RGB(17, 99, 41), RGB(36, 41, 47)}
	var spans []Span
	for i := range 200 {
		spans = append(spans, Span{Text: "word ", Color: colors[i%len(colors)]})
	}
	tt := coreNewTester(func(c *context) {
		coreRichText(c, spans...).FontSize(13)
	}, 800, 600)
	tt.SetScale(2)
	for b.Loop() {
		tt.h.img.Invalidate()
		tt.Frame()
	}
}
