//go:build js

package mygo

import "errors"

// DownloadEvent is passed to Page.OnWillDownload listeners. Browsers show
// no web pages (see ipc_js.go): none is ever fired.
type DownloadEvent struct {
	Preventable
	URL           string
	SuggestedName string
	Path          string
	Download      *Download
}

// Download is a download: the handle DownloadEvent carries while it runs,
// and what Page.OnDownloadDone receives once it ended.
type Download struct {
	URL  string
	Path string
	Err  error
}

// OnProgress registers fn to run as the download's bytes arrive: there
// are none in browsers.
func (d *Download) OnProgress(fn func(received, total int64)) (off func()) {
	return func() {}
}

// Pause pauses a running download: there are none in browsers.
func (d *Download) Pause() error { return errors.New("mygo: no such download in progress") }

// Resume continues a paused download: there are none in browsers.
func (d *Download) Resume() error { return errors.New("mygo: no such download in progress") }

// Cancel stops a running download: there are none in browsers.
func (d *Download) Cancel() {}

// OnWillDownload is called when a page starts a download: never in
// browsers.
func (p *Page) OnWillDownload(fn func(e *DownloadEvent)) (off func()) { return func() {} }

// OnDownloadDone is called when a download ended: never in browsers.
func (p *Page) OnDownloadDone(fn func(d *Download)) (off func()) { return func() {} }

type downloadKey struct{ url, path string }

func (h *windowHandler) DownloadStarted(url, suggestedName string) string { return "" }
func (h *windowHandler) DownloadProgress(url, path string, received, total int64) {}
func (h *windowHandler) DownloadFinished(url, path string, err error)         {}