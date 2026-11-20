package mobile

import (
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/mobile/wire"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/wait"
)

// The lines of Chromium's CanonicalCookie::BuildCookieAttributesLine, which
// Android's getCookieInfo gives, and those of the iOS host read as cookies.
func TestParseCookieLine(t *testing.T) {
	u, _ := url.Parse("https://www.example.com/account")
	for _, c := range []struct {
		line string
		want platform.Cookie
		ok   bool
	}{
		{"sid=abc; domain=.example.com; path=/; expires=Thu, 01 Jan 2037 00:00:00 GMT; secure; httponly; samesite=lax",
			platform.Cookie{Name: "sid", Value: "abc", Domain: ".example.com", Path: "/", Expires: time.Date(2037, 1, 1, 0, 0, 0, 0, time.UTC),
				Secure: true, HTTPOnly: true, SameSite: "lax"}, true},
		{"a=b=c; domain=WWW.Example.com; path=/account; samesite=None",
			platform.Cookie{Name: "a", Value: "b=c", Domain: "www.example.com", Path: "/account", SameSite: "none"}, true},
		// SameSite not set: the engine does not say.
		{"pref=1; domain=www.example.com; path=/", platform.Cookie{Name: "pref", Value: "1", Domain: "www.example.com", Path: "/"}, true},
		// No domain or path: the URL's host's, at "/".
		{"x=1", platform.Cookie{Name: "x", Value: "1", Domain: "www.example.com", Path: "/"}, true},
		// A cookie without a name, as Chromium keeps one.
		{"token; domain=www.example.com; path=/", platform.Cookie{Value: "token", Domain: "www.example.com", Path: "/"}, true},
		{"", platform.Cookie{}, false},
	} {
		got, ok := parseCookieLine(c.line, u)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("parseCookieLine(%q) = %+v, %v; want %+v, %v", c.line, got, ok, c.want, c.ok)
		}
	}
	if _, ok := parseCookieLine("x=1", nil); ok {
		t.Error("a cookie without a domain or a URL")
	}
}

func TestCookieAttributes(t *testing.T) {
	exp := time.Date(2030, 5, 6, 7, 8, 9, 0, time.UTC)
	for _, c := range []struct {
		c         platform.Cookie
		url, want string
	}{
		{platform.Cookie{Domain: ".example.com", Path: "/", SameSite: "lax", Secure: true, HTTPOnly: true, Expires: exp},
			"https://example.com/", "Path=/; SameSite=lax; Domain=.example.com; Expires=Mon, 06 May 2030 07:08:09 GMT; Secure; HttpOnly"},
		// A host's cookie has no Domain.
		{platform.Cookie{Domain: "www.example.com", Path: "/a", SameSite: "strict"}, "http://www.example.com/", "Path=/a; SameSite=strict"},
		{platform.Cookie{Domain: "::1", Path: "/", SameSite: "lax"}, "http://[::1]/", "Path=/; SameSite=lax"},
	} {
		if got := cookieURL(c.c.Domain, c.c.Secure); got != c.url {
			t.Errorf("cookieURL(%q) = %q, want %q", c.c.Domain, got, c.url)
		}
		if got := cookieAttributes(c.c); got != c.want {
			t.Errorf("cookieAttributes(%+v) = %q, want %q", c.c, got, c.want)
		}
	}
}

// siteHost records the calls of website data the fake host gets, decoded
// by readCall, and answers NewDataStore.
type siteHost struct {
	*fakeHost
	mu    sync.Mutex
	got   map[uint64][]any
	store int
}

func newSiteHost(t *testing.T) (*Backend, *siteHost) {
	b, h := startBackend(t)
	s := &siteHost{fakeHost: h, got: map[uint64][]any{}}
	h.answer = func(op uint64, r *wire.Reader) []byte {
		var fields []any
		var reply []byte
		switch op {
		case wire.CallNewDataStore:
			fields = []any{r.String(), r.Bool()}
			s.mu.Lock()
			s.store++
			var w wire.Writer
			w.Int(s.store)
			w.String("")
			s.mu.Unlock()
			reply = w.Buf
		case wire.CallCloseDataStore:
			fields = []any{r.Int()}
		case wire.CallListCookies:
			fields = []any{r.Int(), r.Int(), r.String()}
		case wire.CallSetCookie:
			fields = []any{r.Int(), r.Int(), r.String(), r.String(), r.String(), r.String(), r.String(), r.String(), r.Bool(), r.Bool(), r.String()}
		case wire.CallDeleteCookie:
			fields = []any{r.Int(), r.Int(), r.String(), r.String(), r.String(), r.String(), r.String()}
		case wire.CallClearSiteData:
			fields = []any{r.Int(), r.Int(), r.Int()}
		case wire.CallCreateWindow:
			fields = []any{r.Int(), r.Bool(), r.Bool(), r.Int(), r.Bool(), r.Strings(), r.Strings(), r.Strings(), r.String(), r.String(),
				r.Bool(), r.Bool(), r.Bool(), r.Bool(), r.Int(), r.String(), r.String(), r.Int()}
		default:
			return nil
		}
		if r.Err != nil || len(r.Buf) != 0 {
			t.Errorf("call %d: %v, %d bytes left", op, r.Err, len(r.Buf))
		}
		s.mu.Lock()
		s.got[op] = fields
		s.mu.Unlock()
		return reply
	}
	return b, s
}

