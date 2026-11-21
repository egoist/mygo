//go:build !js

package mygo

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"iter"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
)

// Source receives values of type T that the page sends to a bound method,
// the other direction of Channel: an upload's chunks, a recording's
// samples, a long text as it is typed. The method takes it as a parameter:
//
//	// Save writes what the page sends to a file.
//	func (Files) Save(ctx context.Context, name string, data *mygo.Source[[]byte]) error {
//		f, err := os.Create(name)
//		if err != nil {
//			return err
//		}
//		defer f.Close()
//		for chunk, err := range data.Values() {
//			if errors.Is(err, io.EOF) {
//				break
//			}
//			if err != nil {
//				return err
//			}
//			if _, err := f.Write(chunk); err != nil {
//				return err
//			}
//		}
//		return nil
//	}
//
// and the page passes a Source of mygo-runtime in its place, which it
// fills and closes:
//
//	const data = new Source<string>();
//	const done = Files.save("report.bin", data);
//	for (const chunk of chunks) await data.send(btoa(chunk));
//	data.close();
//	await done;
//
// A Go []byte parameter crosses as a base64 string, as everywhere in the
// JSON API: the page encodes its chunks.
//
// Values arrive in order. Recv returns io.EOF once the page closed the
// source and its values are taken. The source closes when the method
// returns, or earlier with Close, after which the page's send rejects.
// Navigating away and closing the window cancel the call, which Recv
// reports as the context's error.
//
// send waits while the method has more than a MiB of values yet to take,
// so a fast page does not pile them up in memory.
//
// Sources are parameters of bound methods only, never parts of other
// values.
type Source[T any] struct {
	s *source
}

// ErrSourceClosed is returned by Source.Recv once the source is closed
// without the page ending it.
var ErrSourceClosed = errors.New("mygo: source closed")

// Recv returns the next value the page sent. It waits for one, and returns
// io.EOF once the page closed the source and its values are taken, or the
// call's context error once the page went away.
func (s *Source[T]) Recv() (T, error) {
	var v T
	if s.s == nil {
		return v, ErrSourceClosed
	}
	raw, err := s.s.recv()
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(raw, &v, decodeOptions); err != nil {
		return v, fmt.Errorf("mygo: cannot decode source value: %w", err)
	}
	return v, nil
}

// Values iterates the source: each value the page sent, then the error
// that ended it, io.EOF for a clean end.
//
//	for v, err := range data.Values() {
//		if err != nil {
//			return err
//		}
//		use(v)
//	}
func (s *Source[T]) Values() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for {
			v, err := s.Recv()
			if !yield(v, err) || err != nil {
				return
			}
		}
	}
}

// Close closes the source, after which the page's send rejects. It is
// called when the method returns.
func (s *Source[T]) Close() {
	if s.s != nil {
		s.s.close(closedByGo)
	}
}

func (s *Source[T]) init(c *source) { s.s = c }

// sourceHooks are what method.call and handleMessage reach sources
// through, set by *Source[T].valueType when Bind names the type: an app
// without a Source parameter links neither their code nor the sources'.
type sourceHooks struct {
	newSource func(w *Window, page, ctx context.Context, id int64, token string) *source
	message   func(w *Window, id int64, token string, raw []byte, end bool)
	close     func(srcs []*source)
}

var sourceHooksPtr atomic.Pointer[sourceHooks]

// valueType tells Bind what a Source carries, and links what the sources
// need, so that only an app with a *Source[T] parameter links them.
func (*Source[T]) valueType() reflect.Type {
	if sourceHooksPtr.Load() == nil {
		sourceHooksPtr.CompareAndSwap(nil, &sourceHooks{
			newSource: (*Window).newSource,
			message:   (*Window).sourceMessage,
			close:     closeSources,
		})
	}
	return reflect.TypeFor[T]()
}

// closeSources closes the sources a call made, before its result is sent.
func closeSources(srcs []*source) {
	for _, s := range srcs {
		s.close(closedByGo)
	}
}

// sourceParam is implemented by *Source[T] of every T.
type sourceParam interface {
	init(c *source)
	valueType() reflect.Type
}

var sourceParamType = reflect.TypeFor[sourceParam]()

