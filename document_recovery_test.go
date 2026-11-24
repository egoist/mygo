//go:build !js

package mygo

import (
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// recoveryDocs keeps the automatic restore from racing the explicit restores below.
func recoveryDocs(t *testing.T) *Documents[string] {
	t.Helper()
	docRegistry.Lock()
	launched := docRegistry.launched
	docRegistry.launched = false
	docRegistry.Unlock()
	defer func() {
		docRegistry.Lock()
		docRegistry.launched = launched
		docRegistry.Unlock()
	}()
	return testDocs(t, DocumentsOptions[string]{Autosave: AutosaveOff})
}

func writeRecoveryOrphan(t *testing.T, ds *Documents[string], name string, entries []recoveryEntry, copies map[string]string) string {
	t.Helper()
	root, err := ds.kind.recovery.root()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	index, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), index, 0o600); err != nil {
		t.Fatal(err)
	}
	for id, content := range copies {
		if err := os.WriteFile(filepath.Join(dir, id), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDocumentRecoveryKeepsSkippedCopy(t *testing.T) {
	ds := recoveryDocs(t)
	path := writeTemp(t, "already-open.txt", "on disk")
	if _, err := ds.Open(path); err != nil {
		t.Fatal(err)
	}
	stamp, _, err := statStamp(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := writeRecoveryOrphan(t, ds, "already-open", []recoveryEntry{{ID: "unsaved", Path: path, Stamp: stamp}}, map[string]string{"unsaved": "unsaved changes"})

	if n := ds.kind.restore(); n != 0 {
		t.Fatalf("restored %d documents, want none", n)
	}
	if got := readString(t, filepath.Join(dir, "unsaved")); got != "unsaved changes" {
		t.Fatalf("recovery copy = %q, want unsaved changes", got)
	}
}

func TestDocumentRecoveryKeepsCopyWhenBackupFails(t *testing.T) {
	ds := recoveryDocs(t)
	dir := writeRecoveryOrphan(t, ds, "backup-fails", []recoveryEntry{{ID: "unsaved", Name: "Untitled"}}, map[string]string{"unsaved": "unsaved changes"})
	create := ds.kind.create
	blocked := writeTemp(t, "user-data-is-a-file", "not a directory")
	ds.kind.create = func(content any, name string) (*docBase, error) {
		App.SetPath(PathUserData, blocked)
		return create(content, name)
	}
	t.Cleanup(func() { ds.kind.create = create })

	if n := ds.kind.restore(); n != 0 {
		t.Fatalf("restored %d documents without a recovery copy, want none", n)
	}
	if got := readString(t, filepath.Join(dir, "unsaved")); got != "unsaved changes" {
		t.Fatalf("recovery copy = %q, want unsaved changes", got)
	}
}

// TestDocumentRecoveryCanceledQuit cancels a quit after the user chose not
// to save: the documents stay open, and their changes in recovery copies,
// also when their windows close without asking, as when the OS ends the
// session.
func TestDocumentRecoveryCanceledQuit(t *testing.T) {
	copied := func(d *Document[string]) bool {
		r := d.kind.recovery
		r.mu.Lock()
		defer r.mu.Unlock()
		_, ok := r.entries[d.id]
		return ok
	}
	check := func(t *testing.T, docs ...*Document[string]) {
		t.Helper()
		for _, d := range docs {
			waitFor(t, "the recovery copy", func() bool { return copied(d) })
		}
		for _, d := range docs {
			onMain(d.Window().destroy)
		}
		docWork.Wait()
		for _, d := range docs {
			if !copied(d) {
				t.Errorf("%s lost its recovery copy with its window", d.Name())
			}
		}
	}

	t.Run("by a listener", func(t *testing.T) {
		ds := testDocs(t, DocumentsOptions[string]{Autosave: AutosaveOff, AskOnQuit: true})
		d := ds.New()
		d.Set("unsaved")
		if !d.backup() {
			t.Fatal("no recovery copy")
		}
		// Runs after the documents' listener, so it cancels the second quit,
		// once the changes were discarded.
		t.Cleanup(App.OnBeforeQuit(func(e *QuitEvent) { e.PreventDefault() }))
		quits := make(chan bool, 1)
		quitAgain = func() { quits <- App.prepareQuit() }
		t.Cleanup(func() { quitAgain = func() { App.Quit() } })
		answer("Don’t Save")
		onMainValue(fb.QuitRequested)
		if <-quits {
			t.Fatal("the app quits")
		}
		check(t, d)
	})

	t.Run("by the next document", func(t *testing.T) {
		if runtime.GOOS != "darwin" {
			t.Skip("only macOS asks about each document")
		}
		ds := testDocs(t, DocumentsOptions[string]{Autosave: AutosaveOff, AskOnQuit: true})
		a, b := ds.New(), ds.New()
		for _, d := range []*Document[string]{a, b} {
			d.Set("unsaved")
			if !d.backup() {
				t.Fatal("no recovery copy")
			}
		}
		answers := []string{"Review Changes…", "Don’t Save", "Cancel"}
		onMain(func() {
			fb.OnMessageBox = func(o *platform.MessageBoxOptions) platform.MessageBoxResult {
				label := answers[0]
				answers = answers[1:]
				return platform.MessageBoxResult{Response: slices.Index(o.Buttons, label)}
			}
		})
		onMainValue(fb.QuitRequested)
		waitFor(t, "the quit to be canceled", func() bool { return onMainValue(func() bool { return len(answers) == 0 && !docQuit.running }) })
		check(t, a, b)
	})
}

func TestDocumentRecoveryCleansOnlyRestoredEntries(t *testing.T) {
	ds := recoveryDocs(t)
	entries := []recoveryEntry{{ID: "good", Name: "Good"}, {ID: "bad", Name: "Bad"}}
	dir := writeRecoveryOrphan(t, ds, "partial", entries, map[string]string{"good": "good content", "bad": "bad content"})
	read := ds.kind.read
	ds.kind.read = func(r io.Reader) (any, error) {
		v, err := read(r)
		if err == nil && v == "bad content" {
			return nil, errors.New("cannot read bad content")
		}
		return v, err
	}

	if n := ds.kind.restore(); n != 1 {
		t.Fatalf("restored %d documents, want one", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "good")); !os.IsNotExist(err) {
		t.Fatalf("the restored copy was not removed: %v", err)
	}
	if got := readString(t, filepath.Join(dir, "bad")); got != "bad content" {
		t.Fatalf("failed recovery copy = %q, want bad content", got)
	}
	var remaining []recoveryEntry
	if err := json.Unmarshal([]byte(readString(t, filepath.Join(dir, "index.json"))), &remaining); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != "bad" {
		t.Fatalf("remaining index = %+v, want only bad", remaining)
	}

	ds.kind.read = read
	if n := ds.kind.restore(); n != 1 {
		t.Fatalf("restored %d documents on the second attempt, want one", n)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the fully restored session remains: %v", err)
	}
}

func TestDocumentRecoveryRemovesSessionsWithoutEntries(t *testing.T) {
	for _, missingIndex := range []bool{false, true} {
		name := "empty-index"
		if missingIndex {
			name = "missing-index"
		}
		t.Run(name, func(t *testing.T) {
			ds := recoveryDocs(t)
			var copies map[string]string
			if missingIndex {
				copies = map[string]string{"unindexed": "unsaved changes"}
			}
			dir := writeRecoveryOrphan(t, ds, name, []recoveryEntry{}, copies)
			if missingIndex {
				if err := os.Remove(filepath.Join(dir, "index.json")); err != nil {
					t.Fatal(err)
				}
			}

			if n := ds.kind.restore(); n != 0 {
				t.Fatalf("restored %d documents, want none", n)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("the session without entries remains: %v", err)
			}
		})
	}
}

// TestDocumentRecoveryFollowsRename moves the file of a document with a
// recovery copy: the copy is of the file where it is now, whether the app
// crashes or quits then.
func TestDocumentRecoveryFollowsRename(t *testing.T) {
	for name, move := range map[string]func(d *Document[string], path string) error{
		"renamed": func(d *Document[string], path string) error { return d.Rename(filepath.Base(path)) },
		"by another app": func(d *Document[string], path string) error {
			if err := os.Rename(d.Path(), path); err != nil {
				return err
			}
			d.checkDisk()
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			ds := recoveryDocs(t)
			old := writeTemp(t, "old.txt", "one")
			path := filepath.Join(filepath.Dir(old), "new.txt")
			d, err := ds.Open(old)
			if err != nil {
				t.Fatal(err)
			}
			d.Set("unsaved")
			if !d.backup() {
				t.Fatal("no recovery copy")
			}
			if err := move(d, path); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the recovery copy to follow", func() bool {
				r := ds.kind.recovery
				r.mu.Lock()
				defer r.mu.Unlock()
				return r.entries[d.id].Path == path
			})

			// The next launch, once this session's lock is gone.
			onMain(d.Window().destroy)
			docWork.Wait()
			ds.kind.recovery.close()
			ds.kind.restore()
			list := ds.Documents()
			if len(list) != 1 {
				t.Fatalf("restored %d documents", len(list))
			}
			r := list[0]
			if r.Path() != path || r.Content() != "unsaved" || r.State().Deleted || r.HasConflict() {
				t.Errorf("restored %s with %q: %+v", r.Path(), r.Content(), r.State())
			}
			if err := r.Save(); err != nil {
				t.Error(err)
			}
			if got := readString(t, path); got != "unsaved" {
				t.Errorf("new.txt has %q", got)
			}
			if _, err := os.Stat(old); !os.IsNotExist(err) {
				t.Errorf("old.txt is back: %v", err)
			}
		})
	}
}

