//go:build darwin

package darwin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// This file serves an http(s) asset to AVFoundation through the asset's own
// resource loader: the asset gets a URL of a scheme AVFoundation cannot load
// itself, and every byte it asks for comes from Go's net/http here.
//
// It exists so that a read can be stopped. A read of a reader is
// -[AVAssetReaderOutput copyNextSampleBuffer]: synchronous, and blocking
// while its data is loaded over the network. AVFoundation forbids
// -[AVAssetReader cancelReading] from running while such a read is in flight
// (AVAssetReader.h), so a source that does not own the loading cannot cancel
// a stalled read at all: Close could only abandon it.
//
// A resource loading request is the documented way to fail: AVFoundation's
// header says -[AVAssetResourceLoadingRequest finishLoadingWithError:] with a
// nil error means "failed" (the error is optional). Close cancels the Go
// requests and finishes every outstanding loading request that way; the read
// blocked on their data then returns, and only then is cancelReading safe.

// ReferenceRestrictionsForbidAll is AVAssetReferenceRestrictionForbidAll: an
// asset with it only loads media data from its own container, never from a
// reference it points at. The resource loader then owns every byte of the
// asset, so nothing loads around it.
const ReferenceRestrictionsForbidAll = 0xFFFF

// mediaScheme is the scheme of the URL AVFoundation sees. It cannot load it,
// so its resource loader asks the Loader for everything.
const mediaScheme = "mygo-media"

var (
	loaderOnce   sync.Once
	loaderClass  objc.Class
	loaderErr    error
	loaderRoutes sync.Map // uintptr -> *Loader
)

// A Loader is the transport of one http(s) asset: it hands AVFoundation a
// URL of its own, and serves that URL's resource loading requests over HTTP.
// Close fails what is outstanding, which makes a blocked native read return.
type Loader struct {
	raw    *url.URL // where the Go requests go
	custom string   // the URL AVFoundation sees, of mediaScheme
	client *http.Client

	ctx    context.Context
	cancel context.CancelFunc

	delegate uintptr // the AVAssetResourceLoaderDelegate, owned

	mu       sync.Mutex
	closed   bool
	problem  error // the first reason the asset could not be served, if any
	requests map[uintptr]*loaderRequest
	serving  sync.WaitGroup
	done     chan struct{} // closed when the first Close has returned
}

// A loaderRequest is one AVAssetResourceLoadingRequest being served.
type loaderRequest struct {
	l    *Loader
	req  uintptr // the retained loading request
	data uintptr // its data request, the object respondWithData: goes to
	ctx  context.Context
	stop context.CancelFunc

	mu       sync.Mutex
	finished bool
}

// OpenLoader makes a loader for an http(s) URL. The URL's extension (or, when
// that says nothing, the origin's content type) says what container it is;
// only the containers this package reads over the network are served.
func OpenLoader(target string) (*Loader, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("not an http(s) URL")
	}
	if err := Load(); err != nil {
		return nil, err
	}
	class, err := loaderDelegateClass()
	if err != nil {
		return nil, err
	}
	delegate := SendID(Alloc(uintptr(class)), Sel("init"))
	if delegate == 0 {
		return nil, fmt.Errorf("no resource loader delegate")
	}
	ctx, cancel := context.WithCancel(context.Background())
	custom := &url.URL{Scheme: mediaScheme, Host: u.Host, Path: u.Path, RawQuery: u.RawQuery}
	l := &Loader{
		raw:      u,
		custom:   custom.String(),
		client:   &http.Client{},
		ctx:      ctx,
		cancel:   cancel,
		delegate: delegate,
		requests: make(map[uintptr]*loaderRequest),
		done:     make(chan struct{}),
	}
	loaderRoutes.Store(delegate, l)
	return l, nil
}

// URL is the URL to make the asset with. It is of [mediaScheme]: AVFoundation
// asks the Loader for it.
func (l *Loader) URL() string { return l.custom }