// hasSource reports whether values of type t hold a Source.
func hasSource(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	if t.Implements(sourceParamType) || reflect.PointerTo(t).Implements(sourceParamType) {
		return true
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return hasSource(t.Elem(), seen)
	case reflect.Map:
		return hasSource(t.Key(), seen) || hasSource(t.Elem(), seen)
	case reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); (f.IsExported() || f.Anonymous) && hasSource(f.Type, seen) {
				return true
			}
		}
	}
	return false
}

// sourceWindow is how many bytes of values may wait for the method to
// take them. The method acknowledges what it took, once every half
// window.
const sourceWindow = 1 << 20

// source is a Source of any type: the stream of one call's argument, which
// the page fills.
type source struct {
	w     *Window
	id    int64 // the page's
	token string
	ctx   context.Context // the call's

	mu     sync.Mutex
	cond   sync.Cond
	queue  []sourceValue
	ended  bool // the page sent its last value
	closed bool
	// stop stops closing it with ctx.
	stop func() bool
	// Flow control, in bytes: taken by the method, and taken when an
	// acknowledgment was last sent, by seq.
	seq, taken, asked int64
	// closeHead starts the message telling the page the source closed.
	closeHead []byte
}

type sourceValue struct {
	raw []byte
	seq int64
}

// newSource creates the source id of the page with token for a call whose
// context is ctx. page is the context of the page that made the call:
// after a navigation, the source starts closed. Values that arrived
// before the call made its source are queued here in order.
func (w *Window) newSource(page, ctx context.Context, id int64, token string) *source {
	s := &source{w: w, id: id, token: token, ctx: ctx}
	s.cond.L = &s.mu
	head := make([]byte, 0, 48+len(token))
	head = append(head, `{"t":"src-close","c":`...)
	head = strconv.AppendInt(head, id, 10)
	head = append(head, `,"k":`...)
	head, _ = jsontext.AppendQuote(head, token)
	s.closeHead = append(head, '}')

	w.mu.Lock()
	if w.pageCtx == page {
		st := w.sourceStore()
		if st.sources == nil {
			st.sources = map[int64]*source{}
		}
		st.sources[id] = s
		if st.gone[id] {
			// The call ended or failed before its source was made: the
			// method sees it closed, not a stream that never ends.
			s.closed = true
		}
		// Values the page sent before the call made its source, in order.
		s.mu.Lock()
		if early := st.early[id]; early != nil {
			for _, e := range early.values {
				if e.token != token {
					continue
				}
				if e.end {
					s.ended = true
				} else if !s.ended && !s.closed {
					s.seq++
					s.queue = append(s.queue, sourceValue{raw: e.raw, seq: s.seq})
				}
			}
		}
		s.mu.Unlock()
		delete(st.early, id)
	} else {
		s.closed = true
	}
	w.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { s.close(closedByLoss) })
	s.mu.Lock()
	s.stop = stop
	closed := s.closed
	s.mu.Unlock()
	if closed {
		stop()
	}
	return s
}

// maxEarlySourceBytes is how many bytes of values one source may hold
// before its call makes it: flow control lets a well-behaved page hold a
// window and one message, and nothing is acknowledged before the source
// exists, so more than that is a page sending for an id no call makes.
const maxEarlySourceBytes = sourceWindow

// maxEarlySourceIDs bounds the sources that may wait for their calls at
// once; a page sending for more ids than this is not one whose calls are
// on their way.
const maxEarlySourceIDs = 1024

// maxGoneSources bounds the ids of sources that are over, whose in-flight
// values are dropped rather than kept: ids only go up, so the oldest are
// the least likely to have any left in flight.
const maxGoneSources = 1024

// sourceStore is a page's sources: those its calls made, the values that
// arrived before they made them, and the ids that are over. Window holds
// it opaquely, so a window without sources names none of these types.
type sourceStore struct {
	sources map[int64]*source
	early   map[int64]*earlySource
	gone    map[int64]bool
}

// sourceStore returns the page's sources, making the store if needed.
// w.mu is held.
func (w *Window) sourceStore() *sourceStore {
	st, _ := w.sourceStates.(*sourceStore)
	if st == nil {
		st = &sourceStore{}
		w.sourceStates = st
	}
	return st
}

// markSourceGone records that pushes for id are to be dropped. w.mu is
// held.
func (w *Window) markSourceGone(st *sourceStore, id int64) {
	if st.gone == nil {
		st.gone = map[int64]bool{}
	}
	if len(st.gone) >= maxGoneSources {
		oldest := id
		for k := range st.gone {
			if k < oldest {
				oldest = k
			}
		}
		delete(st.gone, oldest)
	}
	st.gone[id] = true
}