// An index that cannot be read or parsed is not one that holds nothing:
// the copies it names are the user's unsaved work, and removing the
// session would drop it. They stay for a later launch.
func TestDocumentRecoveryKeepsSessionWhoseIndexCannotBeRead(t *testing.T) {
	cases := []struct {
		name   string
		damage func(dir string)
		skip   string
	}{
		{"unreadable", func(dir string) {
			if err := os.Chmod(filepath.Join(dir, "index.json"), 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(filepath.Join(dir, "index.json"), 0o600) })
		}, "root reads every file; a mode is no bar on Windows"},
		{"unparseable", func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.skip != "" && (runtime.GOOS == "windows" || os.Geteuid() == 0) {
				t.Skip(c.skip)
			}
			ds := recoveryDocs(t)
			dir := writeRecoveryOrphan(t, ds, c.name, []recoveryEntry{{ID: "unsaved", Name: "Untitled"}}, map[string]string{"unsaved": "unsaved changes"})
			c.damage(dir)

			if n := ds.kind.restore(); n != 0 {
				t.Fatalf("restored %d documents, want none", n)
			}
			if got := readString(t, filepath.Join(dir, "unsaved")); got != "unsaved changes" {
				t.Fatalf("recovery copy = %q, want unsaved changes", got)
			}
		})
	}
}