// Attach makes the asset load through the loader. Call it before asking the
// asset anything.
func (l *Loader) Attach(asset uintptr) error {
	queue, err := CaptureQueue()
	if err != nil {
		return err
	}
	loader := SendID(asset, Sel("resourceLoader"))
	if loader == 0 {
		return fmt.Errorf("the asset has no resource loader")
	}
	SendVoid2(loader, Sel("setDelegate:queue:"), l.delegate, queue)
	return nil
}

// Problem says why the asset could not be served, for the error an open that
// found no tracks reports.
func (l *Loader) Problem() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.problem
}

// Close stops serving the asset and returns when no request is being served
// any more. It cancels the Go requests and finishes the outstanding loading
// requests as failed: the documented way to make a native read blocked on
// their data return. Closing again waits for the first Close, so a caller
// that closes twice also waits for what the first one gave back.
func (l *Loader) Close() error {
	l.mu.Lock()
	if l.closed {
		done := l.done
		l.mu.Unlock()
		<-done
		return nil
	}
	l.closed = true
	outstanding := make([]*loaderRequest, 0, len(l.requests))
	for _, r := range l.requests {
		outstanding = append(outstanding, r)
	}
	l.mu.Unlock()

	l.cancel()
	for _, r := range outstanding {
		r.fail()
	}
	l.serving.Wait()

	loaderRoutes.Delete(l.delegate)
	WithPool(func() { Release(l.delegate) })
	close(l.done)
	return nil
}

// fail finishes an outstanding loading request as failed, once.
func (r *loaderRequest) fail() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	WithPool(func() { SendVoid1(r.req, Sel("finishLoadingWithError:"), 0) })
}

// finish finishes a loading request whose data was all given, once.
func (r *loaderRequest) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	WithPool(func() { SendVoid(r.req, Sel("finishLoading")) })
}

func (r *loaderRequest) stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finished
}

// respond gives some of the requested data to AVFoundation, and says whether
// it was still wanted.
func (r *loaderRequest) respond(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return false
	}
	WithPool(func() {
		nsdata := SendID2(Class("NSData"), Sel("dataWithBytes:length:"), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
		// The pointer went to the system as a uintptr, which does not
		// keep the buffer alive: it must live until NSData copied it.
		runtime.KeepAlive(data)
		SendVoid1(r.data, Sel("respondWithData:"), nsdata)
	})
	return true
}

// setContentInformation answers what the container is and how long it is,
// before any of its data: AVFoundation asks once, for the first request.
//
// The asset is told that byte ranges are supported: the Loader can answer one
// from any offset, by asking the origin for that range or by skipping the part
// of a whole reply before it, so seeking does not depend on the origin's own
// Range support.
func (r *loaderRequest) setContentInformation(uti string, length int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	WithPool(func() {
		info := SendID(r.req, Sel("contentInformationRequest"))
		if info == 0 {
			return
		}
		SendVoid1(info, Sel("setContentType:"), NSString(uti))
		SendVoid1(info, Sel("setContentLength:"), uintptr(length))
		SendVoid1(info, Sel("setByteRangeAccessSupported:"), 1)
	})
}

// waitFor serves a loading request the system asked for, and says whether it
// will be served: AVFoundation takes a false for it as a failure of the load.
func (l *Loader) waitFor(request uintptr) bool {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return false
	}
	r := &loaderRequest{l: l, req: Retain(request)}
	WithPool(func() { r.data = SendID(request, Sel("dataRequest")) })
	r.ctx, r.stop = context.WithCancel(l.ctx)
	l.requests[request] = r
	l.serving.Add(1)
	l.mu.Unlock()
	go r.serve()
	return true
}

// cancelled stops serving a request the system no longer wants.
func (l *Loader) cancelled(request uintptr) {
	l.mu.Lock()
	r := l.requests[request]
	l.mu.Unlock()
	if r == nil {
		return
	}
	r.mu.Lock()
	r.finished = true
	r.mu.Unlock()
	r.stop()
}

