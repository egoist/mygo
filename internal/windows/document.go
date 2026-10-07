//go:build windows && (amd64 || arm64)

package windows

import "github.com/egoist/mygo/internal/platform"

func (w *window) SetDocumentState(s platform.DocumentState) {
	title := s.Title
	if s.Dirty {
		title += " *"
	}
	w.SetTitle(title)
}
