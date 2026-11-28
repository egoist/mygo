//go:build !js

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

func TestSchemeBodyLimits(t *testing.T) {
	if err := mygo.Protocol.SetBodyLimits(mygo.SchemeBodyLimits{MaxRequestBodyBytes: 8, MaxInFlightRequestBodyBytes: 8}); err != nil {
		t.Fatal(err)
	}
	defer mygo.Protocol.SetBodyLimits(mygo.SchemeBodyLimits{})
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var rejected atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload/{kind}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("kind") {
		case "hold":
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		case "reject":
			rejected.Store(true)
		}
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		fmt.Fprintf(w, "%d/%d", n, r.ContentLength)
	})
	if err := mygo.Protocol.Handle("uploadtest", mux); err != nil {
		t.Fatal(err)
	}
	defer mygo.Protocol.Unhandle("uploadtest")
	target := "uploadtest://localhost"
	if runtime.GOOS == "windows" {
		target = "http://uploadtest.localhost"
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Upload limits", Width: 300, Height: 200})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "window.run")
	eval := func(js, want string) {
		t.Helper()
		got, err := mygo.EvalAs[string](w.Page(), js)
		if err != nil || got != want {
			t.Fatalf("%s returned %q, %v; want %q", js, got, err, want)
		}
	}
	eval(`window.upload = async (kind, size) => {
   const r = await fetch("`+target+`/upload/" + kind, {method:"POST",body:"x".repeat(size)});
   return r.status + ":" + await r.text();
 }; return await upload("ok",8)`, "200:8/8")
	eval(`return await upload("reject",9)`, "413:")
	eval(`window.held = upload("hold",8); return "started"`, "started")
	wait.For(t, "held upload to enter handler", started)
	eval(`return await upload("reject",1)`, "503:")
	unblock()
	eval(`return await window.held`, "200:8/8")
	eval(`return await upload("ok",8)`, "200:8/8")
	if rejected.Load() {
		t.Fatal("handler received a rejected upload")
	}
}
