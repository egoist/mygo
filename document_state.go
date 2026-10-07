package mygo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// History is app-owned, not an OS-global list. Only saved file identities are
// restored: unsaved data always goes through the close guard.
type documentHistory struct {
	Recent []string `json:"recent,omitempty"`
	Open   []string `json:"open,omitempty"`
	Closed []string `json:"closed,omitempty"`
}

type documentSession interface {
	prepareQuit()
	cancelQuit()
	closeWindowless() bool
}

// Main thread only. Controllers are explicitly opt-in and can be disposed.
var documentSessions []documentSession

func prepareDocumentQuit() {
	for _, s := range slices.Clone(documentSessions) {
		s.prepareQuit()
	}
}

func cancelDocumentQuit() {
	for _, s := range slices.Clone(documentSessions) {
		s.cancelQuit()
	}
}

func closeWindowlessDocuments() bool {
	for _, s := range slices.Clone(documentSessions) {
		if !s.closeWindowless() {
			return false
		}
	}
	return true
}

func (m *Documents[T]) closeWindowless() bool {
	for _, d := range m.paths {
		if d == nil {
			m.report(ErrDocumentBusy)
			return false
		}
	}
	for _, d := range slices.Clone(m.items) {
		if d.win == nil {
			if err := d.Close(); err != nil {
				m.report(err)
				return false
			}
		}
	}
	return true
}

func (m *Documents[T]) historyPath() (string, error) {
	if m.opts.StatePath != "" {
		return m.opts.StatePath, nil
	}
	dir, err := App.Path(PathUserData)
	return filepath.Join(dir, "documents.json"), err
}

func (m *Documents[T]) loadHistory() {
	if m.loaded {
		return
	}
	m.loaded = true
	if m.opts.MemoryOnly {
		return
	}
	p, err := m.historyPath()
	if err == nil {
		var data []byte
		data, err = os.ReadFile(p)
		if os.IsNotExist(err) {
			return
		}
		if err == nil {
			err = json.Unmarshal(data, &m.state)
		}
	}
	if err != nil {
		m.state = documentHistory{}
		m.report(fmt.Errorf("reading document history: %w", err))
	}
	m.state.Recent = normalizeDocumentPaths(m.state.Recent, max(0, m.opts.RecentLimit))
	m.state.Closed = normalizeDocumentPaths(m.state.Closed, max(1, m.opts.RecentLimit))
	m.state.Open = normalizeDocumentPaths(m.state.Open, 1000)
}

func normalizeDocumentPaths(paths []string, limit int) []string {
	var out []string
	seen := make(map[string]bool)
	for _, path := range paths {
		if len(out) >= limit {
			break
		}
		p, err := documentPath(path)
		if err != nil || seen[documentPathKey(p)] {
			continue
		}
		seen[documentPathKey(p)] = true
		out = append(out, p)
	}
	return out
}

func tailDocuments(paths []string, limit int) []string {
	return slices.Clone(paths[max(0, len(paths)-limit):])
}

func (m *Documents[T]) currentPaths() []string {
	var paths []string
	for _, d := range m.items {
		if d.path != "" {
			paths = append(paths, d.path)
		}
	}
	return paths
}

func (m *Documents[T]) persistHistory() {
	if !m.loaded || m.disposed {
		return
	}
	if !m.quitting && !m.restoring {
		m.state.Open = m.currentPaths()
	}
	if m.opts.MemoryOnly {
		return
	}
	p, err := m.historyPath()
	if err == nil {
		var data []byte
		data, err = json.MarshalIndent(m.state, "", "  ")
		if err == nil {
			err = writeDocumentHistory(p, data)
		}
	}
	if err != nil {
		m.report(fmt.Errorf("saving document history: %w", err))
	}
}

