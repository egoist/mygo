//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
)

// downloadKey identifies a download in progress: its URL and the path it
// is saved to, as the core keys them.
type downloadKey struct{ url, path string }

// downloadOperation returns the ICoreWebView2DownloadOperation of the
// download of url saved at path, or 0 when none is running.
func (w *window) downloadOperation(url, path string) uintptr {
	return w.downloads[downloadKey{url, path}]
}

// finishDownload forgets a download that ended, releases its operation
// and reports how it ended.
func (w *window) finishDownload(key downloadKey, err error) {
	op := w.downloads[key]
	if op == 0 {
		return
	}
	delete(w.downloads, key)
	release(op)
	w.h.DownloadFinished(key.url, key.path, err)
}

func (w *window) PauseDownload(url, path string) error {
	op := w.downloadOperation(url, path)
	if op == 0 {
		return errors.New("mygo: no such download in progress")
	}
	if r := comCall(op, dlOpPause); failed(r) {
		return hresultError("ICoreWebView2DownloadOperation.Pause", r)
	}
	return nil
}

func (w *window) ResumeDownload(url, path string) error {
	op := w.downloadOperation(url, path)
	if op == 0 {
		return errors.New("mygo: no such download in progress")
	}
	if r := comCall(op, dlOpResume); failed(r) {
		return hresultError("ICoreWebView2DownloadOperation.Resume", r)
	}
	return nil
}

func (w *window) CancelDownload(url, path string) {
	if op := w.downloadOperation(url, path); op != 0 {
		comCall(op, dlOpCancel)
	}
}