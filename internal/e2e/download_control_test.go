package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

const downloadSize = 2 << 20

// serveBigDownload writes a 2 MiB attachment in paced chunks, slower than
// the engine's progress updates (WebView2 coalesces them to about twice a
// second), so a download of it is still running when its first progress
// arrives.
func serveBigDownload(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="big.bin"`)
	w.Header().Set("Content-Length", strconv.Itoa(downloadSize))
	chunk := make([]byte, 64<<10)
	for i := range chunk {
		chunk[i] = byte(i)
	}
	for sent := 0; sent < downloadSize; sent += len(chunk) {
		w.Write(chunk)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func checkDownloadContent(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || len(b) != downloadSize {
		t.Fatalf("saved %d bytes, %v, want %d", len(b), err, downloadSize)
	}
	for _, i := range []int{0, 1000, downloadSize / 2, downloadSize - 1} {
		if b[i] != byte(i%(64<<10)) {
			t.Fatalf("byte %d = %d, want %d", i, b[i], byte(i%(64<<10)))
		}
	}
}

// downloadPauseCase checks progress events, then pauses on the first one
// and resumes, and the file arrives whole. engine tells whether the
// webview's engine downloads the URL (a loopback server) or the core
// serves it (a custom scheme); only the engine's downloads may refuse to
// pause, as WebKitGTK does.
func downloadPauseCase(t *testing.T, url, name string, engine bool) {
	w := newWindow(t, mygo.WindowOptions{Title: "Download pause", Width: 300, Height: 200})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, `window.run`)
	dir := t.TempDir()
	var events, badTotal, outOfOrder atomic.Int32
	var last atomic.Int64
	var handle atomic.Pointer[mygo.Download]
	paused := make(chan error, 1)
	done := make(chan *mygo.Download, 1)
	w.Page().OnWillDownload(func(e *mygo.DownloadEvent) {
		e.Path = filepath.Join(dir, name)
		handle.Store(e.Download)
		e.Download.OnProgress(func(received, total int64) {
			if events.Add(1) == 1 {
				paused <- e.Download.Pause()
			}
			if total != downloadSize {
				badTotal.Add(1)
			}
			if received < last.Load() {
				outOfOrder.Add(1)
			}
			last.Store(received)
		})
	})
	w.Page().OnDownloadDone(func(d *mygo.Download) { done <- d })
	mustEval(t, w, fmt.Sprintf(`(location.href = %q, true)`, url))
	pauseErr := wait.For(t, "the first progress event", paused)
	switch {
	case pauseErr != nil && engine && runtime.GOOS == "linux":
		// WebKitGTK cannot pause; the download runs on.
	case pauseErr != nil:
		t.Errorf("Pause: %v", pauseErr)
	default:
		if err := handle.Load().Resume(); err != nil {
			t.Errorf("Resume: %v", err)
		}
	}
	d := wait.For(t, "the download to end", done)
	if d.Err != nil {
		t.Fatalf("download ended with %v", d.Err)
	}
	checkDownloadContent(t, d.Path)
	if events.Load() == 0 || badTotal.Load() != 0 || outOfOrder.Load() != 0 {
		t.Errorf("progress: %d events, %d wrong totals, %d out of order", events.Load(), badTotal.Load(), outOfOrder.Load())
	}
}

func downloadCancelCase(t *testing.T, url, name string) {
	w := newWindow(t, mygo.WindowOptions{Title: "Download cancel", Width: 300, Height: 200})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, `window.run`)
	dir := t.TempDir()
	var canceled atomic.Bool
	done := make(chan *mygo.Download, 1)
	w.Page().OnWillDownload(func(e *mygo.DownloadEvent) {
		e.Path = filepath.Join(dir, name)
		e.Download.OnProgress(func(received, total int64) {
			if !canceled.Swap(true) {
				e.Download.Cancel()
			}
		})
	})
	w.Page().OnDownloadDone(func(d *mygo.Download) { done <- d })
	mustEval(t, w, fmt.Sprintf(`(location.href = %q, true)`, url))
	d := wait.For(t, "the canceled download to end", done)
	if d.Err == nil {
		t.Error("the canceled download ended without an error")
	}
	if b, err := os.ReadFile(d.Path); err == nil && len(b) == downloadSize {
		t.Error("the canceled download saved the whole file")
	}
}

// TestDownloadProgressAndControl downloads a paced 2 MiB attachment both
// through the webview's engine (a loopback server, trusted in development)
// and through a custom scheme the core serves itself, checking progress
// events, pause and resume, and cancel on each path.
func TestDownloadProgressAndControl(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveBigDownload(w)
	}))
	defer srv.Close()
	engine := srv.URL + "/big.bin"
	t.Run("engine pause", func(t *testing.T) { downloadPauseCase(t, engine, "engine-paused.bin", true) })
	t.Run("engine cancel", func(t *testing.T) { downloadCancelCase(t, engine, "engine-canceled.bin") })
	t.Run("scheme pause", func(t *testing.T) { downloadPauseCase(t, "big.bin", "scheme-paused.bin", false) })
	t.Run("scheme cancel", func(t *testing.T) { downloadCancelCase(t, "big.bin", "scheme-canceled.bin") })
}