// serve loads one loading request, and finishes it one way or the other.
func (r *loaderRequest) serve() {
	defer r.l.serving.Done()
	defer func() {
		r.l.mu.Lock()
		delete(r.l.requests, r.req)
		r.l.mu.Unlock()
		r.stop()
		Release(r.req)
	}()
	if err := r.load(); err != nil {
		// A request that ended because Close or the system gave up on
		// it says nothing about the asset: the open that finds no
		// tracks reports the real reason, not that one.
		if !errors.Is(err, errRequestDone) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			r.l.fail(err)
		}
		r.fail()
		return
	}
	r.finish()
}

// fail records why the asset could not be served, for the open that found no
// tracks.
func (l *Loader) fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.problem == nil {
		l.problem = err
	}
}

// load serves the request's range over HTTP: one GET, answered with what the
// origin gave, in chunks so that a long range is never held whole in memory.
func (r *loaderRequest) load() error {
	l := r.l
	var (
		dataReq        uintptr
		info           uintptr
		offset, length int64
		toEnd          bool
	)
	// The getters make temporary objects, and the pool is only for them:
	// the HTTP wait below must not hold an autorelease pool open.
	WithPool(func() {
		dataReq = SendID(r.req, Sel("dataRequest"))
		info = SendID(r.req, Sel("contentInformationRequest"))
		if dataReq != 0 {
			offset = int64(SendInt(dataReq, Sel("requestedOffset")))
			length = int64(SendInt(dataReq, Sel("requestedLength")))
			toEnd = SendBool(dataReq, Sel("requestsAllDataToEndOfResource"))
		}
	})
	if dataReq != 0 && length <= 0 && !toEnd {
		return nil
	}
	o, err := fetchOrigin(r.ctx, l.client, l.raw, offset, length, toEnd)
	if err != nil {
		return err
	}
	defer o.body.Close()
	if info != 0 {
		r.setContentInformation(o.uti, o.total)
	}
	if o.eof || dataReq == 0 {
		// A range past the end of the asset is how it ends, and a
		// request without data only asks what the container is.
		return nil
	}
	if o.start < offset {
		// The origin answered from before the request (a 206 that
		// starts earlier, or a 200 that ignored the Range): skip to
		// where the request is.
		if err := skipTo(o.body, offset-o.start, offset, o.total); err != nil {
			return err
		}
	}
	if toEnd {
		return copyToEnd(r, o.body, offset, o.total)
	}
	return copyN(r, o.body, length, offset, o.total)
}

// originResponse is one validated GET of a byte range of the asset.
type originResponse struct {
	body  io.ReadCloser
	start int64 // the asset offset its bytes begin at
	total int64 // how long the asset is
	uti   string
	eof   bool // the range is past the end: the asset ended before it
}

// fetchOrigin asks the origin for a range of the asset and validates its
// answer: a 206 must say which bytes of which length it is, a 416 must say
// the length it is past, and a 200 must say how long the whole asset is.
func fetchOrigin(ctx context.Context, client *http.Client, raw *url.URL, offset, length int64, toEnd bool) (*originResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw.String(), nil)
	if err != nil {
		return nil, err
	}
	if !toEnd && length > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
	} else if toEnd {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	// The bytes of the answer must be the bytes of the asset: a content
	// encoding would change their offsets and hide the length.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	o := &originResponse{body: resp.Body, total: -1}
	bad := func(why string) (*originResponse, error) {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %s: %s", raw, resp.Status, why)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		o.total = resp.ContentLength
		if o.total < 0 {
			return bad("it does not say how long it is (it must answer with a Content-Length or a Content-Range)")
		}
	case http.StatusPartialContent:
		start, end, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok {
			return bad(fmt.Sprintf("a 206 without a usable Content-Range (%q)", resp.Header.Get("Content-Range")))
		}
		if start < 0 {
			return bad(fmt.Sprintf("a 206 with the whole-range form of Content-Range (%q)", resp.Header.Get("Content-Range")))
		}
		if start > offset {
			return bad(fmt.Sprintf("bytes from offset %d, not the requested %d", start, offset))
		}
		if resp.ContentLength >= 0 && resp.ContentLength != end-start+1 {
			return bad(fmt.Sprintf("a Content-Length of %d for bytes %d-%d", resp.ContentLength, start, end))
		}
		o.start, o.total = start, total
	case http.StatusRequestedRangeNotSatisfiable:
		start, _, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok || start >= 0 {
			return bad(fmt.Sprintf("a 416 without a whole-range Content-Range (%q)", resp.Header.Get("Content-Range")))
		}
		if offset < total {
			return bad(fmt.Sprintf("a 416 for offset %d of %d bytes", offset, total))
		}
		o.total, o.eof = total, true
	default:
		return bad("this version does not read that answer")
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return bad(fmt.Sprintf("its bytes are encoded as %q", enc))
	}
	o.uti = contentTypeFor(path.Ext(strings.ToLower(raw.Path)), resp.Header.Get("Content-Type"))
	if o.uti == "" {
		return bad(fmt.Sprintf("a %q this version does not read over the network", resp.Header.Get("Content-Type")))
	}
	return o, nil
}

