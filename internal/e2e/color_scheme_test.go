//go:build !js

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// A page follows the appearance the app sets, as its prefers-color-scheme,
// also when it changes, and so does Theme.IsDark.
func TestPageColorScheme(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Color scheme", Width: 300, Height: 200})
	w.Page().LoadHTML(`<meta name="color-scheme" content="light dark"><p>scheme</p>`, "")
	waitFor(t, w, "document.readyState === 'complete'")
	if os.Getenv("MYGO_E2E_DARK_THEME") != "" && !mygo.Theme.IsDark() {
		t.Fatal("GTK's theme is not Adwaita-dark")
	}
	defer mygo.Theme.SetSource(mygo.Theme.Source())
	for _, source := range []mygo.ThemeSource{mygo.ThemeLight, mygo.ThemeDark, mygo.ThemeLight} {
		mygo.Theme.SetSource(source)
		dark := source == mygo.ThemeDark
		waitFor(t, w, fmt.Sprintf("matchMedia('(prefers-color-scheme: dark)').matches === %v", dark))
		if got := mygo.Theme.IsDark(); got != dark {
			t.Errorf("%s: Theme.IsDark() = %v", source, got)
		}
	}
}

// On a desktop whose GTK theme is named for its dark variant, as Ubuntu's
// Yaru-dark in dark mode, ThemeLight makes pages light too (#224):
// WebKitGTK takes such a theme for dark whatever
// gtk-application-prefer-dark-theme says.
func TestPageColorSchemeDarkTheme(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("GTK's theme is Linux's")
	}
	if os.Getenv("MYGO_E2E_DARK_THEME") != "" {
		t.Skip("in the helper process")
	}
	config := t.TempDir()
	if err := os.MkdirAll(filepath.Join(config, "gtk-3.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "gtk-3.0", "settings.ini"), []byte("[Settings]\ngtk-theme-name=Adwaita-dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The helper's waits have their guard, after which it fails and says
	// what it waited for.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPageColorScheme$", "-test.v")
	cmd.Env = append(os.Environ(), "MYGO_E2E_DARK_THEME=1", "XDG_CONFIG_HOME="+config)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("%v:\n%s", err, out)
	}
}

// Every page follows the appearance the app sets, whichever way it is shown:
// a window's, a hidden window's, a web view in a window of native UI, and a
// window created after the appearance was set.
func TestColorSchemeReachesEveryWebView(t *testing.T) {
	const page = `<meta name="color-scheme" content="light dark"><p id="ready">scheme</p>`
	shown := newWindow(t, mygo.WindowOptions{Title: "Color scheme", Width: 300, Height: 200})
	hidden := newWindow(t, mygo.WindowOptions{Hidden: true})
	var view *mygo.WebView
	var frames atomic.Int32
	native := newWindow(t, mygo.WindowOptions{Title: "Color scheme view", Width: 300, Height: 200, Content: ui.View(func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if view != nil {
				ui.WebView(c, view).Grow(1)
			}
		})
	})})
	eventually(t, "a frame", func() bool { return frames.Load() > 0 })
	v, err := native.NewWebView(mygo.WebViewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	native.Update(func() { view = v })
	pages := map[string]*mygo.Page{"window": shown.Page(), "hidden window": hidden.Page(), "web view": v.Page()}
	for _, p := range pages {
		p.LoadHTML(page, "")
	}
	for name, p := range pages {
		t.Log(name)
		waitForPage(t, p, "document.getElementById('ready')")
	}

	defer mygo.Theme.SetSource(mygo.Theme.Source())
	isDark := func(dark bool) string {
		return fmt.Sprintf("matchMedia('(prefers-color-scheme: dark)').matches === %v", dark)
	}
	for _, source := range []mygo.ThemeSource{mygo.ThemeLight, mygo.ThemeDark, mygo.ThemeLight, mygo.ThemeDark} {
		dark := source == mygo.ThemeDark
		mygo.Theme.SetSource(source)
		for name, p := range pages {
			t.Logf("%s: %s", source, name)
			waitForPage(t, p, isDark(dark))
		}
		late := newWindow(t, mygo.WindowOptions{Hidden: true})
		late.Page().LoadHTML(page, "")
		waitForPage(t, late.Page(), "document.getElementById('ready') && "+isDark(dark))
	}
}

// OnUpdated fires when SetSource changes the effective appearance, with
// IsDark already the new one.
func TestThemeUpdatedBySetSource(t *testing.T) {
	var updates atomic.Int32
	var dark atomic.Bool
	off := mygo.Theme.OnUpdated(func() {
		dark.Store(mygo.Theme.IsDark())
		updates.Add(1)
	})
	defer off()
	defer mygo.Theme.SetSource(mygo.Theme.Source())
	t.Logf("source %q, dark: %v", mygo.Theme.Source(), mygo.Theme.IsDark())
	for _, source := range []mygo.ThemeSource{mygo.ThemeLight, mygo.ThemeDark, mygo.ThemeLight, mygo.ThemeDark} {
		want := source == mygo.ThemeDark
		was, before := mygo.Theme.IsDark(), updates.Load()
		mygo.Theme.SetSource(source)
		if was == want {
			continue // the system's appearance is already what it sets
		}
		eventually(t, fmt.Sprintf("OnUpdated for %s", source), func() bool { return updates.Load() > before })
		if dark.Load() != want {
			t.Errorf("%s: Theme.IsDark() in OnUpdated = %v", source, dark.Load())
		}
	}
}
