package mygo

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

// Document operation errors. Cancellation is an outcome, not a successful save.
var (
	ErrDocumentCanceled  = errors.New("mygo: document operation canceled")
	ErrDocumentBusy      = errors.New("mygo: document operation already in progress")
	ErrDocumentClosed    = errors.New("mygo: document is closed")
	ErrDocumentUntitled  = errors.New("mygo: an untitled document cannot be reverted")
	ErrDocumentPathInUse = errors.New("mygo: another document is using this path")
)

// DocumentDecision is the answer to a dirty document's close request.
type DocumentDecision int

const (
	// DocumentCancel leaves the document open (the safe zero value).
	DocumentCancel DocumentDecision = iota
	DocumentSave
	DocumentDiscard
)

// DocumentState describes a document independently of its value or view.
type DocumentState struct {
	Path                string // absolute file path; empty for untitled documents
	Title               string
	Dirty, Busy, Closed bool
}

// DocumentsOptions defines an opt-in document application. T is the app's
// model, for example string or an immutable struct. All callbacks and change
// listeners run on the main thread. Read must return a new value; Write must
// not mutate its value and should replace files atomically. Failed operations
// never commit document state. Keep callbacks short; never wait for a
// goroutine that needs the main thread. Prepare expensive data before saving.
type DocumentsOptions[T any] struct {
	// New supplies an untitled document's value; nil uses T's zero value.
	New   func() T
	Read  func(path string) (T, error)
	Write func(path string, value T) error
	// OpenWindow creates a view for a new or opened document. The returned
	// window is attached automatically; nil supports documents without windows.
	OpenWindow func(*Document[T]) *Window
	// ConfirmClose replaces the Save / Discard / Cancel dialog.
	ConfirmClose  func(*Document[T]) (DocumentDecision, error)
	OpenDialog    OpenDialogOptions
	SaveDialog    SaveDialogOptions
	UntitledTitle string // defaults to "Untitled"
	RecentLimit   int    // zero means 10; negative disables recents
	// StatePath defaults to documents.json in PathUserData. Separate controllers
	// must use separate paths. MemoryOnly disables persistence.
	StatePath  string
	MemoryOnly bool
	// HandleOpenFiles routes App.OnOpenFile to Open. Set it before App.Run to
	// receive launch files. Use the single-instance lock on Linux and Windows.
	HandleOpenFiles bool
	// OnError receives errors from open-file events, window close requests and
	// persistence. Explicit operations return their own errors. nil logs errors.
	OnError func(error)
}

// Documents coordinates typed documents, recent files and reopening. Create
// one before App.Run, then call New, Open or Restore when the app is ready.
// Its methods, and Document's, are safe from any goroutine. Values containing
// pointers, maps or slices are shallow copies: treat them as immutable and
// replace them with SetValue, or mutate them only inside Update.
type Documents[T any] struct {
	opts                                  DocumentsOptions[T]
	items                                 []*Document[T]
	paths                                 map[string]*Document[T] // includes reservations during Read / Write
	state                                 documentHistory
	loaded, disposed, quitting, restoring bool
	offOpen                               func()
}

// NewDocuments creates a document controller. Read and Write are required.
// It does not open windows or read/write the history file yet.
func NewDocuments[T any](opts DocumentsOptions[T]) *Documents[T] {
	if opts.Read == nil || opts.Write == nil {
		panic("mygo: NewDocuments requires Read and Write")
	}
	opts.OpenDialog.Filters = cloneFileFilters(opts.OpenDialog.Filters)
	opts.SaveDialog.Filters = cloneFileFilters(opts.SaveDialog.Filters)
	if opts.UntitledTitle == "" {
		opts.UntitledTitle = "Untitled"
	}
	if opts.RecentLimit == 0 {
		opts.RecentLimit = 10
	}
	m := &Documents[T]{opts: opts, paths: make(map[string]*Document[T])}
	onMain(func() { documentSessions = append(documentSessions, m) })
	if opts.HandleOpenFiles {
		m.offOpen = App.OnOpenFile(func(path string) {
			_, err := m.Open(path)
			m.report(err)
		})
	}
	return m
}

func cloneFileFilters(fs []FileFilter) []FileFilter {
	out := slices.Clone(fs)
	for i := range out {
		out[i].Extensions = slices.Clone(out[i].Extensions)
	}
	return out
}

// New opens a clean untitled document. A disposed controller returns nil.
func (m *Documents[T]) New() *Document[T] {
	return onMainValue(func() *Document[T] {
		if m.disposed || m.quitting {
			return nil
		}
		m.loadHistory()
		var value T
		if m.opts.New != nil {
			value = m.opts.New()
		}
		if m.disposed || m.quitting {
			return nil
		}
		d := &Document[T]{owner: m, value: value, title: m.opts.UntitledTitle}
		m.add(d)
		return d
	})
}

