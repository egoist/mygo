//go:build darwin

package e2e

import (
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
	"github.com/egoist/mygo/ui"
)

// TestDialogFromClick shows each kind of dialog from a button's Clicked,
// as the frame that reports the click runs: the dialog shows and waits
// until it is dismissed. A frame drawn as AppKit displayed the view ran in
// its display cycle, where the panel never showed and runModal returned at
// once, and a sheet never showed nor ended (#883). The window draws again
// once the dialog is gone.
func TestDialogFromClick(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		show func(parent *mygo.Window)
	}{
		{"open", func(*mygo.Window) {
			mygo.Dialog.Open(mygo.OpenDialogOptions{DefaultPath: dir, Directory: true, Multiple: true})
		}},
		{"save", func(*mygo.Window) {
			mygo.Dialog.Save(mygo.SaveDialogOptions{DefaultPath: filepath.Join(dir, "note.txt")})
		}},
		{"message", func(*mygo.Window) {
			mygo.Dialog.Message(mygo.MessageOptions{Message: "From a click", Buttons: []string{"OK", "Cancel"}})
		}},
		{"open sheet", func(w *mygo.Window) {
			mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w, DefaultPath: dir})
		}},
		{"message sheet", func(w *mygo.Window) {
			mygo.Dialog.Message(mygo.MessageOptions{Parent: w, Message: "From a click"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w *mygo.Window // main thread only
			var frames atomic.Int32
			var clicked, returned atomic.Bool
			view := func(c *ui.Context) {
				frames.Add(1)
				if ui.Button(c, "Show").Width(120).Height(30).Clicked() {
					clicked.Store(true)
					tc.show(w)
					returned.Store(true)
				}
			}
			var handle uintptr
			mygo.RunOnMain(func() {
				w = mygo.NewWindow(mygo.WindowOptions{Title: "Dialog from a click", Width: 300, Height: 200, Content: ui.View(view)})
				handle = w.NativeHandle()
			})
			win := w
			t.Cleanup(win.Destroy)
			eventually(t, "a frame", func() bool { return frames.Load() > 0 })
			// Frames pause, so that the click's frame is one after a pause,
			// which AppKit drew as it displayed the view.
			time.Sleep(200 * time.Millisecond)
			// The button lays out at the top-left of the window.
			click(win, 60, 15)
			eventually(t, "the click", clicked.Load)
			eventually(t, "the dialog to show", func() bool {
				var running, shows bool
				mygo.RunOnMain(func() { running, shows = darwin.TestModal(handle) })
				return running && shows
			})
			if returned.Load() {
				t.Fatal("the dialog returned before it was dismissed")
			}
			var canceled bool
			mygo.RunOnMain(func() { canceled = darwin.TestCancelModal(handle) })
			if !canceled {
				t.Fatal("no dialog to dismiss")
			}
			eventually(t, "the dialog to return", returned.Load)
			n := frames.Load()
			win.Invalidate()
			eventually(t, "a frame after the dialog", func() bool { return frames.Load() > n })
		})
	}
}

// TestMessageButtonRoles shows message sheets. Escape presses the cancel
// button also when it is the default button, which alone has Return, and
// Cmd+. presses it too; a DefaultButton of -1 gives Return to no button,
// and DestructiveButtons marks theirs.
func TestMessageButtonRoles(t *testing.T) {
	cases := []struct {
		name        string
		opts        mygo.MessageOptions
		code        uint16
		chars       string
		cmd         bool
		keys        []string
		destructive []bool
		cancel      int
	}{
		{"Escape on the default", mygo.MessageOptions{Buttons: []string{"Continue Editing", "Discard"}, DestructiveButtons: []int{1}},
			53, "\x1b", false, []string{"\r", ""}, []bool{false, true}, 0},
		{"Cmd+. without a default", mygo.MessageOptions{Buttons: []string{"Delete", "Cancel"}, DefaultButton: -1, DestructiveButtons: []int{0}},
			47, ".", true, []string{"", ""}, []bool{true, false}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w *mygo.Window
			mygo.RunOnMain(func() {
				w = mygo.NewWindow(mygo.WindowOptions{Title: "Message keys", Width: 300, Height: 200, Content: ui.View(func(*ui.Context) {})})
			})
			t.Cleanup(w.Destroy)
			eventually(t, "the window to show", w.IsVisible)
			answered := make(chan mygo.MessageResult, 1)
			go func() {
				opts := tc.opts
				opts.Parent, opts.Message = w, "Message keys"
				res, err := mygo.Dialog.Message(opts)
				if err != nil {
					t.Error(err)
				}
				answered <- res
			}()
			handle := w.NativeHandle()
			var keys []string
			var destructive []bool
			eventually(t, "the sheet", func() bool {
				var ok bool
				mygo.RunOnMain(func() { keys, destructive, ok = darwin.TestSheetButtonRoles(handle) })
				return ok && len(keys) == len(tc.keys)
			})
			if !slices.Equal(keys, tc.keys) || !slices.Equal(destructive, tc.destructive) {
				t.Errorf("key equivalents %q, destructive %v; want %q, %v", keys, destructive, tc.keys, tc.destructive)
			}
			mygo.RunOnMain(func() { darwin.TestSheetKey(handle, tc.code, tc.chars, tc.cmd) })
			eventually(t, "the sheet to close", func() bool {
				var ok bool
				mygo.RunOnMain(func() { _, _, ok = darwin.TestSheetButtonRoles(handle) })
				return !ok
			})
			if res := <-answered; res.Button != tc.cancel {
				t.Errorf("the key pressed button %d, want %d", res.Button, tc.cancel)
			}
		})
	}
}
