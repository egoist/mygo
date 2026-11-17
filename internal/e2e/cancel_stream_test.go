package e2e

import (
	"context"
	"errors"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

var logAuditStopped = make(chan error, 1)

func (Streams) Tail(ctx context.Context, lines *mygo.Channel[string]) error {
	if err := lines.Send("service started"); err != nil {
		return err
	}
	<-ctx.Done()
	logAuditStopped <- ctx.Err()
	return ctx.Err()
}

func TestCancelStreamingCall(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "window.run")
	mustEval(t, w, `(() => {
  const start = document.createElement('button');
  start.textContent = 'Start logs';
  start.disabled = true;
  const stop = document.createElement('button');
  stop.textContent = 'Stop logs';
  const ac = new AbortController();
  const lines = mygo.channel();
  stop.onclick = () => ac.abort('stopped by user');
  document.body.append(start, stop);
  window.audit = { lines, start, stop, count: 0, error: null };
  mygo.callWith('Streams.Tail', [lines], {signal: ac.signal})
    .catch(error => audit.error = error);
  void (async () => {
    try { for await (const line of lines) audit.count++; }
    finally { start.disabled = false; }
  })();
  return true;
 })()`)
	waitFor(t, w, "audit.count > 0 || audit.error !== null")
	if err := mustEval(t, w, "audit.error && String(audit.error)"); err != nil {
		t.Fatalf("stream did not start: %v", err)
	}
	mustEval(t, w, "(() => { audit.stop.click(); return true; })()")
	if err := wait.For(t, "Go log stream to stop", logAuditStopped); !errors.Is(err, context.Canceled) {
		t.Fatalf("Go stream ended with %v", err)
	}
	got := mustEval(t, w, `(async () => {
  await mygo.call('Greeter.Greet', 'barrier');
  return JSON.stringify({error: audit.error, channelClosed: audit.lines.closed, canRestart: !audit.start.disabled});
 })()`)
	const want = `{"error":"stopped by user","channelClosed":true,"canRestart":true}`
	if got != want {
		t.Fatalf("after stop: %s, want %s", got, want)
	}
	got = mustEval(t, w, `(async () => {
	  const lines = mygo.channel();
	  const ac = new AbortController();
	  const result = mygo.callWith("Streams.Count", [1, lines], { signal: ac.signal });
	  const received = [];
	  for await (const line of lines) received.push(line);
	  await result;
	  return JSON.stringify(received);
	})()`)
	if got != "[0]" {
		t.Fatalf("restarted stream returned %v, want [0]", got)
	}
}
