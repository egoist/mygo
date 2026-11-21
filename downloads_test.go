//go:build !js

package mygo

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

func TestDownloadEvents(t *testing.T) {
	dir := t.TempDir()
	App.SetPath(PathDownloads, dir)
	defer App.SetPath(PathDownloads, "")
	w, fw := testWindow(t, WindowOptions{})

	// The default path is the suggested name in Downloads, numbered.
	os.WriteFile(filepath.Join(dir, "report.csv"), nil, 0o644)
	start := func(url, name string) string {
		return onMainValue(func() string { return fw.H.DownloadStarted(url, name) })
	}
	if got := start("https://x/r", "report.csv"); got != filepath.Join(dir, "report (2).csv") {
		t.Errorf("path = %q", got)
	}
	if got := start("https://x/r", "../../etc/passwd"); got != filepath.Join(dir, "passwd") {
		t.Errorf("unsafe name: path = %q", got)
	}
	off := w.Page().OnWillDownload(func(e *DownloadEvent) { e.PreventDefault() })
	if got := start("https://x/r", "a.txt"); got != "" {
		t.Errorf("a prevented download got path %q", got)
	}
	off()

	// Custom schemes are downloaded from their handler.
	if err := Protocol.HandleFunc("dltest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="data.json"`)
		w.Write([]byte(`{"ok":true}`))
	}); err != nil {
		t.Fatal(err)
	}
	defer Protocol.Unhandle("dltest")
	done := make(chan *Download, 1)
	defer w.Page().OnDownloadDone(func(d *Download) { done <- d })()
	onMain(func() { fw.H.SchemeDownload("dltest://localhost/export") })
	select {
	case d := <-done:
		if d.Err != nil || filepath.Base(d.Path) != "data.json" {
			t.Fatalf("download = %+v", d)
		}
		if b, _ := os.ReadFile(d.Path); string(b) != `{"ok":true}` {
			t.Errorf("saved %q", b)
		}
	case <-time.After(wait.Guard):
		t.Fatal("the scheme download did not finish")
	}
}

func TestDownloadProgressAndControl(t *testing.T) {
	dir := t.TempDir()
	App.SetPath(PathDownloads, dir)
	defer App.SetPath(PathDownloads, "")
	w, fw := testWindow(t, WindowOptions{})

	var handle *Download
	off := w.Page().OnWillDownload(func(e *DownloadEvent) { handle = e.Download })
	defer off()
	path := onMainValue(func() string { return fw.H.DownloadStarted("https://x/big", "big.bin") })
	if handle == nil || handle.URL != "https://x/big" || handle.Path != path {
		t.Fatalf("DownloadEvent.Download = %+v, want the download of https://x/big at %q", handle, path)
	}

	var progress [][2]int64
	handle.OnProgress(func(received, total int64) { progress = append(progress, [2]int64{received, total}) })
	onMain(func() { fw.H.DownloadProgress("https://x/big", path, 100, 500) })
	onMain(func() { fw.H.DownloadProgress("https://x/big", path, 500, 500) })
	onMain(func() { fw.H.DownloadProgress("https://x/other", path, 1, 1) }) // not this download
	if len(progress) != 2 || progress[0] != [2]int64{100, 500} || progress[1] != [2]int64{500, 500} {
		t.Errorf("progress = %v, want [[100 500] [500 500]]", progress)
	}

	if err := handle.Pause(); err != nil {
		t.Errorf("Pause: %v", err)
	}
	if err := handle.Resume(); err != nil {
		t.Errorf("Resume: %v", err)
	}
	handle.Cancel()
	controls := onMainValue(func() []string {
		return append([]string(nil), fw.DownloadControls...)
	})
	want := []string{"pause https://x/big " + path, "resume https://x/big " + path, "cancel https://x/big " + path}
	if len(controls) != 3 || controls[0] != want[0] || controls[1] != want[1] || controls[2] != want[2] {
		t.Errorf("controls = %v, want %v", controls, want)
	}

	done := make(chan *Download, 1)
	defer w.Page().OnDownloadDone(func(d *Download) { done <- d })()
	stopped := errors.New("stopped")
	onMain(func() { fw.H.DownloadFinished("https://x/big", path, stopped) })
	select {
	case d := <-done:
		if d != handle {
			t.Error("OnDownloadDone got a different object than DownloadEvent.Download")
		}
		if !errors.Is(d.Err, stopped) || !errors.Is(d.Err, ErrDownloadCanceled) {
			t.Errorf("Err = %v, want %v and ErrDownloadCanceled", d.Err, stopped)
		}
	case <-time.After(wait.Guard):
		t.Fatal("the download did not end")
	}
	// Progress of an ended download reaches no listener.
	onMain(func() { fw.H.DownloadProgress("https://x/big", path, 600, 500) })
	if len(progress) != 2 {
		t.Errorf("progress after the end = %v", progress)
	}
}

