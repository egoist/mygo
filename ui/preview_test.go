//go:build !mygo_noinspector

package ui

import (
	"fmt"
	"image"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

func previewCounter(initial int, made, disposed *int) func() PreviewSample {
	return func() PreviewSample {
		*made++
		n := initial
		value := ""
		return PreviewSample{View: func(c *Context) {
			Column(c).Padding(12).Gap(8).Children(func() {
				Textf(c, "Count %d", n)
				if Button(c, "Add").Clicked() {
					n++
				}
				field := TextInput(c, &value).Label("Editor")
				local := Local(field, "sample", func() int { return n })
				Textf(c, "Local %d", *local)
			})
		}, Dispose: func() { *disposed++ }}
	}
}

func TestPreviewSamplesAndConfiguration(t *testing.T) {
	var made, disposed int
	p := NewPreview(PreviewOptions{Config: PreviewConfig{Width: 240, Height: 180, Locale: "en-US"}, Presets: []PreviewPreset{
		{Name: "Empty", New: previewCounter(0, &made, &disposed)},
		{Name: "Populated", New: previewCounter(42, &made, &disposed), Config: PreviewConfig{Theme: PreviewDark, Scale: 1.25, Locale: "ja-JP"}},
	}})
	if made != 0 {
		t.Fatal("sample created before attaching a host")
	}
	tt := p.NewTester()
	t.Cleanup(tt.Close)
	if !tt.HasText("Count 0") || !tt.HasText("Local 0") || made != 1 {
		t.Fatalf("initial sample: %q, made %d", tt.Texts(), made)
	}
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	config := p.Config()
	config.Theme, config.Scale = PreviewDark, 2
	if err := p.SetConfig(config); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Count 1") || !tt.HasText("Local 0") || !tt.Focused("Editor") || made != 1 || disposed != 0 {
		t.Fatalf("environment change reset state: %q, made %d, disposed %d", tt.Texts(), made, disposed)
	}
	if got := tt.Image().Bounds().Size(); got != (image.Point{480, 360}) {
		t.Errorf("scale 2 capture: %v", got)
	}
	if px := tt.Image().RGBAAt(0, 0); px.R != DarkTheme().Background.R {
		t.Errorf("dark background: %v", px)
	}
	p.Reset()
	tt.Frame()
	if !tt.HasText("Count 0") || tt.Focused("Editor") || p.Config().Scale != 2 || made != 2 || disposed != 1 {
		t.Fatalf("reset: %q; configuration %+v; made %d, disposed %d", tt.Texts(), p.Config(), made, disposed)
	}
	if err := p.SelectPreset("Populated"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Count 42") || !tt.HasText("Local 42") || p.Preset() != "Populated" || p.Config().Width != 240 || p.Config().Locale != "ja-JP" {
		t.Fatalf("preset selection: %q; %+v", tt.Texts(), p.Config())
	}
	if got := tt.Image().Bounds().Size(); got != (image.Point{300, 225}) {
		t.Errorf("fractional scale: %v", got)
	}
	if err := p.SelectPreset("missing"); err == nil || made != 3 || disposed != 2 {
		t.Fatal("unknown preset changed the sample or returned no error")
	}
	tt.Close()
	tt.Close()
	if disposed != 3 || len(p.watchers) != 0 {
		t.Errorf("close: disposed %d, watchers %d", disposed, len(p.watchers))
	}
	p.Reset()
	tt.Frame()
	tt.Key(0, KeyEnter)
	if made != 3 {
		t.Fatal("closed tester created another sample")
	}
}

