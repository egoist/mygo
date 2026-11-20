//go:build !js

package mygo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo/internal/platform"
)

// DownloadEvent is passed to Page.OnWillDownload listeners. Setting Path
// chooses where the file goes; preventing the event cancels the download.
type DownloadEvent struct {
	Preventable
	// URL of the download.
	URL string
	// SuggestedName is the file name the page or the server suggests.
	SuggestedName string
	// Path is where the file is saved: by default SuggestedName in the
	// Downloads directory, numbered when a file has that name. A file
	// already at a Path listeners set is replaced.
	Path string
	// Download controls the download once it runs: its progress, pausing,
	// resuming and canceling. OnDownloadDone receives the same object.
	Download *Download
}

// Download is a download: the handle DownloadEvent carries while it runs,
// and what Page.OnDownloadDone receives once it ended, with Err set.
type Download struct {
	URL  string
	Path string
	// Err is why the download failed, or nil. It is set once the download
	// ended.
	Err error

	w          *Window
	onProgress listeners[func(received, total int64)]
	// scheme controls a download of a custom scheme, which the core
	// serves itself (downloadWriter); nil for the engine's downloads.
	scheme atomic.Pointer[downloadWriter]
}

// OnProgress registers fn to run as the download's bytes arrive, with the
// bytes received so far and the total the server announced, 0 when it is
// unknown. It runs on the main thread. The returned function removes the
// listener.
func (d *Download) OnProgress(fn func(received, total int64)) (off func()) {
	return d.onProgress.add(fn, false)
}

// Pause pauses a running download, which Resume continues. It returns
// ErrUnsupported on Linux for the engine's downloads, whose WebKitGTK
// cannot pause them, and an error once the download ended.
func (d *Download) Pause() error {
	if s := d.scheme.Load(); s != nil {
		return s.pause()
	}
	return get(d.w, func(n platform.Window) error { return n.PauseDownload(d.URL, d.Path) })
}

// Resume continues a paused download, with the same limits as Pause.
func (d *Download) Resume() error {
	if s := d.scheme.Load(); s != nil {
		return s.resume()
	}
	return get(d.w, func(n platform.Window) error { return n.ResumeDownload(d.URL, d.Path) })
}

// Cancel stops a running download, which ends with an error.
func (d *Download) Cancel() {
	if s := d.scheme.Load(); s != nil {
		s.cancel()
		return
	}
	d.w.do(func(n platform.Window) { n.CancelDownload(d.URL, d.Path) })
}

// OnWillDownload is called when a page starts a download: a link with the
// download attribute, or a response the page cannot show, such as a file
// sent as an attachment. Without listeners, downloads are saved to the
// Downloads directory, and those of an untrusted page
// (PageOptions.Untrusted) are canceled.
func (p *Page) OnWillDownload(fn func(e *DownloadEvent)) (off func()) {
	return p.w.onWillDownload.add(fn, false)
}

// OnDownloadDone is called when a download ended.
func (p *Page) OnDownloadDone(fn func(d *Download)) (off func()) {
	return p.w.onDownloadDone.add(fn, false)
}

// downloadKey identifies a download in progress: its URL and the path it
// is saved to, which numbered names keep apart.
type downloadKey struct{ url, path string }

func (h *windowHandler) DownloadStarted(url, suggested string) string {
	if h.w.untrusted && h.w.onWillDownload.len() == 0 {
		return ""
	}
	name := filepath.Base(filepath.Clean("/" + strings.ReplaceAll(suggested, `\`, "/")))
	if name == "/" || name == "." || name == "" {
		name = "download"
	}
	e := &DownloadEvent{URL: url, SuggestedName: name}
	if dir, err := App.Path(PathDownloads); err == nil && os.MkdirAll(dir, 0o755) == nil {
		e.Path = uniquePath(filepath.Join(dir, name))
	}
	d := &Download{URL: url, w: h.w}
	e.Download = d
	fire1(&h.w.onWillDownload, e)
	if e.prevented || e.Path == "" {
		return ""
	}
	d.Path = e.Path
	if h.w.downloads == nil {
		h.w.downloads = map[downloadKey]*Download{}
	}
	h.w.downloads[downloadKey{url, e.Path}] = d
	return e.Path
}

func (h *windowHandler) DownloadProgress(url, path string, received, total int64) {
	if d := h.w.downloads[downloadKey{url, path}]; d != nil {
		fire2(&d.onProgress, received, total)
	}
}

func (h *windowHandler) DownloadFinished(url, path string, err error) {
	key := downloadKey{url, path}
	d := h.w.downloads[key]
	delete(h.w.downloads, key)
	if d == nil {
		d = &Download{URL: url, Path: path, w: h.w}
	}
	d.Err = err
	fire1(&h.w.onDownloadDone, d)
}

// uniquePath numbers path, like "name (2).ext", when a file has that name.
func uniquePath(path string) string {
	if _, err := os.Lstat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		p := base + " (" + strconv.Itoa(i) + ")" + ext
		if _, err := os.Lstat(p); err != nil {
			return p
		}
	}
}