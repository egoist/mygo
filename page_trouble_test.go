//go:build !js

package mygo

import (
	"errors"
	"testing"
)

// TestPageLoadError checks what a page that failed to load tells the app:
// the URL, the code and the description, and the error they make.
func TestPageLoadError(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	var got *LoadError
	off := w.Page().OnDidFailLoad(func(err *LoadError) { got = err })
	defer off()
	onMain(func() { fw.H.LoadFailed("https://example.test/a", -3, "host not found") })
	if got == nil {
		t.Fatal("the page told of no failed load")
	}
	if got.URL != "https://example.test/a" || got.Code != -3 || got.Description != "host not found" {
		t.Fatalf("load error = %+v", got)
	}
	if want := "mygo: failed to load https://example.test/a: host not found (-3)"; got.Error() != want {
		t.Errorf("error text = %q, want %q", got.Error(), want)
	}
}

// TestPageEvalError checks the error a script that throws makes: the
// message the page gave, as its own error.
func TestPageEvalError(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	fw.AsyncFunction = func(string) (string, error) { return `{"ok":false,"e":"boom"}`, nil }
	t.Cleanup(func() { fw.AsyncFunction = nil })
	_, err := w.Page().Eval("boom()")
	var evalErr *EvalError
	if !errors.As(err, &evalErr) {
		t.Fatalf("Eval = %v, want an EvalError", err)
	}
	if evalErr.Message != "boom" {
		t.Errorf("message = %q, want %q", evalErr.Message, "boom")
	}
	if want := "mygo: javascript error: boom"; evalErr.Error() != want {
		t.Errorf("error text = %q, want %q", evalErr.Error(), want)
	}
}

// TestPageRenderProcessGone checks that a page whose web content process
// crashed or was killed tells the app why, until it stops listening.
func TestPageRenderProcessGone(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	var reasons []string
	off := w.Page().OnRenderProcessGone(func(reason string) { reasons = append(reasons, reason) })
	onMain(func() { fw.H.RenderProcessGone("crashed") })
	if len(reasons) != 1 || reasons[0] != "crashed" {
		t.Fatalf("OnRenderProcessGone heard %q", reasons)
	}
	off()
	onMain(func() { fw.H.RenderProcessGone("killed") })
	if len(reasons) != 1 {
		t.Errorf("the listener turned off heard %q", reasons[1:])
	}
}