func writeDocumentHistory(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mygo-documents-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (m *Documents[T]) remember(path string) {
	key := documentPathKey(path)
	m.state.Recent = slices.DeleteFunc(m.state.Recent, func(p string) bool { return documentPathKey(p) == key })
	if m.opts.RecentLimit > 0 {
		m.state.Recent = append([]string{path}, m.state.Recent...)
		m.state.Recent = m.state.Recent[:min(len(m.state.Recent), m.opts.RecentLimit)]
	}
	m.persistHistory()
}

// Recent returns absolute paths, most recently opened or saved first. Failed
// opens and saves never enter this list. Missing files stay until forgotten.
func (m *Documents[T]) Recent() []string {
	return onMainValue(func() []string { m.loadHistory(); return slices.Clone(m.state.Recent) })
}

// ForgetRecent removes one path; an empty path clears the list.
func (m *Documents[T]) ForgetRecent(path string) {
	onMain(func() {
		m.loadHistory()
		if path == "" {
			m.state.Recent = nil
		} else if p, err := documentPath(path); err == nil {
			m.state.Recent = slices.DeleteFunc(m.state.Recent, func(item string) bool { return documentPathKey(item) == documentPathKey(p) })
		}
		m.persistHistory()
	})
}

// ReopenClosed opens the last closed named document from disk. Discarded
// edits are not restored. Failure keeps the entry available for a retry.
func (m *Documents[T]) ReopenClosed() (*Document[T], error) {
	var doc *Document[T]
	err := errLoopStopped
	onMain(func() {
		m.loadHistory()
		if len(m.state.Closed) == 0 {
			err = ErrDocumentCanceled
			return
		}
		p := m.state.Closed[len(m.state.Closed)-1]
		doc, err = m.open(p)
		if err == nil {
			// Opening can pump events. Remove this entry, not one appended
			// by a different document that closed during Read.
			for i := len(m.state.Closed) - 1; i >= 0; i-- {
				if m.state.Closed[i] == p {
					m.state.Closed = slices.Delete(m.state.Closed, i, i+1)
					break
				}
			}
			m.persistHistory()
		}
	})
	return doc, err
}

// Restore opens the saved session, normally from App.WhenReady. It continues
// after failures and returns successful documents with joined errors. Named
// documents open at a completed quit are remembered, including Save As paths
// chosen during quit. A canceled quit records the documents still open.
func (m *Documents[T]) Restore() ([]*Document[T], error) {
	var docs []*Document[T]
	err := errLoopStopped
	onMain(func() {
		if m.disposed {
			err = ErrDocumentClosed
			return
		}
		if m.restoring || m.quitting {
			err = ErrDocumentBusy
			return
		}
		m.loadHistory()
		paths := slices.Clone(m.state.Open)
		m.restoring = true
		defer func() { m.restoring = false }()
		var failed []string
		var errs []error
		for _, path := range paths {
			d, e := m.open(path)
			if e != nil {
				failed = append(failed, path)
				errs = append(errs, fmt.Errorf("%s: %w", path, e))
			} else {
				docs = append(docs, d)
			}
		}
		// Preserve failed identities for another Restore attempt. Persisting
		// an intermediate successful open must not consume the whole session.
		m.state.Open = normalizeDocumentPaths(append(m.currentPaths(), failed...), 1000)
		m.persistHistory()
		err = errors.Join(errs...)
	})
	return docs, err
}

func (m *Documents[T]) prepareQuit() {
	m.loadHistory()
	m.state.Open = m.currentPaths()
	m.quitting = true
	m.persistHistory()
}

func (m *Documents[T]) cancelQuit() { m.quitting = false; m.persistHistory() }

// A Save As during quit must replace its session identity too. The close
// guard is the last chance for an untitled document to acquire a path.
func (m *Documents[T]) savedDuringQuit(oldPath, path string) {
	if !m.quitting {
		return
	}
	for i, p := range m.state.Open {
		if p == oldPath {
			m.state.Open[i] = path
			return
		}
	}
	m.state.Open = append(m.state.Open, path)
}
