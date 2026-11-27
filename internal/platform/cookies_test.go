package platform

import (
	"net/url"
	"testing"
)

// TestCookieMatchesURL checks which requests a page's cookies go with, by
// the rules of RFC 6265: the domain, the path and Secure.
func TestCookieMatchesURL(t *testing.T) {
	must := func(c Cookie, raw string, want bool) {
		t.Helper()
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parsing %q: %v", raw, err)
		}
		if got := CookieMatchesURL(c, u); got != want {
			t.Errorf("CookieMatchesURL(%+v, %q) = %v", c, raw, got)
		}
	}
	// A cookie for a host goes to that host only, however it is spelled.
	c := Cookie{Domain: "example.com", Path: "/"}
	must(c, "https://example.com/", true)
	must(c, "https://www.example.com/", false)
	must(Cookie{Domain: "Example.com", Path: "/"}, "https://EXAMPLE.com/", true)

	// A cookie for a domain goes to its hosts, but not to one that only
	// ends alike, and not to an address.
	d := Cookie{Domain: ".example.com", Path: "/"}
	must(d, "https://example.com/", true)
	must(d, "https://www.example.com/", true)
	must(d, "https://notexample.com/", false)
	must(Cookie{Domain: ".0.1", Path: "/"}, "http://127.0.0.1/", false)
	must(Cookie{Domain: "127.0.0.1", Path: "/"}, "http://127.0.0.1/", true)

	// The path is: the cookie's, or the root; a longer path goes with it
	// where it is a directory of the request's.
	p := Cookie{Domain: "example.com"}
	must(p, "https://example.com", true) // no path is the root
	must(Cookie{Domain: "example.com", Path: "/foo"}, "https://example.com/foo", true)
	must(Cookie{Domain: "example.com", Path: "/foo"}, "https://example.com/foo/bar", true)
	must(Cookie{Domain: "example.com", Path: "/foo"}, "https://example.com/foobar", false)
	must(Cookie{Domain: "example.com", Path: "/foo/"}, "https://example.com/foo", false)
	must(Cookie{Domain: "example.com", Path: "/foo/"}, "https://example.com/foo/bar", true)

	// A secure cookie goes with https only.
	s := Cookie{Domain: "example.com", Path: "/", Secure: true}
	must(s, "http://example.com/", false)
	must(s, "https://example.com/", true)
	// An insecure one goes with either.
	must(c, "http://example.com/", true)
}