// skipTo throws away the n bytes an answer starts before the requested
// offset. The origin stopping before the request is a failure, unless the
// request is past the end of the asset, where there is nothing to serve.
func skipTo(body io.Reader, n, offset, total int64) error {
	if n <= 0 {
		return nil
	}
	if _, err := io.CopyN(io.Discard, body, n); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			if offset >= total {
				return nil
			}
			return fmt.Errorf("the origin stopped before the requested offset %d of %d bytes", offset, total)
		}
		return err
	}
	return nil
}

// serveChunk is the most of a range given to AVFoundation in one
// respondWithData:, so that no range is ever held whole in memory.
const serveChunk = 64 * 1024

// A sampleSink is a loading request being given the bytes of its range.
type sampleSink interface {
	stopped() bool
	respond(data []byte) bool
}

// copyN gives the range's n bytes, starting at offset of a total-byte asset.
// An origin that stops short is a truncated transfer, not the end of the
// range, unless the bytes it gave reach the end of the asset.
func copyN(s sampleSink, body io.Reader, n, offset, total int64) error {
	want := n
	pos := offset
	buf := make([]byte, serveChunk)
	for n > 0 {
		if pos >= total {
			return nil
		}
		if s.stopped() {
			return errRequestDone
		}
		chunk := int64(len(buf))
		if chunk > n {
			chunk = n
		}
		got, err := io.ReadFull(body, buf[:chunk])
		if got > 0 {
			s.respond(buf[:got])
			n -= int64(got)
			pos += int64(got)
		}
		if err == nil {
			continue
		}
		if pos >= total {
			return nil
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return fmt.Errorf("the origin stopped after %d of %d bytes", want-n, want)
		}
		return err
	}
	return nil
}

// copyToEnd gives the asset's bytes from offset to its end, and fails when
// the origin's body ends before the length it promised.
func copyToEnd(s sampleSink, body io.Reader, offset, total int64) error {
	pos := offset
	buf := make([]byte, serveChunk)
	for {
		if s.stopped() {
			return errRequestDone
		}
		got, err := body.Read(buf)
		if got > 0 {
			s.respond(buf[:got])
			pos += int64(got)
		}
		if err == io.EOF {
			if pos >= total {
				return nil
			}
			return fmt.Errorf("the origin stopped after %d of %d bytes", pos, total)
		}
		if err != nil {
			return err
		}
	}
}

// errRequestDone says a loading request was finished by Close or by the
// system while its data was still being served: there is nothing more to do.
var errRequestDone = errors.New("the loading request is over")

