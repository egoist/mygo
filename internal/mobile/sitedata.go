package mobile

import (
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/internal/mobile/wire"
	"github.com/egoist/mygo/internal/platform"
)

// The website data of pages: the stores of sessions and their cookies, as
// the desktops' backends keep them. iOS's host keeps a WKWebsiteDataStore
// for each store and reads its WKHTTPCookieStore; Android's has
// CookieManager, one store of cookies for the process, which lists only
// the cookies a URL sends.

// oneStore tells that the host keeps one store of website data and lists
// only the cookies of a URL: Android's web view, whose CookieManager the
// process shares. Tests set it.
var oneStore = runtime.GOOS == "android"

// dataStore is a website data store of a session, by the number the host
// knows it by (NewDataStore).
type dataStore struct {
	b      *Backend
	id     int
	closed bool
}

// Close lets the host's store go, which its web views keep while they
// live. Main thread only.
func (s *dataStore) Close() {
	if !s.closed {
		s.closed = true
		s.b.call(&wire.CloseDataStore{Store: s.id}, nil)
	}
}

// NewDataStore makes a WKWebsiteDataStore on iOS, kept on disk under the
// session's UUID, or in memory when ephemeral. Android's web view has one
// store for the process, so sessions other than the default one are
// unsupported there.
func (a appController) NewDataStore(opts platform.DataStoreOptions) (platform.DataStore, error) {
	if oneStore {
		return nil, fmt.Errorf("mygo: sessions: Android's web view keeps one store for the app, the default session's: %w", platform.ErrUnsupported)
	}
	var r wire.NewDataStoreReply
	a.b.call(&wire.NewDataStore{ID: opts.ID, Ephemeral: opts.Ephemeral}, &r)
	if err := errorOf(r.Error); err != nil {
		return nil, err
	}
	if r.Store == 0 {
		return nil, fmt.Errorf("mygo: sessions: the host made no store: %w", platform.ErrUnsupported)
	}
	return &dataStore{b: a.b, id: r.Store}, nil
}

// storeOf returns the host's number of store, 0 for the default one.
func storeOf(store platform.DataStore) (int, error) {
	switch s := store.(type) {
	case nil:
		return 0, nil
	case *dataStore:
		if s.closed {
			return 0, fmt.Errorf("mygo: sessions: the store is closed")
		}
		return s.id, nil
	}
	return 0, fmt.Errorf("mygo: sessions: %T is not a store of this backend", store)
}

// siteData makes the call of a data store that call returns, numbered by
// req, and runs done with the host's answer, SiteDataDone. Main thread
// only.
func (b *Backend) siteData(store platform.DataStore, call func(req, store int) message, done func(wire.SiteDataDoneEvent, error)) {
	n, err := storeOf(store)
	if err != nil {
		done(wire.SiteDataDoneEvent{}, err)
		return
	}
	req := b.request(func(r wire.Reader) {
		var e wire.SiteDataDoneEvent
		e.Decode(&r)
		err := errorOf(e.Error)
		if e.Unsupported {
			err = fmt.Errorf("%s: %w", e.Error, platform.ErrUnsupported)
		}
		done(e, err)
	})
	b.call(call(req, n), nil)
}

// ClearBrowsingData deletes the data of types: every kind on iOS, and
// cookies alone on Android, whose web view has no call that deletes its
// caches or its service workers.
func (a appController) ClearBrowsingData(store platform.DataStore, types platform.SiteData, done func(error)) {
	a.b.siteData(store, func(req, n int) message {
		return &wire.ClearSiteData{Req: req, Store: n, Types: int(types)}
	}, func(_ wire.SiteDataDoneEvent, err error) { done(err) })
}

// ListCookies lists the cookies of the store: the host gives them as
// lines of a Set-Cookie header's form, which Android's
// CookieManagerCompat.getCookieInfo gives and the iOS host writes from
// WKHTTPCookieStore's. Android's lists only those of a URL.
func (a appController) ListCookies(store platform.DataStore, rawURL string, done func([]platform.Cookie, error)) {
	var u *url.URL
	if rawURL != "" {
		var err error
		if u, err = url.Parse(rawURL); err != nil {
			done(nil, err)
			return
		}
	} else if oneStore {
		done(nil, fmt.Errorf("mygo: cookies: Android's web view lists only the cookies of a URL (CookieFilter.URL): %w", platform.ErrUnsupported))
		return
	}
	a.b.siteData(store, func(req, n int) message {
		return &wire.ListCookies{Req: req, Store: n, URL: rawURL}
	}, func(e wire.SiteDataDoneEvent, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		var out []platform.Cookie
		for _, line := range e.Cookies {
			c, ok := parseCookieLine(line, u)
			// WKHTTPCookieStore cannot match a URL: match it here.
			if ok && (u == nil || platform.CookieMatchesURL(c, u)) {
				out = append(out, c)
			}
		}
		done(out, nil)
	})
}

