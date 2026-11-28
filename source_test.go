//go:build !js

package mygo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

type sourceService struct {
	started chan struct{}
}

// The tests share one bound service: binding a second under the same name
// panics, and -count runs them again in the same process.
var (
	srcOnce sync.Once
	srcSvc  = &sourceService{}
)

func sourceServiceFor(t *testing.T) *sourceService {
	t.Helper()
	srcOnce.Do(func() { BindAs("srcTest", srcSvc) })
	srcSvc.started = nil
	return srcSvc
}

func (s *sourceService) TakeOne(ctx context.Context, src *Source[string]) (string, error) {
	if s.started != nil {
		close(s.started)
	}
	return src.Recv()
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
	svc := sourceServiceFor(t)
	svc.started = make(chan struct{})
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`)
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
	n := 0
	if st, _ := w.sourceStates.(*sourceStore); st != nil {
		n = len(st.sources)
	}
	w.mu.Unlock()
	if n != 0 {
		t.Errorf("sources left after the call: %d", n)
	}
}

func TestSourceEndedBeforeTheCall(t *testing.T) {
	sourceServiceFor(t)
	w := &Window{}
	sourceWindowSetup(w)
	w.sourceMessage(7, "tok", nil, true)
	wait.For(t, "the call to end", startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`))
	out := sourceOutbox(w)
	if !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"v":""`) {
		t.Errorf("outbox = %q, want an empty result", out)
	}
}

func TestSourceValuesBeforeTheCall(t *testing.T) {
	sourceServiceFor(t)
	w := &Window{}
	sourceWindowSetup(w)
	// The page's values can arrive before the call's goroutine makes its
	// source: they wait for it, in order.
	w.sourceMessage(7, "tok", []byte(`"a"`), false)
	w.sourceMessage(7, "tok", []byte(`"b"`), false)
	w.sourceMessage(7, "tok", nil, true)
	wait.For(t, "the call to end", startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`))
	out := sourceOutbox(w)
	if !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"v":"a,b"`) {
		t.Errorf("outbox = %q, want the reply a,b", out)
	}
}

func TestSourceInvalidArgument(t *testing.T) {
	sourceServiceFor(t)
	w := &Window{}
	sourceWindowSetup(w)
	wait.For(t, "the call to end", startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[0]}`))
	if out := sourceOutbox(w); !strings.Contains(out, "not a Source") {
		t.Errorf("outbox = %q, want the invalid-argument error", out)
	}
}

func TestSourceCloseByGo(t *testing.T) {
	svc := sourceServiceFor(t)
	svc.started = make(chan struct{})
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.CloseEarly","a":[7]}`)
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
	svc := sourceServiceFor(t)
	svc.started = make(chan struct{})
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`)
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
	svc := sourceServiceFor(t)
	svc.started = make(chan struct{})
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`)
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
func TestSourceValuesAfterGoClosed(t *testing.T) {
	svc := sourceServiceFor(t)
	svc.started = make(chan struct{})
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.TakeOne","a":[7]}`)
	wait.For(t, "the call to start", svc.started)
	w.handleMessage(`{"t":"src-push","c":7,"k":"tok","p":"only"}`)
	wait.For(t, "the call to end", done)
	// The page still had values in flight: they are dropped, not kept for
	// a source that is over until the page navigates.
	for range 10 {
		w.handleMessage(`{"t":"src-push","c":7,"k":"tok","p":"late"}`)
	}
	w.mu.Lock()
	st, _ := w.sourceStates.(*sourceStore)
	early, gone := 0, false
	if st != nil {
		early, gone = len(st.early), st.gone[7]
	}
	w.mu.Unlock()
	if early != 0 || !gone {
		t.Errorf("after the end: %d early sources, gone=%v, want none and gone", early, gone)
	}
}

// A page sending more than flow control lets it hold for a source whose
// call never comes fails that source instead of dropping its values,
// which would leave its call waiting forever.
func TestSourceFloodFailsTheSource(t *testing.T) {
	sourceServiceFor(t)
	w := &Window{}
	sourceWindowSetup(w)
	big := make([]byte, 64<<10)
	for range 50 { // past what flow control lets a page hold
		w.sourceMessage(7, "tok", big, false)
	}
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`)
	wait.For(t, "the call to end", done)
	if out := sourceOutbox(w); !strings.Contains(out, "source closed") {
		t.Errorf("outbox = %q, want the method to see the source closed", out)
	}
}

// The values a bridge sends straight after its call — many small ones,
// within the window's bytes — are not a flood: the call must get them.
func TestSourceManySmallEarlyValues(t *testing.T) {
	sourceServiceFor(t)
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`)
	for range 3000 {
		w.handleMessage(`{"t":"src-push","c":7,"k":"tok","p":"x"}`)
	}
	w.handleMessage(`{"t":"src-end","c":7,"k":"tok"}`)
	wait.For(t, "the call to end", done)
	out := sourceOutbox(w)
	if !strings.Contains(out, `"ok":true`) || strings.Contains(out, "source closed") {
		t.Errorf("outbox = %q, want the method to get every value", out)
	}
}

// A page that fills its flow window before its source exists, and ends,
// is not a flood: the end comes outside flow control, and Go's UTF-8
// bytes count more than the page's UTF-16 units.
func TestSourceEarlyWindowEnd(t *testing.T) {
	sourceServiceFor(t)
	w := &Window{}
	sourceWindowSetup(w)
	value := append(append([]byte(`"`), bytes.Repeat([]byte("A"), 64<<10)...), '"')
	for range 17 { // over a window by the page's count
		w.sourceMessage(7, "tok", value, false)
	}
	w.sourceMessage(7, "tok", nil, true)
	wait.For(t, "the call to end", startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`))
	if out := sourceOutbox(w); !strings.Contains(out, `"ok":true`) {
		t.Errorf("outbox = %q, want the method to get the values", out[:200])
	}
}

// The page numbers its streams from 1 on every load: a source that closes
// after a navigation must not fail the new page's source of the same id.
func TestSourceIdReusedAfterNavigation(t *testing.T) {
	svc := sourceServiceFor(t)
	svc.started = make(chan struct{})
	w := &Window{}
	sourceWindowSetup(w)
	done := startSourceCall(w, `{"id":1,"k":"tok","m":"srcTest.Collect","a":[7]}`)
	wait.For(t, "the first call to start", svc.started)
	w.resetPage() // the page navigates: the old call's context is canceled
	wait.For(t, "the old call to end", done)
	svc.started = make(chan struct{})
	done = startSourceCall(w, `{"id":2,"k":"tok","m":"srcTest.Collect","a":[7]}`)
	wait.For(t, "the new call to start", svc.started)
	w.handleMessage(`{"t":"src-push","c":7,"k":"tok","p":"a"}`)
	w.handleMessage(`{"t":"src-end","c":7,"k":"tok"}`)
	wait.For(t, "the new call to end", done)
	if out := sourceOutbox(w); !strings.Contains(out, `"v":"a"`) {
		t.Errorf("outbox = %q, want the new page's call to get its value", out)
	}
}
