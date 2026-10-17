//go:build darwin

package darwin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// These tests check the HTTP side of the loader without an asset: what the
// loader trusts in an origin's answer, and that a body shorter than the range
// it promised fails rather than ending the range quietly.

func TestParseContentRange(t *testing.T) {
	tests := []struct {
		header     string
		start, end int64
		total      int64
		ok         bool
	}{
		{"bytes 0-99/1234", 0, 99, 1234, true},
		{"bytes 0-0/1", 0, 0, 1, true},
		{"BYTES 5-7/10", 5, 7, 10, true},
		{"  bytes 2-4/10  ", 2, 4, 10, true},
		{"bytes */1234", -1, -1, 1234, true},
		{"bytes 0-99/1234 garbage", 0, 0, 0, false},
		{"", 0, 0, 0, false},
		{"0-99/1234", 0, 0, 0, false},
		{"bytes 0-99", 0, 0, 0, false},
		{"bytes 0-99/*", 0, 0, 0, false},
		{"bytes x-99/1234", 0, 0, 0, false},
		{"bytes 0-x/1234", 0, 0, 0, false},
		{"bytes 5-3/1234", 0, 0, 0, false},
		{"bytes 0-5/5", 0, 0, 0, false},
		{"bytes 0-1/0", 0, 0, 0, false},
		{"bytes -/1", 0, 0, 0, false},
		{"bytes */0", 0, 0, 0, false},
	}
	for _, tt := range tests {
		start, end, total, ok := parseContentRange(tt.header)
		if start != tt.start || end != tt.end || total != tt.total || ok != tt.ok {
			t.Errorf("parseContentRange(%q) = (%d, %d, %d, %v), want (%d, %d, %d, %v)",
				tt.header, start, end, total, ok, tt.start, tt.end, tt.total, tt.ok)
		}
	}
}

func TestContentTypeForTakesAUTI(t *testing.T) {
	tests := []struct{ ext, mime, want string }{
		{".mp4", "", "public.mpeg-4"},
		{".wav", "text/plain", "com.microsoft.waveform-audio"},
		{"", "video/mp4", "public.mpeg-4"},
		{"", "video/mp4; codecs=avc1", "public.mpeg-4"},
		{"", " APPLICATION/MP4 ", "public.mpeg-4"},
		{"", "audio/mpeg", "public.mp3"},
		{"", "", ""},
		{"", "application/octet-stream", ""},
	}
	for _, tt := range tests {
		if got := contentTypeFor(tt.ext, tt.mime); got != tt.want {
			t.Errorf("contentTypeFor(%q, %q) = %q, want %q", tt.ext, tt.mime, got, tt.want)
		}
	}
}

func TestSkipTo(t *testing.T) {
	if err := skipTo(strings.NewReader("0123456789"), 4, 4, 10); err != nil {
		t.Errorf("skipping 4 of 10: %v", err)
	}
	err := skipTo(strings.NewReader("01"), 4, 4, 10)
	if err == nil {
		t.Error("skipping past a body shorter than the offset worked")
	}
	if err := skipTo(strings.NewReader("01"), 4, 10, 10); err != nil {
		t.Errorf("a range past the end of the asset: %v", err)
	}
	if err := skipTo(bytes.NewReader(nil), 0, 0, 0); err != nil {
		t.Errorf("skipping nothing: %v", err)
	}
}

// recordingSink is the loading request a serve helper gives bytes to.
type recordingSink struct {
	mu     sync.Mutex
	chunks [][]byte
	stop   bool
}

func (s *recordingSink) stopped() bool { return s.stop }

func (s *recordingSink) respond(data []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, append([]byte(nil), data...))
	return true
}

func (s *recordingSink) served() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []byte
	for _, c := range s.chunks {
		all = append(all, c...)
	}
	return all
}