// parseContentRange reads a Content-Range header: "bytes 0-99/1234" gives
// the first and last byte of the range and the length, and "bytes */1234"
// (the whole-range form a 416 uses) gives -1, -1 and the length. It says
// false for a header this version cannot trust: a missing form, a length it
// cannot read, a range that ends before it starts or after the asset, or a
// length it does not state.
func parseContentRange(v string) (start, end, total int64, ok bool) {
	v = strings.TrimSpace(v)
	const prefix = "bytes "
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return 0, 0, 0, false
	}
	v = strings.TrimSpace(v[len(prefix):])
	slash := strings.IndexByte(v, '/')
	if slash < 0 {
		return 0, 0, 0, false
	}
	rangePart := strings.TrimSpace(v[:slash])
	totalPart := strings.TrimSpace(v[slash+1:])
	if totalPart == "*" {
		return 0, 0, 0, false
	}
	t, err := strconv.ParseInt(totalPart, 10, 64)
	if err != nil || t <= 0 {
		return 0, 0, 0, false
	}
	if rangePart == "*" {
		return -1, -1, t, true
	}
	dash := strings.IndexByte(rangePart, '-')
	if dash <= 0 {
		return 0, 0, 0, false
	}
	s, err := strconv.ParseInt(strings.TrimSpace(rangePart[:dash]), 10, 64)
	if err != nil || s < 0 {
		return 0, 0, 0, false
	}
	e, err := strconv.ParseInt(strings.TrimSpace(rangePart[dash+1:]), 10, 64)
	if err != nil || e < s || e >= t {
		return 0, 0, 0, false
	}
	return s, e, t, true
}

// contentTypeFor names the UTI of an asset from its path's extension, or from
// the origin's MIME type when the extension says nothing. AVFoundation's
// content information takes a UTI, not a MIME type: a MIME type there makes
// the asset load no tracks at all.
func contentTypeFor(ext, mime string) string {
	if ext != "" {
		switch ext {
		case ".mp4", ".m4v", ".m4b":
			return "public.mpeg-4"
		case ".mov":
			return "com.apple.quicktime-movie"
		case ".m4a":
			return "com.apple.m4a-audio"
		case ".mp3":
			return "public.mp3"
		case ".wav", ".wave":
			return "com.microsoft.waveform-audio"
		}
	}
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "video/mp4", "application/mp4":
		return "public.mpeg-4"
	case "video/quicktime":
		return "com.apple.quicktime-movie"
	case "audio/mp4", "audio/x-m4a", "audio/m4a":
		return "com.apple.m4a-audio"
	case "audio/mpeg", "audio/mp3":
		return "public.mp3"
	case "audio/wav", "audio/x-wav", "audio/wave", "audio/x-pn-wav":
		return "com.microsoft.waveform-audio"
	}
	return ""
}

// loaderDelegateClass is the AVAssetResourceLoaderDelegate class, made once:
// the runtime has no way to free a class, so every asset shares one.
func loaderDelegateClass() (objc.Class, error) {
	loaderOnce.Do(func() {
		var protocols []*objc.Protocol
		if proto := objc.GetProtocol("AVAssetResourceLoaderDelegate"); proto != nil {
			protocols = append(protocols, proto)
		}
		loaderClass, loaderErr = objc.RegisterClass("MyGoMediaLoader", objc.GetClass("NSObject"), protocols, nil, []objc.MethodDef{
			{
				Cmd: objc.RegisterName("resourceLoader:shouldWaitForLoadingOfRequestedResource:"),
				Fn: func(self objc.ID, _cmd objc.SEL, resourceLoader objc.ID, request objc.ID) bool {
					if v, ok := loaderRoutes.Load(uintptr(self)); ok {
						return v.(*Loader).waitFor(uintptr(request))
					}
					return false
				},
			},
			{
				Cmd: objc.RegisterName("resourceLoader:didCancelLoadingRequest:"),
				Fn: func(self objc.ID, _cmd objc.SEL, resourceLoader objc.ID, request objc.ID) {
					if v, ok := loaderRoutes.Load(uintptr(self)); ok {
						v.(*Loader).cancelled(uintptr(request))
					}
				},
			},
		})
	})
	if loaderErr != nil {
		return 0, loaderErr
	}
	if loaderClass == 0 {
		return 0, fmt.Errorf("no resource loader delegate class")
	}
	return loaderClass, nil
}