// parseCookieLine reads a cookie in a Set-Cookie header's form, as
// Chromium's CanonicalCookie::BuildCookieAttributesLine writes it: name and
// value, then the attributes, any case. A domain with a leading dot is the
// domain's and its subdomains'; one without, or none, a host's: u's when
// the line has none. An unknown SameSite is empty.
func parseCookieLine(line string, u *url.URL) (platform.Cookie, bool) {
	parts := strings.Split(line, ";")
	var c platform.Cookie
	pair := strings.TrimSpace(parts[0])
	if name, value, ok := strings.Cut(pair, "="); ok {
		c.Name, c.Value = strings.TrimSpace(name), strings.TrimSpace(value)
	} else {
		c.Value = pair // a cookie without a name, as Chromium keeps one
	}
	if c.Name == "" && c.Value == "" {
		return c, false
	}
	for _, attr := range parts[1:] {
		key, value, _ := strings.Cut(attr, "=")
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "domain":
			c.Domain = strings.ToLower(value)
		case "path":
			c.Path = value
		case "expires":
			if t, err := http.ParseTime(value); err == nil {
				c.Expires = t.UTC().Truncate(time.Second)
			}
		case "secure":
			c.Secure = true
		case "httponly":
			c.HTTPOnly = true
		case "samesite":
			switch v := strings.ToLower(value); v {
			case "lax", "strict", "none":
				c.SameSite = v
			}
		}
	}
	if c.Domain == "" {
		if u == nil {
			return c, false
		}
		c.Domain = strings.ToLower(u.Hostname())
	}
	if c.Path == "" {
		c.Path = "/"
	}
	return c, true
}

// cookieURL returns the URL a response that sets a cookie of domain comes
// from: its host's, https for a Secure cookie.
func cookieURL(domain string, secure bool) string {
	host := strings.TrimPrefix(domain, ".")
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return scheme + "://" + host + "/"
}

// cookieAttributes returns the attributes of a Set-Cookie header that sets
// c, after its name and value, as the desktop's WebKit backend writes
// them: a Domain only for a domain's cookie, as a host's has none.
func cookieAttributes(c platform.Cookie) string {
	attrs := "Path=" + c.Path + "; SameSite=" + c.SameSite
	if strings.HasPrefix(c.Domain, ".") {
		attrs += "; Domain=" + c.Domain
	}
	if !c.Expires.IsZero() {
		attrs += "; Expires=" + c.Expires.UTC().Format(http.TimeFormat)
	}
	if c.Secure {
		attrs += "; Secure"
	}
	if c.HTTPOnly {
		attrs += "; HttpOnly"
	}
	return attrs
}

// SetCookie sets the cookie with a Set-Cookie header's attributes: Android
// sets it as a response from the cookie's host would (CookieManager), and
// iOS makes the NSHTTPCookie of the attributes, as the macOS backend does,
// and checks it is the cookie asked for.
func (a appController) SetCookie(store platform.DataStore, c platform.Cookie, done func(error)) {
	a.b.siteData(store, func(req, n int) message {
		return &wire.SetCookie{Req: req, Store: n, URL: cookieURL(c.Domain, c.Secure), Name: c.Name, Value: c.Value,
			Attributes: cookieAttributes(c), Domain: c.Domain, Path: c.Path, Secure: c.Secure, HTTPOnly: c.HTTPOnly, SameSite: c.SameSite}
	}, func(_ wire.SiteDataDoneEvent, err error) { done(err) })
}

// DeleteCookie deletes the cookie of key: iOS finds it in the store, and
// Android, whose CookieManager has no call that deletes one, sets one of
// the same name, domain and path that expired, which takes its place and
// goes. That one is Secure and HttpOnly, from https, so that the engine
// lets it take the place of any other.
func (a appController) DeleteCookie(store platform.DataStore, key platform.CookieKey, done func(error)) {
	attrs := "Path=" + key.Path
	if strings.HasPrefix(key.Domain, ".") {
		attrs += "; Domain=" + key.Domain
	}
	attrs += "; Expires=" + time.Unix(0, 0).UTC().Format(http.TimeFormat) + "; Secure; HttpOnly"
	a.b.siteData(store, func(req, n int) message {
		return &wire.DeleteCookie{Req: req, Store: n, URL: cookieURL(key.Domain, true), Name: key.Name, Domain: key.Domain, Path: key.Path,
			Attributes: attrs}
	}, func(_ wire.SiteDataDoneEvent, err error) { done(err) })
}

// ClearCookies deletes the store's cookies, its other data kept.
func (a appController) ClearCookies(store platform.DataStore, done func(error)) {
	a.ClearBrowsingData(store, platform.SiteDataCookies, done)
}
