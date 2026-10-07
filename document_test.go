package mygo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func testDocuments(t *testing.T, opts DocumentsOptions[string]) *Documents[string] {
	t.Helper()
	if opts.Read == nil {
		opts.Read = func(p string) (string, error) { b, err := os.ReadFile(p); return string(b), err }
	}
	if opts.Write == nil {
		opts.Write = func(p, s string) error { return os.WriteFile(p, []byte(s), 0o600) }
	}
	if opts.StatePath == "" {
		opts.MemoryOnly = true
	}
	if opts.OnError == nil {
		opts.OnError = func(error) {}
	}
	m := NewDocuments(opts)
	t.Cleanup(func() {
		for _, d := range m.OpenDocuments() {
			if w := d.Window(); w != nil {
				w.Destroy()
			} else {
				d.SetDirty(false)
				_ = d.Close()
			}
		}
		if err := m.Dispose(); err != nil {
			t.Error(err)
		}
	})
	return m
}

func documentFile(t *testing.T, dir, name, value string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := documentPath(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDocumentSaveAndRevert(t *testing.T) {
	dir := t.TempDir()
	fail := errors.New("codec failed")
	var writeErr error
	writes := 0
	m := testDocuments(t, DocumentsOptions[string]{
		New: func() string { return "initial" },
		Write: func(path, value string) error {
			if !isMainThread() {
				t.Error("Write is not on main")
			}
			writes++
			if writeErr != nil {
				return writeErr
			}
			return os.WriteFile(path, []byte(value), 0o600)
		},
	})
	d := m.New()
	if s := d.State(); s.Path != "" || s.Dirty || s.Busy || s.Title != "Untitled" || d.Value() != "initial" {
		t.Fatalf("new: %+v", s)
	}
	if err := d.Revert(); !errors.Is(err, ErrDocumentUntitled) {
		t.Fatalf("untitled revert: %v", err)
	}
	d.SetValue("edited")
	path := filepath.Join(dir, "first.txt")
	writeErr = fail
	if err := d.SaveTo(path); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if s := d.State(); s.Path != "" || !s.Dirty || s.Busy || len(m.Recent()) != 0 {
		t.Fatalf("failed save committed: %+v", s)
	}
	writeErr = nil
	if err := d.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	path, _ = documentPath(path)
	if s := d.State(); s.Path != path || s.Dirty || s.Title != "first.txt" {
		t.Fatalf("saved: %+v", s)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "edited" {
		t.Fatalf("file: %q, %v", b, err)
	}
	if got := m.Recent(); !slices.Equal(got, []string{path}) {
		t.Fatal(got)
	}
	d.SetValue("unsaved")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := d.Revert(); err == nil {
		t.Fatal("missing file reverted")
	}
	if !d.State().Dirty || d.Value() != "unsaved" {
		t.Fatal("failed revert discarded edits")
	}
	documentFile(t, dir, "first.txt", "external")
	if err := d.Revert(); err != nil {
		t.Fatal(err)
	}
	if d.State().Dirty || d.Value() != "external" {
		t.Fatal("revert did not commit loaded value")
	}
	d.SetDirty(true)
	d.SetDirty(false) // an app's undo checkpoint
	if d.State().Dirty {
		t.Fatal("checkpoint did not become clean")
	}
	if writes != 2 {
		t.Fatalf("writes: %d", writes)
	}
}

func TestDocumentSaveDialogs(t *testing.T) {
	m := testDocuments(t, DocumentsOptions[string]{})
	d := m.New()
	d.SetValue("keep me")
	onMain(func() { fb.SaveResult = ""; fb.SaveError = nil })
	t.Cleanup(func() { onMain(func() { fb.SaveResult = ""; fb.SaveError = nil; fb.SaveDialogHook = nil }) })
	if err := d.Save(); !errors.Is(err, ErrDocumentCanceled) {
		t.Fatal(err)
	}
	if !d.State().Dirty || d.State().Path != "" || len(m.Recent()) != 0 {
		t.Fatal("canceled untitled save committed")
	}
	fail := errors.New("dialog failed")
	onMain(func() { fb.SaveError = fail })
	if err := d.SaveAs(); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "saved.txt")
	onMain(func() {
		fb.SaveError = nil
		fb.SaveDialogHook = func(parent platform.Window, o *platform.SaveDialogOptions, done func(string, error)) {
			if o.DefaultPath != "Untitled" {
				t.Errorf("default name: %s", o.DefaultPath)
			}
			done(path, nil)
		}
	})
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	before := d.State()
	onMain(func() { fb.SaveDialogHook = nil; fb.SaveResult = "" })
	if err := d.SaveAs(); !errors.Is(err, ErrDocumentCanceled) {
		t.Fatal(err)
	}
	if after := d.State(); after != before {
		t.Fatalf("canceled Save As changed state: %+v => %+v", before, after)
	}
}

func TestDocumentSaveCoordinatesNestedEvents(t *testing.T) {
	m := testDocuments(t, DocumentsOptions[string]{})
	d := m.New()
	w, _ := testWindow(t, WindowOptions{})
	if err := d.Attach(w); err != nil {
		t.Fatal(err)
	}
	d.SetValue("before dialog")
	ready := make(chan func(string, error), 1)
	onMain(func() {
		fb.SaveDialogHook = func(_ platform.Window, _ *platform.SaveDialogOptions, done func(string, error)) { ready <- done }
	})
	t.Cleanup(func() { onMain(func() { fb.SaveDialogHook = nil }) })
	result := make(chan error, 1)
	go func() { result <- d.Save() }()
	var answer func(string, error)
	select {
	case answer = <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("dialog did not open")
	}
	if !d.State().Busy {
		t.Fatal("save is not busy while awaiting dialog")
	}
	d.SetValue("during dialog")
	if err := d.SaveTo(filepath.Join(t.TempDir(), "other.txt")); !errors.Is(err, ErrDocumentBusy) {
		t.Fatal(err)
	}
	w.Close() // must return without a second dialog or deadlock
	if w.IsDestroyed() {
		t.Fatal("busy document closed")
	}
	path := filepath.Join(t.TempDir(), "saved.txt")
	onMain(func() { answer(path, nil) })
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("save did not complete")
	}
	if s := d.State(); s.Busy || s.Dirty {
		t.Fatal(s)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "during dialog" {
		t.Fatalf("saved stale snapshot: %q %v", b, err)
	}
}

func TestDocumentEditsDuringWriteAndRevert(t *testing.T) {
	var d *Document[string]
	editOnRead := false
	fail := errors.New("write failed")
	failWrite := false
	m := testDocuments(t, DocumentsOptions[string]{
		Read: func(string) (string, error) {
			if editOnRead {
				d.SetValue("newer read edit")
			}
			return "disk", nil
		},
		Write: func(_ string, value string) error {
			if value != "snapshot" {
				t.Errorf("write snapshot: %q", value)
			}
			d.SetValue("newer write edit")
			if failWrite {
				return fail
			}
			return nil
		},
		ConfirmClose: func(*Document[string]) (DocumentDecision, error) { return DocumentSave, nil },
	})
	d = m.New()
	d.SetValue("snapshot")
	if err := d.SaveTo(filepath.Join(t.TempDir(), "snapshot.txt")); err != nil {
		t.Fatal(err)
	}
	if !d.State().Dirty || d.Value() != "newer write edit" {
		t.Fatal("save lost nested edit")
	}
	d.SetValue("snapshot")
	if err := d.Close(); !errors.Is(err, ErrDocumentBusy) {
		t.Fatal("close lost edits made during its save:", err)
	}
	if d.State().Closed {
		t.Fatal("close ignored newer edits")
	}
	d.SetValue("snapshot")
	failWrite = true
	if err := d.Save(); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if !d.State().Dirty {
		t.Fatal("failed save cleaned newer edits")
	}
	editOnRead = true
	if err := d.Revert(); !errors.Is(err, ErrDocumentBusy) {
		t.Fatal(err)
	}
	if d.Value() != "newer read edit" || !d.State().Dirty {
		t.Fatal("revert overwrote newer edit")
	}
	editOnRead = false
	if err := d.Revert(); err != nil {
		t.Fatal(err)
	}
	if d.Value() != "disk" || d.State().Dirty {
		t.Fatal("revert failed")
	}
}

func TestDocumentSuccessfulSaveOnCloseAndDestroyedDialog(t *testing.T) {
	m := testDocuments(t, DocumentsOptions[string]{})
	d := m.New()
	d.SetValue("saved by close")
	w, _ := testWindow(t, WindowOptions{})
	if err := d.Attach(w); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "closed.txt")
	onMain(func() { fb.MessageResult.Response = 0; fb.SaveResult = path })
	t.Cleanup(func() {
		onMain(func() { fb.MessageResult = platform.MessageBoxResult{}; fb.SaveResult = ""; fb.SaveDialogHook = nil })
	})
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if !d.State().Closed || d.State().Dirty || !w.IsDestroyed() {
		t.Fatal("successful save did not close cleanly")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "saved by close" {
		t.Fatalf("close file: %q %v", data, err)
	}
	other := m.New()
	other.SetValue("destroyed during dialog")
	own, _ := testWindow(t, WindowOptions{})
	if err := other.Attach(own); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "never-written.txt")
	onMain(func() {
		fb.SaveDialogHook = func(_ platform.Window, _ *platform.SaveDialogOptions, done func(string, error)) {
			own.Destroy()
			done(second, nil)
		}
	})
	if err := other.Save(); !errors.Is(err, ErrDocumentClosed) {
		t.Fatal(err)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatal("forced destroy still wrote the untitled document", err)
	}
}

func TestDocumentRestoreRetainsFailedFilesForRetry(t *testing.T) {
	dir := t.TempDir()
	p := documentFile(t, dir, "good.txt", "good")
	missing := filepath.Join(dir, "missing.txt")
	statePath := filepath.Join(dir, "documents.json")
	data := fmt.Appendf(nil, `{"open":[%q,%q]}`, p, missing)
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	m := testDocuments(t, DocumentsOptions[string]{StatePath: statePath})
	docs, err := m.Restore()
	if err == nil || len(docs) != 1 {
		t.Fatalf("partial restore: %v %v", docs, err)
	}
	documentFile(t, dir, "missing.txt", "recovered")
	again, err := m.Restore()
	if err != nil || len(again) != 2 || again[0] != docs[0] || again[1].Value() != "recovered" {
		t.Fatalf("restore retry: %v %v", again, err)
	}
}

func TestDocumentCloseGuard(t *testing.T) {
	var writeErr error
	m := testDocuments(t, DocumentsOptions[string]{Write: func(string, string) error { return writeErr }})
	d := m.New()
	w, fw := testWindow(t, WindowOptions{})
	if err := d.Attach(w); err != nil {
		t.Fatal(err)
	}
	d.SetValue("edited")
	if !onMainValue(func() bool { return fw.Document.Dirty }) {
		t.Fatal("native edited state missing")
	}
	t.Cleanup(func() {
		onMain(func() { fb.MessageResult = platform.MessageBoxResult{}; fb.MessageError = nil; fb.SaveResult = "" })
	})
	onMain(func() { fb.MessageResult.Response = 2 })
	if err := d.Close(); !errors.Is(err, ErrDocumentCanceled) {
		t.Fatal(err)
	}
	if onMainValue(fw.H.ShouldClose) || w.IsDestroyed() {
		t.Fatal("system close ignored cancellation")
	}
	onMain(func() { fb.MessageResult.Response = 0; fb.SaveResult = "" })
	if err := d.Close(); !errors.Is(err, ErrDocumentCanceled) {
		t.Fatal("canceled save did not cancel close:", err)
	}
	writeErr = errors.New("disk full")
	onMain(func() { fb.SaveResult = filepath.Join(t.TempDir(), "save.txt") })
	if err := d.Close(); !errors.Is(err, writeErr) {
		t.Fatal(err)
	}
	if w.IsDestroyed() || !d.State().Dirty {
		t.Fatal("failed close lost data")
	}
	onMain(func() { fb.MessageResult.Response = 1 })
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if !w.IsDestroyed() || !d.State().Closed || len(m.OpenDocuments()) != 0 {
		t.Fatal("discard did not close")
	}
}

func TestDocumentCloseListenerAndQuit(t *testing.T) {
	confirmed := 0
	m := testDocuments(t, DocumentsOptions[string]{ConfirmClose: func(*Document[string]) (DocumentDecision, error) { confirmed++; return DocumentCancel, nil }})
	d := m.New()
	w, _ := testWindow(t, WindowOptions{})
	if err := d.Attach(w); err != nil {
		t.Fatal(err)
	}
	d.SetDirty(true)
	off := w.OnClose(func(e *CloseEvent) { e.PreventDefault() })
	w.Close()
	if confirmed != 0 {
		t.Fatal("document prompted despite an OnClose veto")
	}
	off()
	if onMainValue(App.prepareQuit) {
		t.Fatal("dirty document allowed quit")
	}
	if w.IsDestroyed() || d.State().Closed || onMainValue(func() bool { return App.quitting || m.quitting }) {
		t.Fatal("quit cancellation did not unwind")
	}
	// Destroy is the deliberate escape hatch, and still unregisters the doc.
	w.Destroy()
	if !d.State().Closed {
		t.Fatal("Destroy did not close document")
	}
	// Windowless documents participate in quitting too.
	d2 := m.New()
	d2.SetDirty(true)
	if onMainValue(App.prepareQuit) || d2.State().Closed {
		t.Fatal("windowless dirty document allowed quit")
	}
}

func TestDocumentOpenRecentsAndReopen(t *testing.T) {
	dir := t.TempDir()
	a := documentFile(t, dir, "a.txt", "a")
	b := documentFile(t, dir, "b.txt", "b")
	c := documentFile(t, dir, "c.txt", "c")
	reads := 0
	m := testDocuments(t, DocumentsOptions[string]{RecentLimit: 2, Read: func(p string) (string, error) { reads++; data, err := os.ReadFile(p); return string(data), err }})
	d, err := m.Open(a)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Open(filepath.Join(dir, ".", "a.txt"))
	if err != nil || again != d || reads != 1 {
		t.Fatalf("duplicate open: %p %v %d", again, err, reads)
	}
	if _, err := m.Open(b); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(c); err != nil {
		t.Fatal(err)
	}
	if got := m.Recent(); !slices.Equal(got, []string{c, b}) {
		t.Fatal(got)
	}
	if _, err := m.Open(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("missing file opened")
	}
	if got := m.Recent(); !slices.Equal(got, []string{c, b}) {
		t.Fatal("failed open changed recents", got)
	}
	other := m.New()
	if err := other.SaveTo(a); !errors.Is(err, ErrDocumentPathInUse) || other.State().Path != "" {
		t.Fatal("Save As took another document's identity:", err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReopenClosed(); err == nil {
		t.Fatal("missing closed file reopened")
	}
	documentFile(t, dir, "a.txt", "reopened")
	reopened, err := m.ReopenClosed()
	if err != nil || reopened == d || reopened.Value() != "reopened" || reopened.State().Dirty {
		t.Fatalf("reopen: %v %v", reopened, err)
	}
	if !slices.Equal(m.Recent(), []string{a, c}) {
		t.Fatal(m.Recent())
	}
	copy := m.Recent()
	copy[0] = "mutated"
	if m.Recent()[0] != a {
		t.Fatal("Recent aliases internal state")
	}
	m.ForgetRecent(a)
	if !slices.Equal(m.Recent(), []string{c}) {
		t.Fatal(m.Recent())
	}
	m.ForgetRecent("")
	if len(m.Recent()) != 0 {
		t.Fatal(m.Recent())
	}
}

func TestDocumentReadReservationAndOpenFiles(t *testing.T) {
	var m *Documents[string]
	path := documentFile(t, t.TempDir(), "launch.txt", "launch")
	nested := false
	var reported []error
	m = testDocuments(t, DocumentsOptions[string]{HandleOpenFiles: true, OnError: func(err error) { reported = append(reported, err) }, Read: func(p string) (string, error) {
		if !isMainThread() {
			t.Error("Read is not on main")
		}
		if _, err := m.Open(p); !errors.Is(err, ErrDocumentBusy) {
			t.Error("Read reservation missing", err)
		}
		nested = true
		data, err := os.ReadFile(p)
		return string(data), err
	}})
	onMain(func() { (appHandler{}).OpenFiles([]string{path}) })
	if docs := m.OpenDocuments(); !nested || len(docs) != 1 || docs[0].Value() != "launch" {
		t.Fatal("open-file routing failed")
	}
	onMain(func() { (appHandler{}).OpenFiles([]string{path + ".missing"}) })
	if len(reported) != 1 || len(m.OpenDocuments()) != 1 {
		t.Fatal("failed event open was not reported")
	}
}

func TestDocumentSessionPersistence(t *testing.T) {
	dir := t.TempDir()
	path := documentFile(t, dir, "one.txt", "one")
	statePath := filepath.Join(dir, "state", "documents.json")
	m := testDocuments(t, DocumentsOptions[string]{StatePath: statePath})
	d, err := m.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	untitled := m.New()
	untitled.SetValue("two")
	second := filepath.Join(dir, "two.txt")
	onMain(m.prepareQuit)
	if err := untitled.SaveTo(second); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := untitled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Dispose(); err != nil {
		t.Fatal(err)
	}
	second, _ = documentPath(second)
	next := testDocuments(t, DocumentsOptions[string]{StatePath: statePath})
	restored, err := next.Restore()
	if err != nil || len(restored) != 2 {
		t.Fatalf("restore: %v %v", restored, err)
	}
	if restored[0].State().Path != path || restored[1].State().Path != second || restored[1].Value() != "two" {
		t.Fatal("session identities changed")
	}
	if !slices.Equal(next.Recent(), []string{second, path}) {
		t.Fatal(next.Recent())
	}
	if err := restored[0].Close(); err != nil {
		t.Fatal(err)
	}
	onMain(func() { next.prepareQuit(); next.cancelQuit() })
	if got := onMainValue(func() []string { return slices.Clone(next.state.Open) }); !slices.Equal(got, []string{second}) {
		t.Fatal("canceled quit retained closed docs:", got)
	}
}

func TestDocumentRevertAndClosePreserveNestedEdit(t *testing.T) {
	m := testDocuments(t, DocumentsOptions[string]{})
	d := m.New()
	w, _ := testWindow(t, WindowOptions{})
	if err := d.Attach(w); err != nil {
		t.Fatal(err)
	}
	once := false
	off := d.OnChange(func(s DocumentState) {
		if !s.Busy && !once {
			once = true
			d.SetValue("edit on busy=false")
		}
	})
	w.Close()
	off()
	if w.IsDestroyed() || !d.State().Dirty {
		t.Fatal("close discarded a change listener's new edit")
	}
}

func TestDocumentChildCloseProtection(t *testing.T) {
	decision := DocumentCancel
	m := testDocuments(t, DocumentsOptions[string]{ConfirmClose: func(*Document[string]) (DocumentDecision, error) { return decision, nil }})
	parent, _ := testWindow(t, WindowOptions{})
	child, _ := testWindow(t, WindowOptions{Parent: parent})
	d := m.New()
	if err := d.Attach(child); err != nil {
		t.Fatal(err)
	}
	d.SetValue("child edits")
	parent.Close()
	if parent.IsDestroyed() || child.IsDestroyed() {
		t.Fatal("parent close discarded dirty child")
	}
	decision = DocumentDiscard
	parent.Close()
	if !parent.IsDestroyed() || !child.IsDestroyed() || !d.State().Closed {
		t.Fatal("authorized family did not close")
	}
}

func TestDocumentFamilyRevalidatesEarlierCloseDecisions(t *testing.T) {
	var child *Document[string]
	m := testDocuments(t, DocumentsOptions[string]{
		Write: func(string, string) error { child.SetValue("edit during parent's save"); return nil },
		ConfirmClose: func(d *Document[string]) (DocumentDecision, error) {
			if d == child {
				return DocumentDiscard, nil
			}
			return DocumentSave, nil
		},
	})
	parentWin, _ := testWindow(t, WindowOptions{})
	childWin, _ := testWindow(t, WindowOptions{Parent: parentWin})
	parent := m.New()
	child = m.New()
	if err := parent.Attach(parentWin); err != nil {
		t.Fatal(err)
	}
	if err := child.Attach(childWin); err != nil {
		t.Fatal(err)
	}
	parent.SetValue("parent edit")
	child.SetValue("first child edit")
	onMain(func() { fb.SaveResult = filepath.Join(t.TempDir(), "parent.txt") })
	t.Cleanup(func() { onMain(func() { fb.SaveResult = "" }) })
	parentWin.Close()
	if parentWin.IsDestroyed() || childWin.IsDestroyed() || child.Value() != "edit during parent's save" || !child.State().Dirty {
		t.Fatal("later dialog invalidated an earlier close decision")
	}
}

func TestDocumentWebPageTitleDoesNotReplaceFileIdentity(t *testing.T) {
	m := testDocuments(t, DocumentsOptions[string]{})
	d := m.New()
	w, fw := testWindow(t, WindowOptions{})
	if err := d.Attach(w); err != nil {
		t.Fatal(err)
	}
	d.SetDirty(true)
	onMain(func() { fw.H.TitleChanged("HTML page title") })
	if title := w.Title(); title != "Untitled *" {
		t.Fatal("page replaced document title", title)
	}
}

func TestDocumentFileAliasesShareIdentity(t *testing.T) {
	dir := t.TempDir()
	path := documentFile(t, dir, "original.txt", "value")
	alias := filepath.Join(dir, "alias.txt")
	if err := os.Link(path, alias); err != nil {
		t.Skip("filesystem has no hard links:", err)
	}
	m := testDocuments(t, DocumentsOptions[string]{})
	d, err := m.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Open(alias)
	if err != nil || again != d || len(m.OpenDocuments()) != 1 {
		t.Fatal("file alias opened an independent document", err)
	}
	other := m.New()
	if err := other.SaveTo(alias); !errors.Is(err, ErrDocumentPathInUse) {
		t.Fatal("alias bypassed Save As ownership", err)
	}
}
