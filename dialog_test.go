//go:build !js

package mygo

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestDialogFiltersByType passes a filter's MIME types, wildcards too, to
// the system's picker with its extensions.
func TestDialogFiltersByType(t *testing.T) {
	var got []platform.FileFilter
	onMain(func() {
		fb.OnOpenDialog = func(o *platform.OpenDialogOptions) []string {
			got = o.Filters
			return nil
		}
	})
	defer onMain(func() { fb.OnOpenDialog = nil })
	Dialog.Open(OpenDialogOptions{Filters: []FileFilter{
		{Name: "Images", MIMETypes: []string{"image/*"}},
		{Name: "Documents", Extensions: []string{"md"}, MIMETypes: []string{"application/pdf"}},
	}})
	if len(got) != 2 || got[0].Name != "Images" || !slices.Equal(got[0].MIMETypes, []string{"image/*"}) ||
		!slices.Equal(got[1].Extensions, []string{"md"}) || !slices.Equal(got[1].MIMETypes, []string{"application/pdf"}) {
		t.Errorf("the picker gets %+v", got)
	}
}

// TestDialogSaveCopy copies a file to the path the save dialog chooses,
// suggests the given name, and reports cancellations and I/O errors.
func TestDialogSaveCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "export.txt")
	want := []byte("exported bytes")
	if err := os.WriteFile(src, want, 0o644); err != nil {
		t.Fatal(err)
	}

	var defaultPath string
	var chosen string
	onMain(func() {
		fb.OnSaveDialog = func(o *platform.SaveDialogOptions) string {
			defaultPath = o.DefaultPath
			return chosen
		}
	})
	t.Cleanup(func() { onMain(func() { fb.OnSaveDialog = nil }) })

	dst := filepath.Join(dir, "copy.txt")
	onMain(func() { chosen = dst })
	got, err := Dialog.SaveCopy(src, "suggested.txt")
	if err != nil {
		t.Fatalf("SaveCopy: %v", err)
	}
	if got != dst {
		t.Errorf("SaveCopy returned %q, want %q", got, dst)
	}
	if defaultPath != "suggested.txt" {
		t.Errorf("save dialog default path = %q, want %q", defaultPath, "suggested.txt")
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if string(data) != string(want) {
		t.Errorf("copy holds %q, want %q", data, want)
	}

	// Canceling the dialog returns empty without reading the source.
	onMain(func() { chosen = "" })
	got, err = Dialog.SaveCopy(filepath.Join(dir, "missing.txt"), "report.txt")
	if err != nil || got != "" {
		t.Errorf("canceled SaveCopy = (%q, %v), want (\"\", nil)", got, err)
	}

	// A missing source fails and leaves no destination file behind.
	missingDst := filepath.Join(dir, "never.txt")
	onMain(func() { chosen = missingDst })
	if got, err := Dialog.SaveCopy(filepath.Join(dir, "missing.txt"), "report.txt"); err == nil || got != "" {
		t.Errorf("SaveCopy of missing source = (%q, %v), want error", got, err)
	}
	if _, err := os.Stat(missingDst); !os.IsNotExist(err) {
		t.Errorf("destination %q should not exist after source error", missingDst)
	}

	// An unwritable destination returns an error.
	badDst := filepath.Join(dir, "no-such-dir", "copy.txt")
	onMain(func() { chosen = badDst })
	if got, err := Dialog.SaveCopy(src, "report.txt"); err == nil || got != "" {
		t.Errorf("SaveCopy to missing directory = (%q, %v), want error", got, err)
	}
}

// TestDialogMessageCancel infers the cancel button from "Cancel" or "No"
// when unset, keeps explicit choices including -1, and maps out-of-range
// dismissals back to it.
func TestDialogMessageCancel(t *testing.T) {
	var seen platform.MessageBoxOptions
	var reply int
	onMain(func() {
		fb.OnMessageBox = func(o *platform.MessageBoxOptions) platform.MessageBoxResult {
			seen = *o
			return platform.MessageBoxResult{Response: reply}
		}
	})
	t.Cleanup(func() { onMain(func() { fb.OnMessageBox = nil }) })

	for _, tc := range []struct {
		name         string
		opts         MessageOptions
		reply        int
		wantCancelID int
		wantButton   int
	}{
		{
			name:         "infers Cancel",
			opts:         MessageOptions{Buttons: []string{"Save", "Discard", "Cancel"}},
			reply:        99,
			wantCancelID: 2,
			wantButton:   2,
		},
		{
			name:         "infers No case-insensitively",
			opts:         MessageOptions{Buttons: []string{"Yes", "no"}},
			reply:        -1,
			wantCancelID: 1,
			wantButton:   1,
		},
		{
			name:         "falls back to first button when none match",
			opts:         MessageOptions{Buttons: []string{"Delete", "Keep"}},
			reply:        -1,
			wantCancelID: 0,
			wantButton:   0,
		},
		{
			name:         "preserves explicit -1",
			opts:         MessageOptions{Buttons: []string{"Save", "Cancel"}, CancelButton: -1},
			reply:        -1,
			wantCancelID: -1,
			wantButton:   -1,
		},
		{
			name:         "preserves explicit index",
			opts:         MessageOptions{Buttons: []string{"Save", "Later", "Cancel"}, CancelButton: 1},
			reply:        5,
			wantCancelID: 1,
			wantButton:   1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			onMain(func() { reply = tc.reply })
			res, err := Dialog.Message(tc.opts)
			if err != nil {
				t.Fatalf("Dialog.Message: %v", err)
			}
			if seen.CancelID != tc.wantCancelID {
				t.Errorf("CancelID = %d, want %d", seen.CancelID, tc.wantCancelID)
			}
			if res.Button != tc.wantButton {
				t.Errorf("Button = %d, want %d", res.Button, tc.wantButton)
			}
		})
	}
}
