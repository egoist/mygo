// WebView is a native file browser beside an embedded web view: choose a
// file on the left, and the preview on the right loads it in a system web
// view drawn inside the window MyGo draws.
//
//	go run ./examples/webview [dir]
//
// With no argument it browses the bundled sample pages.
package main

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// browser is the state the window shows.
type browser struct {
	dir    string
	files  []string
	list   ui.ListState
	picked int
}

func (b *browser) view(c *ui.Context) {
	b.list.Selected = &b.picked
	b.list.Key = func(i int) any { return i }
	b.list.Label = func(i int) string { return b.files[i] }
	t := c.Theme()
	ui.Row(c).Fill().Gap(0).Children(func() {
		ui.Column(c).Width(240).Gap(0).Background(t.Background).Children(func() {
			ui.Text(c, filepath.Base(b.dir)).Padding(12, 12, 4).FontSize(12).TextColor(t.TextMuted)
			ui.List(c.Key("files"), &b.list, len(b.files)).Fill().Rows(func(r ui.ListRow) {
				ui.Text(r.Context, b.files[r.Index]).SingleLine().Padding(6, 12)
			})
		})
		ui.Box(c).Fill().Width(1).Background(t.Border)
		wv := ui.WebView(c.Key("preview")).Fill()
		if u := b.url(); u != "" {
			wv = wv.LoadURL(u)
		}
	})
}

// url is the file:// address of the picked page, or empty for none.
func (b *browser) url() string {
	if b.picked < 0 || b.picked >= len(b.files) {
		return ""
	}
	return "file://" + filepath.Join(b.dir, b.files[b.picked])
}

func main() {
	dir := os.Args[len(os.Args)-1]
	if len(os.Args) <= 1 {
		dir = ""
	}
	b := &browser{dir: resolveDir(dir), picked: -1}
	b.files = listHTML(b.dir)
	if len(b.files) > 0 {
		b.picked = 0
	}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:     "WebView",
			Width:     900,
			Height:    560,
			MinWidth:  640,
			MinHeight: 400,
			Content:   ui.View(b.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// resolveDir turns the argument (or the bundled sample pages, when there is
// none) into an absolute directory.
func resolveDir(dir string) string {
	if dir != "" {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(dir)
			return abs
		}
	}
	cands := []string{
		"sample",
		filepath.Join("examples", "webview", "sample"),
	}
	if exec, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exec), "sample"))
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	abs, _ := filepath.Abs(".")
	return abs
}

// listHTML is the sorted .html files of dir.
func listHTML(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".html") || strings.HasSuffix(n, ".htm") {
			files = append(files, n)
		}
	}
	slices.Sort(files)
	return files
}
