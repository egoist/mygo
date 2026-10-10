package mygo

import (
	"context"
	"encoding/json/v2"
	"sync"
	"unsafe"
)

// pageLink is the IPC state of a page that runs the bridge: the page
// shown in a window, or a browser tab that `mygo dev` serves the frontend
// to (see devbrowser.go). Calls, the channels they stream, and the messages
// queued for the page belong to it, and go away with the page.
type pageLink struct {
	// w is the window calls report as theirs (CallerWindow); nil for a
	// browser tab opened while no window shows a page.
	w *Window
	// name is how logs refer to the page.
	name string
	// deliver runs a batch of messages on the page, a script shaped like
	// __mygo.receive([...]). It is called on the main thread.
	deliver func(js string)

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	// channels are those of the current page, by the page's id for them.
	channels map[int64]*channel
	// closedEarly holds the channels the current page closed before
	// their calls made them, with the page's token.
	closedEarly map[int64]string

	outMu    sync.Mutex
	outbox   []message
	flushing bool
	// held keeps events until the page's DOM is ready, so events sent right
	// after creating a window or during a navigation are not lost.
	held     []message
	domReady bool
}

func newPageLink(w *Window, name string, deliver func(js string)) *pageLink {
	l := &pageLink{w: w, name: name, deliver: deliver}
	l.reset()
	return l
}

// reset starts a new page: a new context, the previous one being
// canceled, and events held until its DOM is ready.
func (l *pageLink) reset() {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), callerKey{}, l.w))
	l.outMu.Lock()
	l.domReady = false
	l.outMu.Unlock()
	l.mu.Lock()
	prev := l.cancel
	l.ctx, l.cancel = ctx, cancel
	l.channels = nil // they close with the previous page's context
	l.closedEarly = nil
	l.mu.Unlock()
	if prev != nil {
		prev()
	}
}

// context returns the context of the current page. It is canceled when
// the page goes away.
func (l *pageLink) context() context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ctx
}

// end cancels the context of the current page, for good.
func (l *pageLink) end() {
	l.mu.Lock()
	cancel := l.cancel
	l.mu.Unlock()
	cancel()
}

// maxHeldEvents bounds the events kept for a page that never becomes ready
// (e.g. an image or a failed load).
const maxHeldEvents = 1024

// enqueue schedules a message for the page. Messages are batched into a
// single script evaluation per main loop iteration. Events are held until
// the DOM of the page is ready; replies to calls are sent right away since
// the page is waiting for them.
func (l *pageLink) enqueue(msg message, event bool) {
	l.outMu.Lock()
	if event && !l.domReady {
		if len(l.held) == maxHeldEvents {
			l.held = l.held[1:]
		}
		l.held = append(l.held, msg)
		l.outMu.Unlock()
		return
	}
	l.outbox = append(l.outbox, msg)
	schedule := !l.flushing
	l.flushing = true
	l.outMu.Unlock()
	if schedule {
		postMain(l.flush)
	}
}

// ready records that the page's DOM is ready: the events held for it are
// sent with the next flush.
func (l *pageLink) ready() {
	l.outMu.Lock()
	l.domReady = true
	l.outbox = append(l.outbox, l.held...)
	l.held = nil
	l.outMu.Unlock()
}

func (l *pageLink) flush() {
	l.outMu.Lock()
	msgs := l.outbox
	l.outbox = nil
	l.flushing = false
	l.outMu.Unlock()
	if len(msgs) == 0 {
		return
	}
	size := 64
	for _, m := range msgs {
		size += m.len() + 1
	}
	js := make([]byte, 0, size)
	// A script shaped like a.b(JSON) runs without being compiled: WebKit's
	// JavaScriptCore parses the value as JSON unless the inspector is on,
	// several times faster. Pages without the runtime only throw.
	js = append(js, receivePrefix...)
	for i, m := range msgs {
		if i > 0 {
			js = append(js, ',')
		}
		js = m.appendTo(js)
	}
	js = append(js, receiveSuffix...)
	// js is not used again, so the script can share its memory.
	l.deliver(unsafe.String(unsafe.SliceData(js), len(js)))
}

// The script flush runs on the page wraps the messages in these.
const (
	receivePrefix = "__mygo.receive(["
	receiveSuffix = "])"
)

// linkMessage is a message of the bridge other than a call, decoded.
type linkMessage struct {
	T string  `json:"t"`
	X float64 `json:"x"` // drop
	Y float64 `json:"y"`
	C int64   `json:"c"` // channels
	K string  `json:"k"`
	N int64   `json:"n"`
}

// receive handles the messages every page sends: calls, and the
// acknowledgments and closes of channels. Others are decoded into m, for
// the caller, and receive reports false. It runs on the main thread for
// windows.
func (l *pageLink) receive(msg string, trusted bool, m *linkMessage) bool {
	// Calls are decoded and executed off the main thread. The page context
	// is captured here so a navigation cannot slip in between.
	if len(msg) > 12 && msg[:12] == `{"t":"call",` {
		go handleCall(l, l.context(), msg, trusted)
		return true
	}
	if err := json.Unmarshal(stringBytes(msg), m); err != nil {
		return true
	}
	switch m.T {
	case "chan-ack":
		if c := l.channel(m.C, m.K); c != nil {
			c.ack(m.N)
		}
	case "chan-close":
		l.pageClosedChannel(m.C, m.K)
	default:
		return false
	}
	return true
}
