package mygo

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/ui"
)

func TestLocaleOverrideBeforeRun(t *testing.T) {
	a := &Application{}
	a.SetLocale("it-IT")
	if got := a.Locale(); got != "it-IT" {
		t.Fatalf("pre-run locale %q", got)
	}
	a.SetLocale("")
	if got := a.Locale(); got != "en-US" {
		t.Fatalf("OS fallback %q", got)
	}
}

func TestNativeLocaleAppWindowViewPrecedence(t *testing.T) {
	App.mu.Lock()
	previous := App.locale
	App.mu.Unlock()
	App.SetLocale("de-DE")
	t.Cleanup(func() { App.SetLocale(previous) })
	var tag, childTag string
	w, _, surface := contentWindow(t, func(c *ui.Context) {
		tag = c.Locale().Tag()
		c.WithLocale(ui.NewLocale("ja-JP"), func() { childTag = c.Locale().Tag(); ui.Text(c, "Developer content") })
		if c.Locale().Tag() != tag {
			t.Error("view override escaped its scope")
		}
	})
	read := func() string { return onMainValue(func() string { return tag }) }
	if w.Locale() != "de-DE" || read() != "de-DE" || onMainValue(func() string { return childTag }) != "ja-JP" {
		t.Fatal("app/view inheritance failed")
	}
	// The public setters below are called off the main thread by go test.
	w.SetLocale("fr-FR")
	onMain(func() { surface.Frame() })
	if w.Locale() != "fr-FR" || read() != "fr-FR" {
		t.Fatal("window override failed")
	}
	App.SetLocale("en-GB")
	onMain(func() { surface.Frame() })
	if read() != "fr-FR" {
		t.Fatal("app setting replaced window override")
	}
	w.SetLocale("")
	onMain(func() { surface.Frame() })
	if w.Locale() != "en-GB" || read() != "en-GB" {
		t.Fatal("clearing window override failed")
	}
	App.SetLocale("")
	onMain(func() { surface.Frame() })
	if w.Locale() != "en-US" || read() != "en-US" {
		t.Fatal("OS default not restored")
	}
	w.Destroy()
	w.SetLocale("ar-EG") // no native operation after destruction
	if w.Locale() != "en-US" {
		t.Fatal("destroyed window changed locale")
	}
}

func TestNativeLocaleWindowOptions(t *testing.T) {
	var tag string
	w := NewWindow(WindowOptions{Locale: "fr-CA", Width: 300, Height: 200, Content: ui.View(func(c *ui.Context) { tag = c.Locale().Tag() })})
	t.Cleanup(w.Destroy)
	surface := fb.Windows()[len(fb.Windows())-1].FakeSurface()
	onMain(func() { surface.Send(platform.SurfaceEvent{Kind: platform.SurfaceFrame}) })
	if w.Locale() != "fr-CA" || onMainValue(func() string { return tag }) != "fr-CA" {
		t.Fatalf("window option locale %q", w.Locale())
	}
}