func TestPreviewPreferencesAndLocaleHook(t *testing.T) {
	var theme Theme
	var prefs Preferences
	var config PreviewConfig
	var size image.Point
	var animated float32
	target := float32(0)
	p := NewPreview(PreviewOptions{Presets: []PreviewPreset{{Name: "Settings", New: func() PreviewSample {
		return PreviewSample{View: func(c *Context) {
			theme, prefs = *c.Theme(), c.Preferences()
			var ok bool
			config, ok = PreviewEnvironment(c)
			if !ok {
				t.Fatal("sample has no preview configuration")
			}
			w, h := c.Size()
			size = image.Pt(int(w), int(h))
			Text(c, config.Locale)
			animated = Box(c).Size(10, 10).Animate("x", target, time.Second)
		}}
	}}}})
	tt := p.NewTester()
	t.Cleanup(tt.Close)
	tt.SetDark(true)
	tt.SetPreferences(Preferences{Accent: RGB(200, 20, 100), ReduceMotion: true, HighContrast: true, TextScale: 1.5})
	if !theme.Dark || theme.Accent != RGB(200, 20, 100) || theme.FontSize != DarkTheme().FontSize*1.5 {
		t.Fatalf("system configuration not followed: %+v", theme)
	}
	zero := Preferences{}
	err := p.SetConfig(PreviewConfig{Theme: PreviewLight, Width: 201, Height: 101, Scale: 1.25, Locale: "ar-EG", Preferences: &zero})
	if err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if theme.Dark || prefs.ReduceMotion || prefs.HighContrast || prefs.TextScale != 1 || theme.FontSize != LightTheme().FontSize || size != image.Pt(201, 101) || config.Locale != "ar-EG" {
		t.Fatalf("explicit override: theme %+v, prefs %+v, config %+v, size %v", theme, prefs, config, size)
	}
	if got := tt.Image().Bounds().Size(); got != image.Pt(252, 127) {
		t.Errorf("fractional viewport capture: %v", got)
	}
	// Config and the context hook return copies; neither can mutate a
	// running preview by changing an escaped preference pointer.
	config.Preferences.HighContrast = true
	copy := p.Config()
	copy.Preferences.ReduceMotion = true
	zero.TextScale = 3
	tt.Frame()
	if prefs.HighContrast || prefs.ReduceMotion || prefs.TextScale != 1 {
		t.Fatalf("preference pointers escaped: %+v", prefs)
	}
	copy = p.Config()
	copy.Preferences = &Preferences{ReduceMotion: true, HighContrast: true, TextScale: 2}
	_ = p.SetConfig(copy)
	target = 10
	tt.Frame()
	if animated != target || theme.Focus.A != 255 || theme.FontSize != LightTheme().FontSize*2 {
		t.Errorf("preview preferences did not reach the engine: animation %v, theme %+v", animated, theme)
	}
	copy.Preferences, copy.Theme = nil, PreviewSystem
	_ = p.SetConfig(copy)
	tt.Frame()
	if !theme.Dark || prefs.TextScale != 1.5 || theme.Accent != RGB(200, 20, 100) {
		t.Error("clearing overrides did not restore host settings")
	}
}

func TestPreviewValidationAndConcurrentController(t *testing.T) {
	p := NewPreview(PreviewOptions{Presets: []PreviewPreset{{Name: "One", New: func() PreviewSample {
		return PreviewSample{View: func(c *Context) { Text(c, "One") }}
	}}}})
	base := p.Config()
	for _, bad := range []PreviewConfig{
		{Theme: "unknown"}, {Width: -1}, {Height: -1}, {Width: 20000},
		{Scale: -1}, {Scale: 9}, {Scale: float32(math.NaN())}, {Scale: float32(math.Inf(1))},
		{Width: 8192, Height: 1, Scale: 4},
		{Width: 16384, Height: 16384, Scale: 8}, {Preferences: &Preferences{TextScale: -1}},
		{Preferences: &Preferences{TextScale: float32(math.NaN())}},
	} {
		if err := p.SetConfig(bad); err == nil || !reflect.DeepEqual(p.Config(), base) {
			t.Errorf("invalid config was accepted or changed state: %+v", bad)
		}
	}
	tt := p.NewTester()
	t.Cleanup(tt.Close)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 20; j++ {
				c := p.Config()
				c.Locale = fmt.Sprintf("sample-%d", j)
				_ = p.SetConfig(c)
				_ = p.SelectPreset("One")
				p.Reset()
			}
		})
	}
	wg.Wait()
	tt.Frame()
	if !tt.HasText("One") {
		t.Fatal("concurrent updates lost the sample")
	}
}

func TestPreviewInvalidSampleCleansUp(t *testing.T) {
	disposed := 0
	p := NewPreview(PreviewOptions{Presets: []PreviewPreset{{Name: "Invalid", New: func() PreviewSample {
		return PreviewSample{Dispose: func() { disposed++ }}
	}}}})
	func() {
		defer func() {
			if recover() == nil {
				t.Error("nil sample view did not panic")
			}
		}()
		p.NewTester()
	}()
	if disposed != 1 || len(p.watchers) != 0 {
		t.Fatalf("failed sample retained resources: disposed %d, watchers %d", disposed, len(p.watchers))
	}
}