// call waits for the host to get the call op, and returns its fields.
func (s *siteHost) call(t *testing.T, op uint64) []any {
	t.Helper()
	var fields []any
	wait.Until(t, "the host's call", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		fields = s.got[op]
		delete(s.got, op)
		return fields != nil
	})
	return fields
}

// answer sends SiteDataDone for req.
func (s *siteHost) answer(req int, cookies []string, msg string, unsupported bool) {
	s.onUI(func() {
		deliver(encodeEvent(func(w *wire.Writer) {
			w.Uvarint(wire.EventSiteDataDone)
			w.Int(req)
			w.Strings(cookies)
			w.String(msg)
			w.Bool(unsupported)
		}), true)
	})
}

type listResult struct {
	cookies []platform.Cookie
	err     error
}

// iOS: the host lists every cookie of the store, and Go matches the URL.
func TestListCookiesThroughHost(t *testing.T) {
	b, h := newSiteHost(t)
	got := make(chan listResult, 1)
	onMain(b, func() {
		b.App().ListCookies(nil, "https://www.example.com/account", func(c []platform.Cookie, err error) { got <- listResult{c, err} })
	})
	f := h.call(t, wire.CallListCookies)
	if f[1] != 0 || f[2] != "https://www.example.com/account" {
		t.Errorf("ListCookies store %v, URL %v", f[1], f[2])
	}
	h.answer(f[0].(int), []string{
		"sid=1; domain=.example.com; path=/; secure; httponly; samesite=none",
		"other=2; domain=other.example; path=/; samesite=lax",
		"deep=3; domain=www.example.com; path=/account/settings; samesite=lax",
	}, "", false)
	r := wait.For(t, "the cookies", got)
	want := platform.Cookie{Name: "sid", Value: "1", Domain: ".example.com", Path: "/", Secure: true, HTTPOnly: true, SameSite: "none"}
	if r.err != nil || len(r.cookies) != 1 || r.cookies[0] != want {
		t.Errorf("ListCookies = %+v, %v; want the HttpOnly cookie the URL sends", r.cookies, r.err)
	}
}

// Set, Delete and Clear reach the host with what each needs, and fail with
// the host's error; an error it calls unsupported is ErrUnsupported.
func TestChangeCookiesThroughHost(t *testing.T) {
	b, h := newSiteHost(t)
	errs := make(chan error, 1)
	done := func(err error) { errs <- err }

	c := platform.Cookie{Name: "sid", Value: "v", Domain: ".example.com", Path: "/", Secure: true, HTTPOnly: true, SameSite: "lax"}
	onMain(b, func() { b.App().SetCookie(nil, c, done) })
	f := h.call(t, wire.CallSetCookie)
	want := []any{f[0], 0, "https://example.com/", "sid", "v", "Path=/; SameSite=lax; Domain=.example.com; Secure; HttpOnly",
		".example.com", "/", true, true, "lax"}
	for i := range want {
		if f[i] != want[i] {
			t.Errorf("SetCookie field %d = %v, want %v", i, f[i], want[i])
		}
	}
	h.answer(f[0].(int), nil, "mygo: cookies: the web view refused the cookie", false)
	if err := wait.For(t, "Set", errs); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("Set = %v, want the host's error", err)
	}

	onMain(b, func() {
		b.App().DeleteCookie(nil, platform.CookieKey{Name: "sid", Domain: "www.example.com", Path: "/a"}, done)
	})
	f = h.call(t, wire.CallDeleteCookie)
	want = []any{f[0], 0, "https://www.example.com/", "sid", "www.example.com", "/a",
		"Path=/a; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Secure; HttpOnly"}
	for i := range want {
		if f[i] != want[i] {
			t.Errorf("DeleteCookie field %d = %v, want %v", i, f[i], want[i])
		}
	}
	h.answer(f[0].(int), nil, "", false)
	if err := wait.For(t, "Delete", errs); err != nil {
		t.Errorf("Delete = %v", err)
	}

	onMain(b, func() { b.App().ClearCookies(nil, done) })
	f = h.call(t, wire.CallClearSiteData)
	if f[2] != int(platform.SiteDataCookies) {
		t.Errorf("ClearSiteData types %v", f[2])
	}
	h.answer(f[0].(int), nil, "mygo: no", true)
	if err := wait.For(t, "Clear", errs); !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("Clear = %v, want ErrUnsupported", err)
	}
}

