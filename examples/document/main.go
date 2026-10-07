// Document is a native text editor with coordinated save/revert, recent
// documents, reopening, printing and PDF export. It needs Go alone:
//
//	go run ./examples/document
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type editor struct {
	doc     *mygo.Document[string]
	files   *mygo.Documents[string]
	win     *mygo.Window
	message string // main thread only
}

func (e *editor) run(action func() error) {
	// Defer dialogs until the UI frame that asked for them has finished.
	go func() {
		err := action()
		e.win.Update(func() {
			e.message = ""
			if err != nil && !errors.Is(err, mygo.ErrDocumentCanceled) && !errors.Is(err, mygo.ErrPrintCanceled) {
				e.message = err.Error()
			}
		})
	}()
}

func (e *editor) view(c *ui.Context) {
	state := e.doc.State()
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		e.run(e.doc.Save)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyS) {
		e.run(e.doc.SaveAs)
	}
	if c.Shortcut(ui.Cmd, ui.KeyO) {
		e.run(func() error { _, err := e.files.OpenDialog(e.win); return err })
	}
	if c.Shortcut(ui.Cmd, ui.KeyN) {
		go e.files.New()
	}
	if c.Shortcut(ui.Cmd, ui.KeyP) {
		e.run(func() error { return e.win.Print(e.printable(), mygo.PrintOptions{Title: state.Title}) })
	}
	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		ui.Toolbar(c, func() {
			if ui.Button(c, "New").Clicked() {
				go e.files.New()
			}
			if ui.Button(c, "Open…").Clicked() {
				e.run(func() error { _, err := e.files.OpenDialog(e.win); return err })
			}
			ui.MenuButton(c, "Recent", func(m *ui.Menu) {
				for _, path := range e.files.Recent() {
					if m.Item(path).Chosen() {
						e.run(func() error { _, err := e.files.Open(path); return err })
					}
				}
				m.Separator()
				if m.Item("Reopen closed document").Chosen() {
					e.run(func() error { _, err := e.files.ReopenClosed(); return err })
				}
				if m.Item("Clear recent documents").Chosen() {
					e.files.ForgetRecent("")
				}
			})
			if ui.PrimaryButton(c, "Save").Disabled(state.Busy).Clicked() {
				e.run(e.doc.Save)
			}
			if ui.Button(c, "Save as…").Disabled(state.Busy).Clicked() {
				e.run(e.doc.SaveAs)
			}
			if ui.Button(c, "Revert").Disabled(state.Busy || state.Path == "").Clicked() {
				e.run(func() error {
					answer, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: e.win, Type: mygo.MessageWarning,
						Message: "Reload from disk and discard edits?", Buttons: []string{"Revert", "Cancel"}, CancelButton: 1})
					if err != nil {
						return err
					}
					if answer.Button != 0 {
						return mygo.ErrDocumentCanceled
					}
					return e.doc.Revert()
				})
			}
			ui.MenuButton(c, "Print", func(m *ui.Menu) {
				if m.Item("Print…").Chosen() {
					e.run(func() error { return e.win.Print(e.printable(), mygo.PrintOptions{Title: state.Title}) })
				}
				if m.Item("Export PDF…").Chosen() {
					e.run(e.exportPDF)
				}
			})
		}).Label("Document actions")
		// The controller owns the text. Commit only edits reported by the UI.
		value := e.doc.Value()
		if ui.TextArea(c, &value).FillWidth().Grow(1).Font("monospace").Label("Document text").Changed() {
			e.doc.SetValue(value)
		}
		status := state.Path
		if status == "" {
			status = "Untitled — choose a path with Save"
		}
		if state.Dirty {
			status += " · Unsaved changes"
		}
		if state.Busy {
			status += " · Working…"
		}
		ui.Text(c, status).FontSize(12).TextColor(c.Theme().TextMuted)
		if e.message != "" {
			ui.Text(c, e.message).TextColor(c.Theme().Danger)
		}
	})
}

