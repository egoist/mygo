//go:build darwin

package darwin

import "github.com/egoist/mygo/internal/platform"

func (w *window) SetDocumentState(s platform.DocumentState) {
	withPool(func() {
		w.SetTitle(s.Title)
		send(w.win, "setRepresentedFilename:", uintptr(nsString(s.Path)))
		send(w.win, "setDocumentEdited:", boolArg(s.Dirty))
	})
}
