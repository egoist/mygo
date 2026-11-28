//go:build (darwin && !ios) || (linux && !android) || windows

package e2e

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/wait"
)

func TestSessionProxy(t *testing.T) {
	// Use an RFC 2606 reserved subdomain (not .test, where Edge WebView2
	// declines ALWAYS_ALLOW certificate overrides).
	const host = "proxied.example.com"
	const target = "https://" + host + "/"
	for _, tc := range []struct{ name, user, password string }{
		{"authenticated", "alice", "s3cret"}, {"rejected", "alice", "wrong"}, {"anonymous", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var counts proxyTraffic
			var hits atomic.Int32
			origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.Header.Get("Proxy-Authorization") != "" {
					t.Error("proxy credentials reached origin")
				}
				fmt.Fprint(w, "<!doctype html><title>proxied</title>")
			}))
			origin.TLS = &tls.Config{Certificates: []tls.Certificate{selfSigned(t, host)}}
			origin.StartTLS()
			t.Cleanup(origin.Close)
			proxy := connectProxy(t, host+":443", origin.Listener.Addr().String(), &counts)
			s, err := mygo.NewSessionWithOptions(mygo.SessionOptions{Ephemeral: true, Proxy: &mygo.SessionProxyConfig{HTTPProxy: proxy.URL, Username: tc.user, Password: tc.password}})
			if err != nil {
				t.Fatal(err)
			}
			w := newWindow(t, mygo.WindowOptions{Page: mygo.PageOptions{Session: s}})
			failed := make(chan struct{}, 4)
			var certErrors atomic.Int32
			end := func() {
				if tc.user == "" && counts.challenges.Load() > 0 || tc.user != "" && counts.rejected.Load() > 0 {
					offer(failed, struct{}{})
				}
			}
			w.Page().OnDidFailLoad(func(e *mygo.LoadError) {
				if e.URL == target {
					end()
				}
			})
			w.Page().OnDidFinishLoad(end)
			w.Page().SetCertificateErrorHandler(func(e mygo.CertificateError) bool {
				certErrors.Add(1)
				return e.Host == host
			})
			if err := w.Page().LoadURL(target); err != nil {
				t.Fatal(err)
			}
			defer func() {
				t.Logf("CONNECT accepted=%d 407 missing=%d rejected=%d origin=%d certErrors=%d", counts.accepted.Load(), counts.challenges.Load(), counts.rejected.Load(), hits.Load(), certErrors.Load())
			}()
			if tc.name == "authenticated" {
				waitFor(t, w, "document.title === 'proxied'")
				if counts.challenges.Load() == 0 || counts.accepted.Load() == 0 || hits.Load() == 0 {
					t.Fatal("page bypassed CONNECT")
				}
			} else {
				wait.For(t, "proxy navigation to fail", failed)
				if hits.Load() != 0 || counts.accepted.Load() != 0 || mustEval(t, w, "document.title") == "proxied" {
					t.Fatal("unauthorized navigation reached the origin")
				}
				if tc.user == "" && counts.challenges.Load() == 0 || tc.user != "" && counts.rejected.Load() == 0 {
					t.Fatal("expected a 407 for the supplied credentials")
				}
			}
			sessionWindow(t, nil, sessionServer(t), "Direct")
		})
	}
	t.Run("plain_http", func(t *testing.T) {
		var counts proxyTraffic
		var hits atomic.Int32
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			if r.Header.Get("Proxy-Authorization") != "" {
				t.Error("proxy credentials reached origin")
			}
			fmt.Fprint(w, "<!doctype html><title>proxied-http</title>")
		}))
		t.Cleanup(origin.Close)
		proxy := connectProxy(t, host+":80", origin.Listener.Addr().String(), &counts)
		s, err := mygo.NewSessionWithOptions(mygo.SessionOptions{
			Ephemeral: true,
			Proxy:     &mygo.SessionProxyConfig{HTTPProxy: proxy.URL, Username: "alice", Password: "s3cret"},
		})
		if err != nil {
			t.Fatal(err)
		}
		w := newWindow(t, mygo.WindowOptions{Page: mygo.PageOptions{Session: s}})
		if err := w.Page().LoadURL("http://" + host + "/"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, w, "document.title === 'proxied-http'")
		if counts.challenges.Load() == 0 || counts.accepted.Load() == 0 || hits.Load() == 0 {
			t.Fatalf("plain HTTP bypassed proxy: challenges=%d accepted=%d hits=%d", counts.challenges.Load(), counts.accepted.Load(), hits.Load())
		}
	})
	t.Run("loopback", func(t *testing.T) {
		var tlsHits atomic.Int32
		tlsOrigin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tlsHits.Add(1)
			if r.Header.Get("Proxy-Authorization") != "" {
				t.Error("proxy credentials reached loopback origin")
			}
			fmt.Fprint(w, "<!doctype html><title>loopback-tls</title>")
		}))
		tlsOrigin.TLS = &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}}
		tlsOrigin.StartTLS()
		t.Cleanup(tlsOrigin.Close)

		var tlsCounts proxyTraffic
		tlsProxy := connectProxy(t, tlsOrigin.Listener.Addr().String(), tlsOrigin.Listener.Addr().String(), &tlsCounts)
		s1, err := mygo.NewSessionWithOptions(mygo.SessionOptions{
			Ephemeral: true,
			Proxy:     &mygo.SessionProxyConfig{HTTPProxy: tlsProxy.URL, Username: "alice", Password: "s3cret"},
		})
		if err != nil {
			t.Fatal(err)
		}
		w1 := newWindow(t, mygo.WindowOptions{Page: mygo.PageOptions{Session: s1}})
		w1.Page().SetCertificateErrorHandler(func(e mygo.CertificateError) bool { return e.Host == "127.0.0.1" })
		if err := w1.Page().LoadURL(tlsOrigin.URL + "/"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, w1, "document.title === 'loopback-tls'")

		var httpHits atomic.Int32
		httpOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpHits.Add(1)
			if r.Header.Get("Proxy-Authorization") != "" {
				t.Error("proxy credentials reached loopback origin")
			}
			fmt.Fprint(w, "<!doctype html><title>loopback-http</title>")
		}))
		t.Cleanup(httpOrigin.Close)

		var httpCounts proxyTraffic
		httpProxy := connectProxy(t, httpOrigin.Listener.Addr().String(), httpOrigin.Listener.Addr().String(), &httpCounts)
		s2, err := mygo.NewSessionWithOptions(mygo.SessionOptions{
			Ephemeral: true,
			Proxy:     &mygo.SessionProxyConfig{HTTPProxy: httpProxy.URL, Username: "alice", Password: "s3cret"},
		})
		if err != nil {
			t.Fatal(err)
		}
		w2 := newWindow(t, mygo.WindowOptions{Page: mygo.PageOptions{Session: s2}})
		if err := w2.Page().LoadURL(httpOrigin.URL + "/"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, w2, "document.title === 'loopback-http'")

		t.Logf("loopback[%s] https(accepted=%d 407=%d origin=%d) http(accepted=%d 407=%d origin=%d)",
			runtime.GOOS,
			tlsCounts.accepted.Load(), tlsCounts.challenges.Load(), tlsHits.Load(),
			httpCounts.accepted.Load(), httpCounts.challenges.Load(), httpHits.Load())

		if runtime.GOOS == "darwin" {
			// Network.framework's nw_proxy_config always bypasses loopback.
			if tlsCounts.accepted.Load() != 0 || httpCounts.accepted.Load() != 0 || tlsHits.Load() == 0 || httpHits.Load() == 0 {
				t.Fatalf("unexpected macOS loopback proxy routing: https=%+v/%d http=%+v/%d",
					tlsCounts.accepted.Load(), tlsHits.Load(), httpCounts.accepted.Load(), httpHits.Load())
			}
		} else {
			// Linux (WebKitNetworkProxySettings with no ignore list) and Windows
			// (<-loopback>) route 127.0.0.1 through the configured proxy.
			if tlsCounts.challenges.Load() == 0 || tlsCounts.accepted.Load() == 0 || tlsHits.Load() == 0 {
				t.Fatalf("HTTPS loopback bypassed proxy: challenges=%d accepted=%d hits=%d",
					tlsCounts.challenges.Load(), tlsCounts.accepted.Load(), tlsHits.Load())
			}
			if httpCounts.challenges.Load() == 0 || httpCounts.accepted.Load() == 0 || httpHits.Load() == 0 {
				t.Fatalf("HTTP loopback bypassed proxy: challenges=%d accepted=%d hits=%d",
					httpCounts.challenges.Load(), httpCounts.accepted.Load(), httpHits.Load())
			}
		}
	})
}

