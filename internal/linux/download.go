//go:build linux && !android && (amd64 || arm64)

package linux

import (
	"fmt"
	"net/url"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
)

// Downloads: the web context reports them, WebKit asks each where to save
// the file, and reports how it ended ("failed" comes before "finished").
// Progress comes from the estimated-progress property's notifications,
// read as bytes received of the response's content length. WebKitGTK
// cannot pause a download, only cancel it.

type download struct {
	w         *window
	url, path string
	err       error
}

var (
	downloads       = map[ptr]*download{}
	downloadsHooked bool

	cbDownloadStarted, cbDownloadDestination, cbDownloadProgress, cbDownloadFinished, cbDownloadFailed ptr
)

func initDownloadCallbacks() {
	cbDownloadStarted = purego.NewCallback(func(ctx, d, data ptr) {
		w := theBackend.byWebView[webkitDownloadGetWebView(d)]
		if w == nil {
			return
		}
		downloads[d] = &download{w: w, url: goStr(webkitURIRequestGetURI(webkitDownloadGetRequest(d)))}
		connect(d, "decide-destination", cbDownloadDestination, 0)
		connect(d, "finished", cbDownloadFinished, 0)
		connect(d, "failed", cbDownloadFailed, 0)
	})
	cbDownloadDestination = purego.NewCallback(func(d, suggested, data ptr) bool {
		dl := downloads[d]
		if dl == nil {
			return false
		}
		dl.path = dl.w.h.DownloadStarted(dl.url, goStr(suggested))
		if dl.path == "" {
			delete(downloads, d)
			webkitDownloadCancel(d)
			return true
		}
		webkitDownloadSetAllowOverwrite(d, true)
		webkitDownloadSetDestination(d, cs((&url.URL{Scheme: "file", Path: dl.path}).String()))
		connect(d, "notify::estimated-progress", cbDownloadProgress, 0)
		return true
	})
	cbDownloadProgress = purego.NewCallback(func(d, pspec, data ptr) {
		if dl := downloads[d]; dl != nil && dl.path != "" {
			var total uint64
			if resp := webkitDownloadGetResponse(d); resp != 0 {
				total = webkitURIResponseGetContentLength(resp)
			}
			dl.w.h.DownloadProgress(dl.url, dl.path, int64(webkitDownloadGetReceivedDataLength(d)), int64(total))
		}
	})
	cbDownloadFailed = purego.NewCallback(func(d, gerr, data ptr) {
		if dl := downloads[d]; dl != nil {
			// GError: domain, code, then the message.
			dl.err = fmt.Errorf("mygo: download failed: %s", goStr(*(*ptr)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&gerr)), 8))))
		}
	})
	cbDownloadFinished = purego.NewCallback(func(d, data ptr) {
		if dl := downloads[d]; dl != nil {
			delete(downloads, d)
			dl.w.h.DownloadFinished(dl.url, dl.path, dl.err)
		}
	})
}

// hookDownloads listens to the downloads of the web context once.
func hookDownloads() {
	if !downloadsHooked {
		downloadsHooked = true
		connect(webkitWebContextGetDefault(), "download-started", cbDownloadStarted, 0)
	}
}

// downloadFor returns the WebKitDownload of url saved at path, or 0 when
// none is running.
func downloadFor(url, path string) ptr {
	for d, dl := range downloads {
		if dl.url == url && dl.path == path {
			return d
		}
	}
	return 0
}

func (w *window) PauseDownload(string, string) error {
	return fmt.Errorf("mygo: pausing a download: %w", platform.ErrUnsupported)
}

func (w *window) ResumeDownload(string, string) error {
	return fmt.Errorf("mygo: resuming a download: %w", platform.ErrUnsupported)
}

func (w *window) CancelDownload(url, path string) {
	if d := downloadFor(url, path); d != 0 {
		webkitDownloadCancel(d)
	}
}