// earlySource is what one source holds before its call makes it.
type earlySource struct {
	values []earlySourceValue
	bytes  int // as the method counts them when it takes them
}

// earlySourceValue is a value or end that arrived before its source.
type earlySourceValue struct {
	token string
	raw   []byte
	end   bool
}

// sourceMessage routes a value or end the page sent to its source, or
// keeps it for the call that is about to make it. It runs on the main
// thread, under w.mu so a value cannot slip past the source's creation.
func (w *Window) sourceMessage(id int64, token string, raw []byte, end bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.sourceStore()
	if s := st.sources[id]; s != nil && s.token == token {
		if end {
			s.end()
		} else {
			s.push(raw)
		}
		return
	}
	if w.pageCtx == nil || st.gone[id] {
		// The call is over, or never comes: drop what it would hold.
		return
	}
	early := st.early[id]
	if early == nil {
		if len(st.early) >= maxEarlySourceIDs {
			// Too many sources waiting at once: fail this one rather
			// than dropping its values and leaving its call waiting.
			w.markSourceGone(st, id)
			return
		}
		if st.early == nil {
			st.early = map[int64]*earlySource{}
		}
		early = &earlySource{}
		st.early[id] = early
	}
	if early.bytes >= maxEarlySourceBytes {
		// More than flow control lets a page hold for a source that does
		// not exist: fail it instead of buffering more.
		delete(st.early, id)
		w.markSourceGone(st, id)
		return
	}
	early.values = append(early.values, earlySourceValue{token: token, raw: bytes.Clone(raw), end: end})
	early.bytes += len(raw) + 64 // as recv counts them
}

// push queues a value the page sent, on the main thread. The raw bytes are
// copied: the message they share does not outlive the dispatch.
func (s *source) push(raw []byte) {
	s.mu.Lock()
	if s.closed || s.ended {
		s.mu.Unlock()
		return
	}
	s.seq++
	s.queue = append(s.queue, sourceValue{raw: bytes.Clone(raw), seq: s.seq})
	s.mu.Unlock()
	s.cond.Broadcast()
}

// end records that the page sent its last value.
func (s *source) end() {
	s.mu.Lock()
	s.ended = true
	s.mu.Unlock()
	s.cond.Broadcast()
}

// recv takes the next value, waiting for one, and acknowledges what the
// method took once every half window.
func (s *source) recv() ([]byte, error) {
	s.mu.Lock()
	for len(s.queue) == 0 && !s.ended && !s.closed && s.ctx.Err() == nil {
		s.cond.Wait()
	}
	if len(s.queue) > 0 {
		e := s.queue[0]
		s.queue = s.queue[1:]
		// The page measures the whole message (body.length); this estimate
		// stays under it, and acknowledging every half window keeps the
		// two within one window of each other.
		s.taken += int64(len(e.raw)) + 64
		var ack []byte
		if s.taken-s.asked >= sourceWindow/2 {
			s.asked = s.taken
			ack = make([]byte, 0, 48+len(s.token))
			ack = append(ack, `{"t":"src-ack","c":`...)
			ack = strconv.AppendInt(ack, s.id, 10)
			ack = append(ack, `,"k":`...)
			ack, _ = jsontext.AppendQuote(ack, s.token)
			ack = append(ack, `,"n":`...)
			ack = strconv.AppendInt(ack, e.seq, 10)
			ack = append(ack, '}')
		}
		s.mu.Unlock()
		if ack != nil {
			s.w.enqueue(message{head: ack}, false)
		}
		return e.raw, nil
	}
	ended, closed, err := s.ended, s.closed, s.ctx.Err()
	s.mu.Unlock()
	switch {
	case ended:
		return nil, io.EOF
	case err != nil:
		return nil, err
	case closed:
		return nil, ErrSourceClosed
	}
	return nil, ErrSourceClosed
}

func (s *source) close(by int) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if by == closedByGo {
		s.w.enqueue(message{head: s.closeHead}, false)
	}
	stop := s.stop
	s.mu.Unlock()
	s.cond.Broadcast()
	if stop != nil {
		stop()
	}
	s.w.mu.Lock()
	st := s.w.sourceStore()
	if st.sources[s.id] == s {
		delete(st.sources, s.id)
	}
	s.w.markSourceGone(st, s.id)
	s.w.mu.Unlock()
}