type proxyTraffic struct {
	challenges, rejected, accepted atomic.Int32
}

// connectProxy maps a single authority to a loopback origin, supporting both
// CONNECT tunnels (HTTPS everywhere, plus plain HTTP on macOS) and standard
// absolute-URI HTTP proxy forwarding (plain HTTP on Linux and Windows).
func connectProxy(t *testing.T, authority, origin string, counts *proxyTraffic) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	var tunnels sync.WaitGroup
	active := make(map[net.Conn]net.Conn)
	closing := false
	wantHost, wantPort, _ := net.SplitHostPort(authority)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isConnect := r.Method == "CONNECT"
		if isConnect {
			if r.Host != authority {
				http.Error(w, "unknown target", 403)
				return
			}
		} else {
			port := r.URL.Port()
			if port == "" {
				port = "80"
			}
			if r.URL.Scheme != "http" || r.URL.Hostname() != wantHost || port != wantPort {
				http.Error(w, "unknown target", 403)
				return
			}
		}
		auth := r.Header.Get("Proxy-Authorization")
		if auth != "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:s3cret")) {
			if auth == "" {
				counts.challenges.Add(1)
			} else {
				counts.rejected.Add(1)
			}
			w.Header().Set("Proxy-Authenticate", `Basic realm="mygo-e2e"`)
			w.Header().Set("Connection", "close")
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		if !isConnect {
			if counts.challenges.Load() == 0 {
				t.Error("authenticated proxy request arrived before the first 407")
			}
			counts.accepted.Add(1)
			req, err := http.NewRequestWithContext(r.Context(), r.Method, "http://"+origin+r.URL.RequestURI(), r.Body)
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			resp, err := http.DefaultTransport.RoundTrip(req)
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			defer resp.Body.Close()
			for k, vs := range resp.Header {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			return
		}
		upstream, err := net.Dial("tcp", origin)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer upstream.Close()
		mu.Lock()
		if closing {
			mu.Unlock()
			http.Error(w, "proxy closed", http.StatusServiceUnavailable)
			return
		}
		client, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			mu.Unlock()
			return
		}
		defer client.Close()
		active[client] = upstream
		tunnels.Add(1)
		mu.Unlock()
		defer func() { mu.Lock(); delete(active, client); mu.Unlock(); tunnels.Done() }()
		copied := make(chan struct{})
		if counts.challenges.Load() == 0 {
			t.Error("authenticated CONNECT arrived before the first 407")
		}
		counts.accepted.Add(1)
		fmt.Fprint(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() {
			defer close(copied)
			io.Copy(upstream, buf)
			upstream.(*net.TCPConn).CloseWrite()
		}()
		io.Copy(client, upstream)
		client.Close()
		upstream.Close()
		<-copied
	}))
	t.Cleanup(func() {
		mu.Lock()
		closing = true
		for client, upstream := range active {
			client.Close()
			upstream.Close()
		}
		mu.Unlock()
		proxy.Close()
		tunnels.Wait()
	})
	return proxy
}