// A handler that panics while a download is served from it fails the
// download, before it started or with part of the file written, which is
// removed. The app goes on.
func TestDownloadSchemePanic(t *testing.T) {
	dir := t.TempDir()
	App.SetPath(PathDownloads, dir)
	defer App.SetPath(PathDownloads, "")
	w, fw := testWindow(t, WindowOptions{})
	if err := Protocol.HandleFunc("dlpanic", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/part" {
			w.Header().Set("Content-Disposition", `attachment; filename="part.bin"`)
			w.Write([]byte("half"))
		}
		panic("boom")
	}); err != nil {
		t.Fatal(err)
	}
	defer Protocol.Unhandle("dlpanic")
	done := make(chan *Download, 1)
	defer w.Page().OnDownloadDone(func(d *Download) { done <- d })()
	for _, tt := range []struct{ url, path string }{
		{"dlpanic://localhost/none", ""},
		{"dlpanic://localhost/part", filepath.Join(dir, "part.bin")},
	} {
		onMain(func() { fw.H.SchemeDownload(tt.url) })
		select {
		case d := <-done:
			if d.Err == nil || d.Path != tt.path {
				t.Errorf("%s: download = %+v, want an error and the path %q", tt.url, d, tt.path)
			}
		case <-time.After(wait.Guard):
			t.Fatalf("%s: the download did not finish", tt.url)
		}
		if tt.path != "" {
			if _, err := os.Stat(tt.path); !os.IsNotExist(err) {
				t.Errorf("%s: the partial file was left behind (%v)", tt.url, err)
			}
		}
	}
}

// slowScheme serves size bytes in paced chunks from proto, so a download
// of it is still running when its first progress arrives.
func slowScheme(t *testing.T, proto string, size int) {
	t.Helper()
	if err := Protocol.HandleFunc(proto, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="big.bin"`)
		chunk := make([]byte, 64<<10)
		for sent := 0; sent < size; sent += len(chunk) {
			w.Write(chunk)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Protocol.Unhandle(proto) })
}

func TestSchemeDownloadCanceled(t *testing.T) {
	dir := t.TempDir()
	App.SetPath(PathDownloads, dir)
	defer App.SetPath(PathDownloads, "")
	w, fw := testWindow(t, WindowOptions{})
	slowScheme(t, "dlcancel", 512<<10)
	var canceled atomic.Bool
	first := make(chan struct{}, 1)
	off := w.Page().OnWillDownload(func(e *DownloadEvent) {
		e.Path = filepath.Join(dir, "big.bin")
		e.Download.OnProgress(func(received, total int64) {
			if !canceled.Swap(true) {
				e.Download.Cancel()
				first <- struct{}{}
			}
		})
	})
	defer off()
	done := make(chan *Download, 1)
	defer w.Page().OnDownloadDone(func(d *Download) { done <- d })()
	onMain(func() { fw.H.SchemeDownload("dlcancel://localhost/file") })
	wait.For(t, "the first progress", first)
	d := wait.For(t, "the canceled download to end", done)
	if !errors.Is(d.Err, ErrDownloadCanceled) {
		t.Errorf("Err = %v, want ErrDownloadCanceled", d.Err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "big.bin")); err == nil && len(b) == 512<<10 {
		t.Error("the canceled download saved the whole file")
	}
}

func TestSchemeDownloadPausedWindowClose(t *testing.T) {
	dir := t.TempDir()
	App.SetPath(PathDownloads, dir)
	defer App.SetPath(PathDownloads, "")
	w, fw := testWindow(t, WindowOptions{})
	closed := make(chan struct{})
	if err := Protocol.HandleFunc("dlpause", func(rw http.ResponseWriter, r *http.Request) {
		defer close(closed)
		rw.Header().Set("Content-Disposition", `attachment; filename="big.bin"`)
		chunk := make([]byte, 64<<10)
		for sent := 0; sent < 512<<10; sent += len(chunk) {
			rw.Write(chunk)
			if f, ok := rw.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer Protocol.Unhandle("dlpause")
	paused := make(chan struct{}, 1)
	off := w.Page().OnWillDownload(func(e *DownloadEvent) {
		e.Path = filepath.Join(dir, "big.bin")
		e.Download.OnProgress(func(received, total int64) {
			select {
			case <-paused:
			default:
				if err := e.Download.Pause(); err == nil {
					close(paused)
				}
			}
		})
	})
	defer off()
	onMain(func() { fw.H.SchemeDownload("dlpause://localhost/file") })
	wait.For(t, "the first progress", paused)
	// The handler is blocked in Write: closing the window must let it go.
	w.Destroy()
	wait.For(t, "the handler to return", closed)
}