// Open reads a file once. Opening its path again returns and focuses the
// existing document; a path being read or written returns ErrDocumentBusy.
// Paths are absolute, cleaned and resolved through existing symlinks.
func (m *Documents[T]) Open(path string) (*Document[T], error) {
	var d *Document[T]
	err := errLoopStopped
	onMain(func() { d, err = m.open(path) })
	return d, err
}

func documentPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("mygo: empty document path")
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Save As targets may not exist yet. Resolve their existing ancestor
	// too, so creating the file does not change its identity (/var and
	// /private/var on macOS, for example).
	parent, suffix := p, []string{}
	for {
		if real, e := filepath.EvalSymlinks(parent); e == nil {
			p = filepath.Join(append([]string{real}, suffix...)...)
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		suffix = append([]string{filepath.Base(parent)}, suffix...)
		parent = next
	}
	return p, nil
}

func documentPathKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// Existing file aliases (hard links or differently cased names on a
// case-insensitive volume) must not create two independently dirty editors.
func (m *Documents[T]) pathOwner(path string) (*Document[T], bool) {
	if d, ok := m.paths[documentPathKey(path)]; ok {
		return d, true
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	for name, d := range m.paths {
		if other, err := os.Stat(name); err == nil && os.SameFile(info, other) {
			return d, true
		}
	}
	return nil, false
}

func (m *Documents[T]) open(path string) (*Document[T], error) {
	if m.disposed {
		return nil, ErrDocumentClosed
	}
	if m.quitting {
		return nil, ErrDocumentBusy
	}
	m.loadHistory()
	p, err := documentPath(path)
	if err != nil {
		return nil, err
	}
	k := documentPathKey(p)
	if existing, ok := m.pathOwner(p); ok {
		if existing == nil || existing.busy {
			return nil, ErrDocumentBusy
		}
		if existing.win != nil {
			existing.win.Show()
		}
		m.remember(existing.path)
		return existing, nil
	}
	m.paths[k] = nil // reserve before calling app code, which may pump events
	defer func() {
		if m.paths[k] == nil {
			delete(m.paths, k)
		}
	}()
	value, err := m.opts.Read(p)
	if err != nil {
		return nil, err
	}
	if m.disposed || m.quitting {
		return nil, ErrDocumentClosed
	}
	d := &Document[T]{owner: m, value: value, path: p, title: filepath.Base(p)}
	m.paths[k] = d
	m.add(d)
	m.remember(p)
	return d, nil
}

func (m *Documents[T]) add(d *Document[T]) {
	m.items = append(m.items, d)
	if m.opts.OpenWindow != nil {
		if w := m.opts.OpenWindow(d); w != nil {
			if err := d.attach(w); err != nil {
				panic(err)
			}
		}
	}
	m.persistHistory()
}

// OpenDialog opens the selected files. Cancel returns ErrDocumentCanceled.
// A partial result includes the files that opened and the joined errors.
func (m *Documents[T]) OpenDialog(parent *Window) ([]*Document[T], error) {
	o := m.opts.OpenDialog
	o.Parent = parent
	paths, err := Dialog.Open(o)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, ErrDocumentCanceled
	}
	return m.openPaths(paths)
}

func (m *Documents[T]) openPaths(paths []string) ([]*Document[T], error) {
	var docs []*Document[T]
	var errs []error
	for _, p := range paths {
		d, err := m.Open(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		} else {
			docs = append(docs, d)
		}
	}
	return docs, errors.Join(errs...)
}

// OpenDocuments returns the open documents in creation order.
func (m *Documents[T]) OpenDocuments() []*Document[T] {
	return onMainValue(func() []*Document[T] { return slices.Clone(m.items) })
}

// Dispose detaches the controller's app hooks. Close its documents first.
func (m *Documents[T]) Dispose() error {
	err := errLoopStopped
	onMain(func() {
		if len(m.items) != 0 || len(m.paths) != 0 {
			err = ErrDocumentBusy
			return
		}
		m.disposed = true
		if m.offOpen != nil {
			m.offOpen()
			m.offOpen = nil
		}
		for i, s := range documentSessions {
			if s == m {
				documentSessions = slices.Delete(documentSessions, i, i+1)
				break
			}
		}
		err = nil
	})
	return err
}

func (m *Documents[T]) report(err error) {
	if err == nil || errors.Is(err, ErrDocumentCanceled) {
		return
	}
	if m.opts.OnError != nil {
		m.opts.OnError(err)
	} else {
		log.Printf("mygo: documents: %v", err)
	}
}

// Document owns a typed value and its file identity. Only successful saves
// change the path or saved revision. Attach links its close guard and native
// dirty indicator to a window; windowless documents use Close directly.
type Document[T any] struct {
	owner           *Documents[T]
	value           T
	path, title     string
	revision, saved uint64
	busy, closed    bool
	win             *Window
	closeErr        error
	closeRevision   uint64
	closeApproved   bool
	onChange        listeners[func(DocumentState)]
}

