//go:build !js

package mygo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

type sourceService struct {
	started chan struct{}
}

func (s *sourceService) Collect(ctx context.Context, src *Source[string]) (string, error) {
	if s.started != nil {
		close(s.started)
	}
	var out []string
	for v, err := range src.Values() {
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		out = append(out, v)
	}
	return strings.Join(out, ","), nil
}

func (s *sourceService) CloseEarly(ctx context.Context, src *Source[string]) error {
	if s.started != nil {
		close(s.started)
	}
	src.Close()
	_, err := src.Recv()
	return err
}

// sourceWindowSetup gives the window a page context and keeps its outbox
// from flushing, so a test reads what the call queued.
func sourceWindowSetup(w *Window) {
	w.resetPage()
	w.outMu.Lock()
	w.flushing = true
	w.outMu.Unlock()
}

// sourceOutbox reads the messages the window queued.
func sourceOutbox(w *Window) string {
	w.outMu.Lock()
	defer w.outMu.Unlock()
	var b []byte
	for _, m := range w.outbox {
		b = m.appendTo(b)
	}
	return string(b)
}

// startSourceCall runs a call on its own goroutine; the channel closes
// when it ends.
func startSourceCall(w *Window, raw string) chan struct{} {
	done := make(chan struct{})
	go func() {
		handleCall(w, w.pageContext(), raw, true)
		close(done)
	}()
	return done
}

func TestSourceEndToEnd(t *testing.T) {
	svc := &sourceService{started: make(chan struct{})}
	BindAs("srcEnd", svc)
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcEnd.Collect","a":[7]}`)
	wait.For(t, "the call to start", svc.started)
	w.handleMessage(`{"t":"src-push","c":7,"k":"tok","p":"a"}`)
	w.handleMessage(`{"t":"src-push","c":7,"k":"tok","p":"b"}`)
	w.handleMessage(`{"t":"src-push","c":7,"k":"stale","p":"wrong"}`)
	w.handleMessage(`{"t":"src-end","c":7,"k":"tok"}`)
	wait.For(t, "the call to end", done)
	out := sourceOutbox(w)
	if !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"v":"a,b"`) {
		t.Errorf("outbox = %q, want the reply a,b", out)
	}
	// The source closed with the method: a later value reaches nothing.
	w.handleMessage(`{"t":"src-push","c":7,"k":"tok","p":"late"}`)
	w.mu.Lock()
	n := len(w.sources)
	w.mu.Unlock()
	if n != 0 {
		t.Errorf("sources left after the call: %d", n)
	}
}

func TestSourceEndedBeforeTheCall(t *testing.T) {
	svc := &sourceService{}
	BindAs("srcEarly", svc)
	w := &Window{}
	sourceWindowSetup(w)
	w.sourceMessage(7, "tok", nil, true)
	wait.For(t, "the call to end", startSourceCall(w, `{"id":1,"k":"tok","m":"srcEarly.Collect","a":[7]}`))
	out := sourceOutbox(w)
	if !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"v":""`) {
		t.Errorf("outbox = %q, want an empty result", out)
	}
}

func TestSourceValuesBeforeTheCall(t *testing.T) {
	svc := &sourceService{}
	BindAs("srcEarlyValues", svc)
	w := &Window{}
	sourceWindowSetup(w)
	// The page's values can arrive before the call's goroutine makes its
	// source: they wait for it, in order.
	w.sourceMessage(7, "tok", []byte(`"a"`), false)
	w.sourceMessage(7, "tok", []byte(`"b"`), false)
	w.sourceMessage(7, "tok", nil, true)
	wait.For(t, "the call to end", startSourceCall(w, `{"id":1,"k":"tok","m":"srcEarlyValues.Collect","a":[7]}`))
	out := sourceOutbox(w)
	if !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"v":"a,b"`) {
		t.Errorf("outbox = %q, want the reply a,b", out)
	}
}

func TestSourceInvalidArgument(t *testing.T) {
	svc := &sourceService{}
	BindAs("srcInvalid", svc)
	w := &Window{}
	sourceWindowSetup(w)
	wait.For(t, "the call to end", startSourceCall(w, `{"id":1,"k":"tok","m":"srcInvalid.Collect","a":[0]}`))
	if out := sourceOutbox(w); !strings.Contains(out, "not a Source") {
		t.Errorf("outbox = %q, want the invalid-argument error", out)
	}
}

func TestSourceCloseByGo(t *testing.T) {
	svc := &sourceService{started: make(chan struct{})}
	BindAs("srcClose", svc)
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcClose.CloseEarly","a":[7]}`)
	wait.For(t, "the call to start", svc.started)
	wait.For(t, "the call to end", done)
	out := sourceOutbox(w)
	if !strings.Contains(out, "source closed") {
		t.Errorf("outbox = %q, want the closed-source error", out)
	}
	if !strings.Contains(out, `"t":"src-close"`) {
		t.Errorf("outbox = %q, want a src-close for the page", out)
	}
}

func TestSourceCanceledContext(t *testing.T) {
	svc := &sourceService{started: make(chan struct{})}
	BindAs("srcCancel", svc)
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcCancel.Collect","a":[7]}`)
	wait.For(t, "the call to start", svc.started)
	w.mu.Lock()
	cancel := w.pageCancel
	w.mu.Unlock()
	cancel()
	wait.For(t, "the call to end", done)
	if out := sourceOutbox(w); !strings.Contains(out, "context canceled") {
		t.Errorf("outbox = %q, want the context error", out)
	}
}

func TestSourceAcknowledgments(t *testing.T) {
	svc := &sourceService{started: make(chan struct{})}
	BindAs("srcAck", svc)
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcAck.Collect","a":[7]}`)
	wait.For(t, "the call to start", svc.started)
	big := strings.Repeat("x", 600<<10)
	raw, _ := json.Marshal(big)
	for range 2 {
		w.handleMessage(`{"t":"src-push","c":7,"k":"tok","p":` + string(raw) + `}`)
	}
	w.handleMessage(`{"t":"src-end","c":7,"k":"tok"}`)
	wait.For(t, "the call to end", done)
	// The method took over half a MiB: an acknowledgment went to the page.
	if out := sourceOutbox(w); !strings.Contains(out, `"t":"src-ack"`) {
		t.Errorf("outbox = %q, want a src-ack", out)
	}
}