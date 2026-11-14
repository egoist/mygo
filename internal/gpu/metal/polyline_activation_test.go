//go:build darwin

package metal

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

// A fresh process keeps registration and the shared shader cache untouched by
// other tests. The physical GPU is unchanged: this tests the legacy capability
// gate, not whether a particular older driver hangs (#1566).
func TestCompactPolylineActivation(t *testing.T) {
	const child = "MYGO_COMPACT_ACTIVATION_CHILD"
	if os.Getenv(child) != "1" {
		cmd := exec.CommandContext(wait.Context(t), os.Args[0], "-test.run=^TestCompactPolylineActivation$", "-test.v")
		cmd.Env = append(os.Environ(), child+"=1")
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if polylineProgram != nil || instanceUpload != nil {
		t.Fatal("activation requires a fresh process")
	}
	r, err := newRenderer()
	if err != nil {
		unavailable(t, err)
	}
	defer r.Release()
	needSpecialized(t, r)
	r.canSpecialize, r.b.Classes = false, false
	s := compactScene(720)
	wantLegacy := renderRaw(t, r, s, false)
	EnablePolyline()
	if polylineProgram != nil {
		t.Fatal("registration queued compact compilation before checking the renderer")
	}
	if !bytes.Equal(renderRaw(t, r, s, false), wantLegacy) {
		t.Fatal("legacy registration changed pixels")
	}
	if polylineProgram != nil || len(shaders.byKey) != 0 {
		t.Fatal("unsupported renderer queued compact compilation")
	}
	t.Log("legacy first: registration and rendering queued no compact shader")

	// Hold the real worker before compilation. The first eligible renderer
	// must still draw and release without waiting for it.
	shaders.once.Do(func() {
		shaders.byKey = map[string]*shaderEntry{}
		shaders.device = createDevice()
		shaders.sem = make(chan struct{}, 1)
	})
	shaders.sem <- struct{}{}
	held := true
	defer func() {
		if held {
			<-shaders.sem
		}
	}()
	modern, err := newRenderer()
	if err != nil {
		t.Fatal(err)
	}
	defer modern.Release()
	instanceUpload = nil
	want := renderRaw(t, modern, s, false)
	instanceUpload = uploadPolylines
	done := make(chan struct{})
	go func() {
		defer close(done)
		if !bytes.Equal(renderRaw(t, modern, s, false), want) {
			t.Error("pending compilation changed pixels")
		}
		modern.Release()
	}()
	wait.For(t, "drawing and releasing while compilation is pending", done)
	e := polylineProgram
	if e == nil || len(shaders.byKey) != 1 {
		t.Fatal("first supported renderer did not queue exactly one shader")
	}
	select {
	case <-e.done:
		t.Fatal("worker semaphore did not hold compilation")
	default:
	}
	<-shaders.sem
	held = false
	wait.For(t, "shared shader after its first renderer was destroyed", e.done)
	if e.pipe == 0 {
		t.Fatalf("compact compile failed: %v", e.err)
	}
	next, err := newRenderer()
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	if !bytes.Equal(renderRaw(t, next, s, false), want) {
		t.Fatal("recreated renderer changed pixels")
	}
	if n, _ := compactDrawn(next); n != 1440 {
		t.Fatalf("recreated supported renderer compact=%d", n)
	}
	if !bytes.Equal(renderRaw(t, r, s, false), wantLegacy) {
		t.Fatal("legacy renderer changed pixels after compilation")
	}
	if n, _ := compactDrawn(r); n != 0 {
		t.Fatalf("unsupported renderer used %d compact instances", n)
	}
	if polylineProgram != e || len(shaders.byKey) != 1 {
		t.Fatal("renderer reuse queued another compact shader")
	}
	// A completed failure must also draw normally, without retrying or
	// waiting. Keep the real cache owned by its existing worker.
	failed := &shaderEntry{done: make(chan struct{})}
	close(failed.done)
	polylineProgram = failed
	defer func() { polylineProgram = e }()
	if !bytes.Equal(renderRaw(t, next, s, false), want) {
		t.Fatal("failed compilation changed pixels")
	}
	if polylineProgram != failed || len(shaders.byKey) != 1 {
		t.Fatal("failed compilation was retried")
	}
	t.Log("supported after legacy: pending draw/release, compile completion, recreate=1440, legacy after ready=0, failed fallback")
}