func TestPreviewIndependentHostsAndTimers(t *testing.T) {
	var made, disposed int
	p := NewPreview(PreviewOptions{Presets: []PreviewPreset{{Name: "One", New: func() PreviewSample {
		factory := previewCounter(0, &made, &disposed)
		s := factory()
		view := s.View
		s.View = func(c *Context) {
			view(c)
			c.After(time.Hour)
			Box(c).Draw(func(p *Painter, r Rect) { p.After(time.Hour) })
		}
		return s
	}}}})
	a, b := p.NewTester(), p.NewTester()
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	_ = a.Click("Add")
	b.Frame()
	if !a.HasText("Count 1") || !b.HasText("Count 0") || made != 2 {
		t.Fatal("hosts share sample state")
	}
	if a.rt.timer == nil || a.rt.repaintTimer == nil {
		t.Fatal("sample did not arm its timers")
	}
	a.Close()
	if disposed != 1 || a.rt.timer.Stop() || a.rt.repaintTimer.Stop() || len(p.watchers) != 1 {
		t.Fatal("closing a host did not stop timers and remove its subscription")
	}
	p.Reset()
	b.Frame()
	if made != 3 || disposed != 2 {
		t.Fatal("remaining host did not reset independently")
	}
}

func TestPreviewControls(t *testing.T) {
	var made, disposed int
	p := NewPreview(PreviewOptions{Config: PreviewConfig{Width: 300, Height: 200}, Presets: []PreviewPreset{
		{Name: "Zero", New: previewCounter(0, &made, &disposed)},
		{Name: "Full", New: previewCounter(10, &made, &disposed)},
	}, Configure: func(c *Context, config *PreviewConfig) {
		if Button(c, "Large Japanese sample").Clicked() {
			config.Locale, config.Scale = "ja-JP", 2
		}
	}})
	sample := p.NewTester()
	controls := NewTester(p.Controls, 450, 900)
	t.Cleanup(sample.Close)
	t.Cleanup(controls.Close)
	for _, text := range []string{"Preview theme", "Preview width", "Preview height", "Preview scale", "Preview locale", "Follow system preferences"} {
		if _, ok := controls.Find(text); !ok {
			t.Errorf("no control %q", text)
		}
	}
	_ = controls.Click("Preview sample")
	if err := controls.Click("Full"); err != nil {
		t.Fatal(err)
	}
	sample.Frame()
	if !sample.HasText("Count 10") {
		t.Fatalf("sample control did not select a preset: %q", sample.Texts())
	}
	_ = controls.Click("Preview theme")
	if err := controls.Click("dark"); err != nil {
		t.Fatal(err)
	}
	_ = controls.Click("Follow system preferences")
	_ = controls.Click("Reduced motion")
	_ = controls.Click("High contrast")
	_ = controls.Click("Large Japanese sample")
	sample.Frame()
	if c := p.Config(); c.Theme != PreviewDark || c.Locale != "ja-JP" || c.Scale != 2 || c.Preferences == nil || !c.Preferences.ReduceMotion || !c.Preferences.HighContrast {
		t.Fatalf("configuration controls: %+v", c)
	}
	_ = sample.Click("Add")
	_ = controls.Click("Reset sample")
	sample.Frame()
	if !sample.HasText("Count 10") || p.Config().Locale != "ja-JP" {
		t.Fatal("reset control failed or changed configuration")
	}
}

func TestPreviewResetDropsToastsAndOldTimers(t *testing.T) {
	p := NewPreview(PreviewOptions{Presets: []PreviewPreset{{Name: "Toast", New: func() PreviewSample {
		return PreviewSample{View: func(c *Context) {
			if Button(c, "Notify").Clicked() {
				c.Toast("Old sample")
				c.After(time.Hour)
			}
		}}
	}}}})
	tt := p.NewTester()
	t.Cleanup(tt.Close)
	_ = tt.Click("Notify")
	if len(tt.rt.toasts) != 1 || tt.rt.timer == nil {
		t.Fatal("sample did not create a toast and timer")
	}
	timer := tt.rt.timer
	p.Reset()
	tt.Frame()
	if len(tt.rt.toasts) != 0 || timer.Stop() || tt.rt.pressed != nil {
		t.Fatal("sample reset retained old transient state")
	}
}

type previewSurface struct {
	w, h, scale float64
	frames      int
	pixelSize   image.Point
	ime         platform.TextInputState
	access      *platform.AccessTree
}

func (s *previewSurface) Native() platform.SurfaceNative             { return platform.SurfaceNative{} }
func (s *previewSurface) Size() (float64, float64, float64)          { return s.w, s.h, s.scale }
func (s *previewSurface) RefreshRate() float64                       { return 60 }
func (s *previewSurface) RequestFrame()                              {}
func (s *previewSurface) SetCursor(platform.Cursor)                  {}
func (s *previewSurface) SetTextInput(t platform.TextInputState)     { s.ime = t }
func (s *previewSurface) UpdateAccessibility(t *platform.AccessTree) { s.access = t }
func (s *previewSurface) PresentPixels(_ []byte, _, w, h int) {
	s.frames++
	s.pixelSize = image.Pt(w, h)
}