// A session's store on iOS: the host numbers it, its web views and calls
// name it, and once closed it is gone.
func TestSessionStore(t *testing.T) {
	b, h := newSiteHost(t)
	var store platform.DataStore
	var err error
	onMain(b, func() {
		store, err = b.App().NewDataStore(platform.DataStoreOptions{ID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if f := h.call(t, wire.CallNewDataStore); f[0] != "6ba7b810-9dad-11d1-80b4-00c04fd430c8" || f[1] != false {
		t.Errorf("NewDataStore %v", f)
	}
	onMain(b, func() {
		if _, err := b.NewWindow(&platform.WindowOptions{DataStore: store}, &winHandler{}); err != nil {
			t.Error(err)
		}
	})
	if f := h.call(t, wire.CallCreateWindow); f[len(f)-1] != 1 {
		t.Errorf("CreateWindow's store = %v, want 1", f[len(f)-1])
	}
	got := make(chan listResult, 1)
	onMain(b, func() {
		b.App().ListCookies(store, "", func(c []platform.Cookie, err error) { got <- listResult{c, err} })
	})
	f := h.call(t, wire.CallListCookies)
	if f[1] != 1 {
		t.Errorf("ListCookies of store %v, want 1", f[1])
	}
	h.answer(f[0].(int), []string{"b=2; domain=b.example; path=/", "a=1; domain=a.example; path=/"}, "", false)
	if r := wait.For(t, "the cookies", got); r.err != nil || len(r.cookies) != 2 {
		t.Errorf("ListCookies = %+v, %v", r.cookies, r.err)
	}
	onMain(b, store.Close)
	if f := h.call(t, wire.CallCloseDataStore); f[0] != 1 {
		t.Errorf("CloseDataStore %v", f)
	}
	onMain(b, func() {
		b.App().ListCookies(store, "", func(c []platform.Cookie, err error) { got <- listResult{c, err} })
	})
	if r := wait.For(t, "the error", got); r.err == nil {
		t.Error("ListCookies of a closed store")
	}
}

// Android's web view has one store, and lists the cookies of a URL alone.
func TestOneStore(t *testing.T) {
	oneStore = true
	t.Cleanup(func() { oneStore = false })
	b, h := newSiteHost(t)
	var err error
	onMain(b, func() { _, err = b.App().NewDataStore(platform.DataStoreOptions{Ephemeral: true}) })
	if !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("NewDataStore = %v, want ErrUnsupported", err)
	}
	got := make(chan listResult, 1)
	onMain(b, func() {
		b.App().ListCookies(nil, "", func(c []platform.Cookie, err error) { got <- listResult{c, err} })
	})
	if r := wait.For(t, "the error", got); !errors.Is(r.err, platform.ErrUnsupported) {
		t.Errorf("ListCookies without a URL = %v, want ErrUnsupported", r.err)
	}
	if h.called(wire.CallListCookies) || h.called(wire.CallNewDataStore) {
		t.Error("the host was asked")
	}
	// With a URL, the host's lines have the attributes and are those the
	// URL sends.
	onMain(b, func() {
		b.App().ListCookies(nil, "https://example.com/", func(c []platform.Cookie, err error) { got <- listResult{c, err} })
	})
	f := h.call(t, wire.CallListCookies)
	h.answer(f[0].(int), []string{"SID=x; domain=example.com; path=/; secure; httponly"}, "", false)
	r := wait.For(t, "the cookies", got)
	if r.err != nil || len(r.cookies) != 1 || !r.cookies[0].HTTPOnly || r.cookies[0].Name != "SID" {
		t.Errorf("ListCookies = %+v, %v", r.cookies, r.err)
	}
}