// State returns the file identity and lifecycle state.
func (d *Document[T]) State() DocumentState { return onMainValue(d.state) }
func (d *Document[T]) state() DocumentState {
	return DocumentState{Path: d.path, Title: d.title, Dirty: d.revision != d.saved, Busy: d.busy, Closed: d.closed}
}

// Value returns a shallow copy of the value. See Documents for ownership.
func (d *Document[T]) Value() T { return onMainValue(func() T { return d.value }) }

// SetValue replaces the value and marks the document dirty.
func (d *Document[T]) SetValue(value T) { d.Update(func(v *T) { *v = value }) }

// Update changes the value on the main thread and marks it dirty. It runs
// synchronously. A closed document ignores updates. Mutating during Write is
// allowed, but the revision saved by Write will leave newer edits dirty.
func (d *Document[T]) Update(fn func(*T)) {
	onMain(func() {
		if d.closed {
			return
		}
		fn(&d.value)
		d.revision++
		d.changed()
	})
}

// SetDirty marks an external change, or marks the current revision clean
// (for example after undoing to a saved checkpoint). Every true call counts
// as a new revision, even when the document was already dirty.
func (d *Document[T]) SetDirty(dirty bool) {
	onMain(func() {
		if d.closed {
			return
		}
		if dirty {
			d.revision++
		} else {
			d.saved = d.revision
		}
		d.changed()
	})
}

// Window returns the attached window, or nil.
func (d *Document[T]) Window() *Window { return onMainValue(func() *Window { return d.win }) }

// Attach associates one document with one window. The controller calls it
// for OpenWindow's result. Existing OnClose listeners still take precedence;
// Window.Destroy deliberately bypasses dirty-document protection.
func (d *Document[T]) Attach(w *Window) error {
	err := errLoopStopped
	onMain(func() { err = d.attach(w) })
	return err
}

func (d *Document[T]) attach(w *Window) error {
	if d.closed || w == nil || w.native == nil {
		return ErrDocumentClosed
	}
	if d.win == w && w.document == d {
		return nil
	}
	if d.win != nil || w.document != nil {
		return errors.New("mygo: document or window already attached")
	}
	d.win, w.document = w, d
	d.changed()
	return nil
}

// OnChange observes value, dirty, busy, identity and closed transitions on
// the main thread. State has already been committed when listeners run.
func (d *Document[T]) OnChange(fn func(DocumentState)) func() { return d.onChange.add(fn, false) }

func (d *Document[T]) changed() {
	s := d.state()
	if w := d.win; w != nil && w.native != nil {
		w.native.SetDocumentState(platform.DocumentState{Title: s.Title, Path: s.Path, Dirty: s.Dirty})
		w.contentChanged()
	}
	fire1(&d.onChange, s)
}

func (d *Document[T]) operation(fn func() error) error {
	err := errLoopStopped
	onMain(func() {
		if d.closed {
			err = ErrDocumentClosed
			return
		}
		if d.busy {
			err = ErrDocumentBusy
			return
		}
		d.busy = true
		defer func() { d.busy = false; d.changed() }()
		d.changed()
		if d.closed {
			err = ErrDocumentClosed
			return
		}
		err = fn()
	})
	return err
}

// Save writes the current file, or asks for a path for an untitled document.
func (d *Document[T]) Save() error { return d.operation(func() error { return d.save(false, "") }) }

// SaveAs asks for a new path even for a named document.
func (d *Document[T]) SaveAs() error { return d.operation(func() error { return d.save(true, "") }) }

// SaveTo writes a specific path without a dialog. Write is responsible for
// any overwrite policy; SaveAs uses the system's overwrite confirmation.
func (d *Document[T]) SaveTo(path string) error {
	if path == "" {
		return errors.New("mygo: empty document path")
	}
	return d.operation(func() error { return d.save(false, path) })
}

func (d *Document[T]) save(as bool, target string) error {
	if target == "" {
		target = d.path
	}
	if as || target == "" {
		o := d.owner.opts.SaveDialog
		o.Parent = d.win
		if o.DefaultPath == "" {
			o.DefaultPath = or(d.path, d.title)
		}
		var err error
		target, err = Dialog.Save(o)
		if err != nil {
			return err
		}
		if target == "" {
			return ErrDocumentCanceled
		}
	}
	if d.closed {
		return ErrDocumentClosed
	}
	p, err := documentPath(target)
	if err != nil {
		return err
	}
	k := documentPathKey(p)
	if other, ok := d.owner.pathOwner(p); ok && other != d {
		return ErrDocumentPathInUse
	}
	oldKey := documentPathKey(d.path)
	d.owner.paths[k] = d // reserve the new identity through Write
	defer func() {
		if d.closed || d.path != p {
			if d.owner.paths[k] == d {
				delete(d.owner.paths, k)
			}
		}
	}()
	revision := d.revision
	if err := d.owner.opts.Write(p, d.value); err != nil {
		return err
	}
	if d.closed {
		return ErrDocumentClosed
	}
	if oldKey != k && d.owner.paths[oldKey] == d {
		delete(d.owner.paths, oldKey)
	}
	d.owner.savedDuringQuit(d.path, p)
	d.path, d.title, d.saved = p, filepath.Base(p), revision
	d.owner.remember(p)
	d.changed()
	return nil
}