func TestPreviewNativeFitInputAccessibilityAndCleanup(t *testing.T) {
	var disposed, clicks int
	p := NewPreview(PreviewOptions{Config: PreviewConfig{Width: 200, Height: 200, Scale: 1.25}, Presets: []PreviewPreset{
		{Name: "Input", New: func() PreviewSample {
			value := "hello"
			return PreviewSample{View: func(c *Context) {
				Column(c).Padding(20).Gap(12).Children(func() {
					if Button(c, "Press").Clicked() {
						clicks++
					}
					TextInput(c, &value).Label("Editor")
				})
			}, Dispose: func() { disposed++ }}
		}},
	}})
	s := &previewSurface{w: 800, h: 400, scale: 2}
	conn := &surface.Conn{Surface: s, Invalidate: s.RequestFrame}
	p.Content().AttachContent(conn)
	t.Cleanup(conn.Detach)
	conn.Event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	conn.Event(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	conn.Event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if s.pixelSize != image.Pt(1600, 800) {
		t.Fatalf("presentation did not match native framebuffer: %v", s.pixelSize)
	}
	button := node(t, s.access, platform.RoleButton, "Press")
	if button.Bounds.X < 240 || button.Bounds.Y != 40 {
		t.Fatalf("accessible bounds were not fitted: %+v", button.Bounds)
	}
	click := func(r platform.RectF) {
		x, y := r.X+r.W/2, r.Y+r.H/2
		conn.Event(platform.SurfaceEvent{Kind: platform.PointerDown, X: x, Y: y})
		conn.Event(platform.SurfaceEvent{Kind: platform.PointerUp, X: x, Y: y})
		conn.Event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	}
	// A click on the surrounding margin must not reach the sample.
	click(platform.RectF{X: 0, Y: 40, W: 10, H: 10})
	if clicks != 0 {
		t.Fatal("letterbox margin reached the sample")
	}
	click(button.Bounds)
	if clicks != 1 {
		t.Fatal("native pointer was not mapped to the sample")
	}
	field := node(t, s.access, platform.RoleTextField, "Editor")
	click(field.Bounds)
	if !s.ime.Active || s.ime.Caret.X < field.Bounds.X || s.ime.Caret.Y < field.Bounds.Y {
		t.Fatalf("IME caret not mapped to native bounds: %+v", s.ime)
	}
	w, h, pix := conn.Capture()
	if w != 250 || h != 250 || len(pix) != 250*250*4 {
		t.Fatalf("native capture: %dx%d, %d bytes", w, h, len(pix))
	}
	var rt *engine
	for engine := range p.watchers {
		rt = engine
	}
	host := rt.host.(*previewHost)
	imageID := host.image.ID()
	config := p.Config()
	config.Scale = 2
	_ = p.SetConfig(config)
	conn.Event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if host.image.ID() == imageID || host.image.W != 400 || host.image.H != 400 {
		t.Fatal("DPI change reused an image's immutable texture dimensions")
	}
	s.w, s.h = 400, 800
	conn.Event(platform.SurfaceEvent{Kind: platform.SurfaceResize})
	conn.Event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	if s.pixelSize != image.Pt(800, 1600) {
		t.Fatal("resized native framebuffer was not used")
	}
	button = node(t, s.access, platform.RoleButton, "Press")
	if button.Bounds.X != 40 || button.Bounds.Y < 240 {
		t.Fatalf("accessibility did not follow new fit: %+v", button.Bounds)
	}
	field = node(t, s.access, platform.RoleTextField, "Editor")
	if !s.ime.Active || s.ime.Caret.X < field.Bounds.X || s.ime.Caret.X > field.Bounds.X+field.Bounds.W || s.ime.Caret.Y < field.Bounds.Y {
		t.Fatalf("IME caret did not follow resized fit: %+v, field %+v", s.ime, field.Bounds)
	}
	if rt.pointerY >= 0 || len(rt.hover) != 0 {
		t.Fatal("stationary pointer did not follow the resized viewport into its margin")
	}
	oldField := field.ID
	p.Reset()
	conn.Event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
	field = node(t, s.access, platform.RoleTextField, "Editor")
	if disposed != 1 || field.ID == oldField || s.ime.Active {
		t.Fatal("reset retained old accessibility nodes or active input methods")
	}
	conn.Detach()
	if disposed != 2 || len(p.watchers) != 0 {
		t.Fatal("native detach did not dispose sample and subscription")
	}
}