// A restored document whose file cannot be looked at is not a deleted
// one: the look failed, it did not answer that the file is gone. The
// document says neither deleted nor in conflict, and the next look of
// the disk (a focus, a save) tells what it really is, as checkDisk
// does.
func TestDocumentRecoveryDoesNotSayDeletedWhenItCannotLook(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a directory with no permission needs a user who is not root")
	}
	ds := recoveryDocs(t)
	outer := t.TempDir()
	if err := os.Chmod(outer, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(outer, 0o700) })
	path := filepath.Join(outer, "doc.txt")
	writeRecoveryOrphan(t, ds, "cannot-look",
		[]recoveryEntry{{ID: "unsaved", Path: path, Stamp: fileStamp{ModTime: 1, Size: 4}}},
		map[string]string{"unsaved": "saved work"})

	if n := ds.kind.restore(); n != 1 {
		t.Fatalf("restored %d documents, want one", n)
	}
	for _, b := range allDocuments() {
		if b.path != path {
			continue
		}
		if st := b.State(); st.Deleted {
			t.Errorf("the document whose file cannot be looked at says deleted")
		} else if st.Conflict {
			t.Errorf("the document whose file cannot be looked at says in conflict")
		}
		return
	}
	t.Fatalf("the document of %s was not restored", path)
}
