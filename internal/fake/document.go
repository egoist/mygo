package fake

import "github.com/egoist/mygo/internal/platform"

func (w *Window) SetDocumentState(s platform.DocumentState) {
	w.Document = s
	title := s.Title
	if s.Dirty {
		title += " *"
	}
	w.SetTitle(title)
}
