//go:build !js

package mygo

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestPage(t *testing.T) {
	w := NewWindow(WindowOptions{Hidden: true, URL: "https://example.com/a", Page: PageOptions{
		PreloadScript: "window.preloaded = true", UserAgent: "agent", UserAgentSuffix: " MyApp/1.2 ", ZoomFactor: 1.5, DevTools: DevToolsEnabled,
	}})
	t.Cleanup(w.Destroy)
	fw := fb.Windows()[len(fb.Windows())-1]
	p := w.Page()
	if p == nil || p.Window() != w {
		t.Fatalf("Page %v", p)
	}
	o := fw.Opts
	preloaded := slices.ContainsFunc(o.UserScripts, func(s platform.UserScript) bool { return s.Source == "window.preloaded = true" })
	if o.UserAgent != "agent" || o.UserAgentSuffix != "MyApp/1.2" || o.Zoom != 1.5 || !o.DevTools || !preloaded {
		t.Errorf("page options: user agent %q, suffix %q, zoom %v, devtools %v, preloaded %v", o.UserAgent, o.UserAgentSuffix, o.Zoom, o.DevTools, preloaded)
	}
	if got := p.URL(); got != "https://example.com/a" {
		t.Errorf("URL %q", got)
	}
	if err := p.LoadURL("https://example.com/b"); err != nil || p.URL() != "https://example.com/b" {
		t.Errorf("LoadURL: %v, URL %q", err, p.URL())
	}
	if _, err := EvalAs[int]((*Page)(nil), "1"); !errors.Is(err, errNoPage) {
		t.Errorf("EvalAs of no page: %v", err)
	}
}

func TestCheckStorageID(t *testing.T) {
	for _, id := range []string{"", "a", "acct-a", "A.b_c-9", ".", "..", strings.Repeat("x", 64)} {
		if err := checkStorageID(id); err != nil {
			t.Errorf("StorageID %q: %v", id, err)
		}
	}
	for _, id := range []string{"a/b", `a\b`, "a b", "a:b", "é", "a\x00b", "a\n", "../x", strings.Repeat("x", 65), strings.Repeat("é", 40)} {
		err := checkStorageID(id)
		if !errors.Is(err, ErrInvalidStorageID) {
			t.Errorf("StorageID %q: %v, want ErrInvalidStorageID", id, err)
		} else if len(err.Error()) > 200 {
			t.Errorf("StorageID of %d bytes: the error is %d bytes long", len(id), len(err.Error()))
		}
	}
}

// TestPageStorageID gives the backend a window's StorageID, and the
// windows its page opens the same one, whatever their options say.
func TestPageStorageID(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{Hidden: true, Page: PageOptions{StorageID: "acct-a"}})
	if fw.Opts.StorageID != "acct-a" {
		t.Errorf("StorageID %q", fw.Opts.StorageID)
	}
	if _, other := testWindow(t, WindowOptions{Hidden: true}); other.Opts.StorageID != "" {
		t.Errorf("a window without a StorageID got %q", other.Opts.StorageID)
	}
	w.Page().SetWindowOpenHandler(func(WindowOpenRequest) *WindowOptions {
		return &WindowOptions{Hidden: true, Page: PageOptions{StorageID: "elsewhere"}}
	})
	onMain(func() { fw.H.NewWindow(platform.NewWindowRequest{URL: "https://example.com/"}) })
	wins := fb.Windows()
	if got := wins[len(wins)-1].Opts.StorageID; got != "acct-a" {
		t.Errorf("the window the page opened got StorageID %q, want its opener's", got)
	}

	defer func() {
		if err, _ := recover().(error); !errors.Is(err, ErrInvalidStorageID) {
			t.Errorf("NewWindow with an invalid StorageID panicked with %v", err)
		}
	}()
	NewWindow(WindowOptions{Hidden: true, Page: PageOptions{StorageID: "a/b"}})
	t.Error("NewWindow with an invalid StorageID returned")
}