func TestCopyN(t *testing.T) {
	t.Run("the whole range", func(t *testing.T) {
		sink := new(recordingSink)
		if err := copyN(sink, strings.NewReader("0123456789"), 10, 0, 10); err != nil {
			t.Fatalf("copyN: %v", err)
		}
		if got := string(sink.served()); got != "0123456789" {
			t.Errorf("gave %q", got)
		}
	})
	t.Run("a body that stops short", func(t *testing.T) {
		sink := new(recordingSink)
		err := copyN(sink, strings.NewReader("01234"), 10, 0, 20)
		if err == nil {
			t.Fatal("a body shorter than the range it promised ended quietly")
		}
		if !strings.Contains(err.Error(), "stopped after 5 of 10 bytes") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("a range that reaches the end of the asset", func(t *testing.T) {
		sink := new(recordingSink)
		if err := copyN(sink, strings.NewReader("01234"), 10, 0, 5); err != nil {
			t.Fatalf("a range ending at the end of the asset: %v", err)
		}
		if got := string(sink.served()); got != "01234" {
			t.Errorf("gave %q", got)
		}
	})
	t.Run("a range past the end of the asset", func(t *testing.T) {
		sink := new(recordingSink)
		if err := copyN(sink, strings.NewReader(""), 5, 10, 10); err != nil {
			t.Fatalf("a range past the end: %v", err)
		}
		if got := sink.served(); len(got) != 0 {
			t.Errorf("gave %q", got)
		}
	})
	t.Run("a request the system finished", func(t *testing.T) {
		sink := &recordingSink{stop: true}
		if err := copyN(sink, strings.NewReader("01234"), 5, 0, 5); !errors.Is(err, errRequestDone) {
			t.Fatalf("copyN = %v, want errRequestDone", err)
		}
	})
}

func TestCopyToEnd(t *testing.T) {
	t.Run("the whole asset", func(t *testing.T) {
		sink := new(recordingSink)
		if err := copyToEnd(sink, strings.NewReader("0123456789"), 0, 10); err != nil {
			t.Fatalf("copyToEnd: %v", err)
		}
		if got := string(sink.served()); got != "0123456789" {
			t.Errorf("gave %q", got)
		}
	})
	t.Run("a body that stops before the length it promised", func(t *testing.T) {
		sink := new(recordingSink)
		err := copyToEnd(sink, strings.NewReader("0123"), 0, 10)
		if err == nil {
			t.Fatal("a truncated body ended quietly")
		}
		if !strings.Contains(err.Error(), "stopped after 4 of 10 bytes") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("from an offset", func(t *testing.T) {
		sink := new(recordingSink)
		if err := copyToEnd(sink, strings.NewReader("6789"), 6, 10); err != nil {
			t.Fatalf("copyToEnd from 6: %v", err)
		}
		if got := string(sink.served()); got != "6789" {
			t.Errorf("gave %q", got)
		}
	})
	t.Run("a request the system finished", func(t *testing.T) {
		sink := &recordingSink{stop: true}
		if err := copyToEnd(sink, strings.NewReader("0123"), 0, 4); !errors.Is(err, errRequestDone) {
			t.Fatalf("copyToEnd = %v, want errRequestDone", err)
		}
	})
}

// A few deterministic origins for fetchOrigin: each answers every request the
// same way, and the test says what the loader should make of it.
func TestFetchOrigin(t *testing.T) {
	body := []byte("0123456789")
	serve := func(t *testing.T, handler http.HandlerFunc) *url.URL {
		t.Helper()
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		u, err := url.Parse(srv.URL + "/clip.mp4")
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	rangeOf := func(r *http.Request) (int64, int64) {
		// "bytes=a-b" or "bytes=a-"
		v := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		dash := strings.IndexByte(v, '-')
		if dash < 0 {
			return 0, -1
		}
		start, _ := strconv.ParseInt(v[:dash], 10, 64)
		end := int64(-1)
		if rest := v[dash+1:]; rest != "" {
			end, _ = strconv.ParseInt(rest, 10, 64)
		}
		return start, end
	}

	t.Run("a 200 with its whole length", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body)
		})
		o, err := fetchOrigin(context.Background(), http.DefaultClient, u, 0, 0, false)
		if err != nil {
			t.Fatalf("fetchOrigin: %v", err)
		}
		defer o.body.Close()
		if o.total != int64(len(body)) || o.start != 0 || o.uti != "public.mpeg-4" || o.eof {
			t.Fatalf("origin = {start:%d total:%d uti:%q eof:%v}", o.start, o.total, o.uti, o.eof)
		}
	})
	t.Run("a 200 that ignores the range", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body)
		})
		o, err := fetchOrigin(context.Background(), http.DefaultClient, u, 4, 3, false)
		if err != nil {
			t.Fatalf("fetchOrigin: %v", err)
		}
		defer o.body.Close()
		if o.start != 0 || o.total != int64(len(body)) {
			t.Fatalf("origin = {start:%d total:%d}, want the whole asset", o.start, o.total)
		}
		if err := skipTo(o.body, 4, 4, o.total); err != nil {
			t.Fatalf("skipping to the request: %v", err)
		}
		got := make([]byte, 3)
		if _, err := io.ReadFull(o.body, got); err != nil || string(got) != "456" {
			t.Fatalf("the bytes of the range = %q, %v", got, err)
		}
	})
	t.Run("a 200 that does not say how long it is", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Write(body)
			w.(http.Flusher).Flush()
		})
		_, err := fetchOrigin(context.Background(), http.DefaultClient, u, 0, 0, false)
		if err == nil || !strings.Contains(err.Error(), "does not say how long it is") {
			t.Fatalf("fetchOrigin = %v, want an unknown-length error", err)
		}
	})
	t.Run("a 206", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			start, end := rangeOf(r)
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
			w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[start : end+1])
		})
		o, err := fetchOrigin(context.Background(), http.DefaultClient, u, 4, 3, false)
		if err != nil {
			t.Fatalf("fetchOrigin: %v", err)
		}
		defer o.body.Close()
		if o.start != 4 || o.total != int64(len(body)) {
			t.Fatalf("origin = {start:%d total:%d}, want {4, %d}", o.start, o.total, len(body))
		}
	})
	t.Run("a 206 without a Content-Range", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[4:7])
		})
		_, err := fetchOrigin(context.Background(), http.DefaultClient, u, 4, 3, false)
		if err == nil || !strings.Contains(err.Error(), "Content-Range") {
			t.Fatalf("fetchOrigin = %v, want a Content-Range error", err)
		}
	})
	t.Run("a 206 whose bytes are not the requested ones", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 6-8/%d", len(body)))
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[6:9])
		})
		_, err := fetchOrigin(context.Background(), http.DefaultClient, u, 4, 3, false)
		if err == nil || !strings.Contains(err.Error(), "not the requested 4") {
			t.Fatalf("fetchOrigin = %v, want a misaligned-range error", err)
		}
	})
	t.Run("a 206 that starts earlier than requested", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 2-4/%d", len(body)))
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[2:5])
		})
		o, err := fetchOrigin(context.Background(), http.DefaultClient, u, 4, 3, false)
		if err != nil {
			t.Fatalf("fetchOrigin: %v", err)
		}
		defer o.body.Close()
		if o.start != 2 {
			t.Fatalf("start = %d, want 2", o.start)
		}
	})
	t.Run("a 206 whose Content-Length does not match its Content-Range", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-6/%d", len(body)))
			w.Header().Set("Content-Length", "5")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[4:9])
		})
		_, err := fetchOrigin(context.Background(), http.DefaultClient, u, 4, 3, false)
		if err == nil || !strings.Contains(err.Error(), "Content-Length") {
			t.Fatalf("fetchOrigin = %v, want a Content-Length error", err)
		}
	})
	t.Run("a 416 past the end", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(body)))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		})
		o, err := fetchOrigin(context.Background(), http.DefaultClient, u, int64(len(body)), 2, false)
		if err != nil {
			t.Fatalf("fetchOrigin: %v", err)
		}
		defer o.body.Close()
		if !o.eof || o.total != int64(len(body)) {
			t.Fatalf("origin = {total:%d eof:%v}, want the end of the asset", o.total, o.eof)
		}
	})
	t.Run("a 416 for a range within the asset", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(body)))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		})
		_, err := fetchOrigin(context.Background(), http.DefaultClient, u, 5, 2, false)
		if err == nil || !strings.Contains(err.Error(), "416 for offset 5") {
			t.Fatalf("fetchOrigin = %v, want a 416 error", err)
		}
	})
	t.Run("a 416 without a whole-range Content-Range", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		})
		_, err := fetchOrigin(context.Background(), http.DefaultClient, u, 10, 2, false)
		if err == nil || !strings.Contains(err.Error(), "416") {
			t.Fatalf("fetchOrigin = %v, want a 416 error", err)
		}
	})
	t.Run("an answer it does not read", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no", http.StatusInternalServerError)
		})
		_, err := fetchOrigin(context.Background(), http.DefaultClient, u, 0, 0, false)
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("fetchOrigin = %v, want a status error", err)
		}
	})
	t.Run("a request that asks for the identity encoding", func(t *testing.T) {
		sawIdentity := false
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			sawIdentity = r.Header.Get("Accept-Encoding") == "identity"
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body)
		})
		o, err := fetchOrigin(context.Background(), http.DefaultClient, u, 0, 0, false)
		if err != nil {
			t.Fatalf("fetchOrigin: %v", err)
		}
		o.body.Close()
		if !sawIdentity {
			t.Error("the origin was not asked to send the bytes as they are")
		}
	})
	t.Run("an encoded answer", func(t *testing.T) {
		u := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body)
		})
		_, err := fetchOrigin(context.Background(), http.DefaultClient, u, 0, 0, false)
		if err == nil || !strings.Contains(err.Error(), "Content-Encoding") && !strings.Contains(err.Error(), "encoded as") {
			t.Fatalf("fetchOrigin = %v, want an encoded-bytes error", err)
		}
	})
	t.Run("a container this version does not read", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "1")
			w.Write([]byte("x"))
		}))
		t.Cleanup(srv.Close)
		u, err := url.Parse(srv.URL + "/stream")
		if err != nil {
			t.Fatal(err)
		}
		_, err = fetchOrigin(context.Background(), http.DefaultClient, u, 0, 0, false)
		if err == nil || !strings.Contains(err.Error(), "does not read over the network") {
			t.Fatalf("fetchOrigin = %v, want an unsupported-type error", err)
		}
	})
}

func TestOpenLoaderRefusesWhatItCannotLoad(t *testing.T) {
	for _, target := range []string{"rtsp://127.0.0.1:1/a.mp4", "ftp://127.0.0.1/a.mp4", "file:///tmp/a.mp4", "not a url"} {
		if _, err := OpenLoader(target); err == nil {
			t.Errorf("OpenLoader(%q) worked", target)
		}
	}
}

// TestOpenLoaderTakesAnyCaseOfScheme checks that the scheme is lowercased
// before it decides what the URL is.
func TestOpenLoaderTakesAnyCaseOfScheme(t *testing.T) {
	for _, target := range []string{"HTTP://example.com/clip.mp4", "Https://example.com/clip.mp4"} {
		l, err := OpenLoader(target)
		if err != nil {
			t.Fatalf("OpenLoader(%q): %v", target, err)
		}
		if err := l.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
}

// TestLoaderCloseTwice checks that the second Close waits for the first rather
// than returning while the other one still frees the delegate.
func TestLoaderCloseTwice(t *testing.T) {
	l, err := OpenLoader("http://127.0.0.1:1/clip.mp4")
	if err != nil {
		t.Fatalf("OpenLoader: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- l.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	}
}
