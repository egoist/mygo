package mygo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/internal/platform"
)

// ErrDownloadCanceled is what a download the app canceled ends with: the
// errors DownloadFinished reports wrap it, whatever the engine's own cause.
var ErrDownloadCanceled = platform.ErrDownloadCanceled

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
	// canceled records that the app canceled it, so that how it ended can
	// be reported as ErrDownloadCanceled whatever the engine names; ready
	// tells that it is running, and so that controls reach it.
	canceled atomic.Bool
	ready    atomic.Bool
	// mu guards scheme, the writer of a custom scheme's download, which
	// the core serves itself; nil for the engine's downloads.
	mu     sync.Mutex
	scheme schemeControl
}

// schemeControl is a custom scheme's download, which the core serves
// itself (downloadWriter).
type schemeControl interface {
	pause() error
	resume() error
	cancel()
}

func (d *Download) setScheme(s schemeControl) {
	d.mu.Lock()
	d.scheme = s
	d.mu.Unlock()
	if d.canceled.Load() {
		// Cancel landed after the download registered but before its
		// writer did: it went to the engine, whose download this is not.
		s.cancel()
	}
}

func (d *Download) schemeDownload() schemeControl {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.scheme
}

// OnProgress registers fn to run as the download's bytes arrive, with the
// bytes received so far and the total the server announced, 0 when it is
// unknown. It runs on the main thread. The returned function removes the
// listener.
func (d *Download) OnProgress(fn func(received, total int64)) (off func()) {
	return d.onProgress.add(fn, false)
}

// Pause pauses a running download, which Resume continues. It returns
// ErrUnsupported for the engine's downloads on macOS and Linux, whose
// WKWebView and WebKitGTK can only cancel them, and an error once the
// download ended. Downloads of the app's own schemes pause everywhere.
// Controls act on a running download: one called from the OnWillDownload
// listener, before it registered, reaches nothing.
func (d *Download) Pause() error {
	if s := d.schemeDownload(); s != nil {
		return s.pause()
	}
	return get(d.w, func(n platform.Window) error { return n.PauseDownload(d.URL, d.Path) })
}

// Resume continues a paused download, with the same limits as Pause.
func (d *Download) Resume() error {
	if s := d.schemeDownload(); s != nil {
		return s.resume()
	}
	return get(d.w, func(n platform.Window) error { return n.ResumeDownload(d.URL, d.Path) })
}

// Cancel stops a running download, which ends with ErrDownloadCanceled.
func (d *Download) Cancel() {
	if !d.ready.Load() {
		return // not running yet: nothing to cancel, and no error to relabel
	}
	d.canceled.Store(true)
	if s := d.schemeDownload(); s != nil {
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
	d.ready.Store(true)
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
	if err != nil && d.canceled.Load() && !errors.Is(err, ErrDownloadCanceled) {
		err = fmt.Errorf("%w: %w", ErrDownloadCanceled, err)
	}
	d.Err = err
	fire1(&h.w.onDownloadDone, d)
}

// cancelSchemeDownloads ends the window's custom-scheme downloads: their
// writers wait on the page of a window that is going away, paused or not.
func (w *Window) cancelSchemeDownloads() {
	w.mu.Lock()
	var schemes []schemeControl
	for _, d := range w.downloads {
		if s := d.schemeDownload(); s != nil {
			schemes = append(schemes, s)
		}
	}
	w.mu.Unlock()
	for _, s := range schemes {
		s.cancel()
	}
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