// Revert reloads the current file. It intentionally discards edits after a
// successful Read. A failed Read, or edits made while Read pumps events,
// leave the current value and dirty state intact.
func (d *Document[T]) Revert() error {
	return d.operation(func() error {
		if d.path == "" {
			return ErrDocumentUntitled
		}
		revision := d.revision
		value, err := d.owner.opts.Read(d.path)
		if err != nil {
			return err
		}
		if d.closed {
			return ErrDocumentClosed
		}
		if d.revision != revision {
			return ErrDocumentBusy
		}
		d.value, d.saved = value, d.revision
		d.changed()
		return nil
	})
}

func (d *Document[T]) canClose() error {
	authorizedRevision := d.revision
	err := d.operation(func() error {
		if d.revision == d.saved {
			return nil
		}
		var decision DocumentDecision
		var err error
		if confirm := d.owner.opts.ConfirmClose; confirm != nil {
			decision, err = confirm(d)
		} else {
			var answer MessageResult
			answer, err = Dialog.Message(MessageOptions{Parent: d.win, Type: MessageWarning,
				Message: "Save changes to “" + d.title + "”?", Detail: "Your changes will be lost if you discard them.",
				Buttons: []string{"Save", "Discard", "Cancel"}, CancelButton: 2})
			decision = []DocumentDecision{DocumentSave, DocumentDiscard, DocumentCancel}[answer.Button]
		}
		if err != nil {
			return err
		}
		if d.closed {
			return ErrDocumentClosed
		}
		switch decision {
		case DocumentDiscard:
			authorizedRevision = d.revision
			return nil
		case DocumentSave:
			if err := d.save(false, ""); err != nil {
				return err
			}
			// A nested event edited the value after the save snapshot.
			if d.revision != d.saved {
				return ErrDocumentBusy
			}
			authorizedRevision = d.revision
			return nil
		default:
			return ErrDocumentCanceled
		}
	})
	// Busy=false listeners can make a new edit after the operation's last
	// check. Closing a clean/saved document must protect that edit too.
	if err == nil && d.revision != authorizedRevision {
		return ErrDocumentBusy
	}
	return err
}

// Close asks the window's OnClose listeners and then offers to save dirty
// changes. A failed/canceled save prevents both window close and app quit.
func (d *Document[T]) Close() error {
	err := errLoopStopped
	onMain(func() {
		if d.closed {
			err = nil
			return
		}
		if d.win != nil {
			d.closeErr = nil
			if !d.win.close() {
				err = or(d.closeErr, error(ErrDocumentCanceled))
				return
			}
		} else {
			if err = d.canClose(); err != nil {
				return
			}
			d.windowClosed()
		}
		err = nil
	})
	return err
}

func (d *Document[T]) allowClose() bool {
	d.closeApproved = false
	d.closeErr = d.canClose()
	if d.closeErr == nil {
		d.closeApproved = true
		d.closeRevision = d.revision
	}
	d.owner.report(d.closeErr)
	return d.closeErr == nil
}

// A different document's dialog may process edits after this one answered.
// Revalidate prepared documents before destroying a window family.
func (d *Document[T]) validateClose() bool {
	if d.closeApproved && !d.busy && d.revision == d.closeRevision {
		return true
	}
	d.closeErr = ErrDocumentBusy
	d.owner.report(d.closeErr)
	return false
}

func (d *Document[T]) windowClosed() {
	if d.closed {
		return
	}
	d.closed, d.win = true, nil
	m := d.owner
	if m.paths[documentPathKey(d.path)] == d {
		delete(m.paths, documentPathKey(d.path))
	}
	for i, item := range m.items {
		if item == d {
			m.items = slices.Delete(m.items, i, i+1)
			break
		}
	}
	if d.path != "" && !m.quitting {
		m.state.Closed = append(m.state.Closed, d.path)
		m.state.Closed = tailDocuments(m.state.Closed, max(1, m.opts.RecentLimit))
	}
	m.persistHistory()
	d.changed()
}

// windowDocument is the non-generic connection to the window lifecycle.
type windowDocument interface {
	allowClose() bool
	validateClose() bool
	windowClosed()
}