func (e *editor) printable() *ui.PrintContent {
	// Capture once: edits made while the print dialog is open affect the
	// editor's dirty state, while the print job keeps this revision.
	value, title := e.doc.Value(), e.doc.State().Title
	return ui.PrintPages(func(l ui.PrintLayout) []func(*ui.Context) {
		lines := wrapLines(value, l.Width)
		perPage := max(1, int((l.Height-60)/16))
		var pages []func(*ui.Context)
		for start := 0; start < len(lines); start += perPage {
			body := strings.Join(lines[start:min(start+perPage, len(lines))], "\n")
			number := len(pages) + 1
			pages = append(pages, func(c *ui.Context) {
				ui.Column(c).Fill().Gap(8).Children(func() {
					ui.Text(c, title).FontSize(14).Bold()
					ui.Text(c, body).Font("monospace").FontSize(11).FixedLineHeight(16).NoWrap().Grow(1)
					ui.Textf(c, "Page %d", number).FontSize(10).TextColor(c.Theme().TextMuted)
				})
			})
		}
		return pages
	})
}

// wrapLines breaks logical text at shaped cluster boundaries, preserving
// combining characters and ligatures. Printing then groups these lines by
// the page's available height. Applications can choose their own page breaks.
func wrapLines(value string, width float32) []string {
	var lines []string
	font := ui.Font{Family: "monospace", Size: 11}
	for _, paragraph := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		runes := []rune(paragraph)
		advances := make(map[int]float32)
		for _, g := range ui.Shape(paragraph, font) {
			advances[g.Cluster] += g.Advance
		}
		var clusters []int
		for cluster := range advances {
			clusters = append(clusters, cluster)
		}
		slices.Sort(clusters)
		start, used := 0, float32(0)
		for _, cluster := range clusters {
			if cluster > start && used+advances[cluster] > width {
				lines = append(lines, string(runes[start:cluster]))
				start, used = cluster, 0
			}
			used += advances[cluster]
		}
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

func (e *editor) exportPDF() error {
	title := e.doc.State().Title
	name := strings.TrimSuffix(title, filepath.Ext(title)) + ".pdf"
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: e.win, Title: "Export PDF", DefaultPath: name,
		Filters: []mygo.FileFilter{{Name: "PDF", Extensions: []string{"pdf"}}}})
	if err != nil {
		return err
	}
	if path == "" {
		return mygo.ErrDocumentCanceled
	}
	pdf, err := e.win.PrintToPDF(e.printable(), mygo.PrintOptions{Title: title})
	if err != nil {
		return err
	}
	return atomicWrite(path, string(pdf))
}

func atomicWrite(path, value string) error {
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mygo-document-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.WriteString(value)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func main() {
	mygo.App.SetName("MyGo Documents")
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	var files *mygo.Documents[string]
	files = mygo.NewDocuments(mygo.DocumentsOptions[string]{
		Read:            func(path string) (string, error) { b, err := os.ReadFile(path); return string(b), err },
		Write:           atomicWrite,
		HandleOpenFiles: true,
		OpenDialog:      mygo.OpenDialogOptions{Multiple: true, Filters: []mygo.FileFilter{{Name: "Text files", Extensions: []string{"txt", "md"}}}},
		SaveDialog:      mygo.SaveDialogOptions{DefaultPath: "Untitled.txt", Filters: []mygo.FileFilter{{Name: "Text", Extensions: []string{"txt"}}}},
		OpenWindow: func(d *mygo.Document[string]) *mygo.Window {
			e := &editor{doc: d, files: files}
			e.win = mygo.NewWindow(mygo.WindowOptions{Title: d.State().Title, Width: 900, Height: 650, Content: ui.View(e.view)})
			return e.win
		},
		OnError: func(err error) { log.Print(err) },
	})
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu}}))
	if runtime.GOOS == "darwin" {
		mygo.App.OnWindowAllClosed(func() {})
		mygo.App.OnActivate(func(visible bool) {
			if !visible {
				files.New()
			}
		})
	}
	mygo.App.WhenReady(func() {
		restored, err := files.Restore()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		if len(restored) == 0 {
			files.New()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
