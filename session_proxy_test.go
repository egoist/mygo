//go:build (darwin && !ios) || (linux && !android) || windows

package mygo

import "testing"

func TestSessionProxyOptions(t *testing.T) {
	if !supportsSessionProxy() {
		t.Skip("session proxies unavailable")
	}
	cfg := SessionProxyConfig{HTTPProxy: "http://[0:0:0:0:0:0:0:1]:8080"}
	opts := SessionOptions{Name: t.Name(), Proxy: &cfg}
	s, err := NewSessionWithOptions(opts)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HTTPProxy = "http://[::1]:8080"
	same, err := NewSessionWithOptions(opts)
	if err != nil || same != s || NewSession(opts.Name) != s {
		t.Fatal("named session identity", err)
	}
	cfg.Password = "changed"
	if s.proxy.Password != "" {
		t.Fatal("caller changed session proxy")
	}
	for _, bad := range []SessionOptions{{}, {Name: "named", Ephemeral: true}, opts, {Name: opts.Name}, {Ephemeral: true, Proxy: &SessionProxyConfig{HTTPProxy: "http://127.0.0.1:80#"}}} {
		if _, err := NewSessionWithOptions(bad); err == nil {
			t.Fatal("accepted invalid or conflicting options")
		}
	}
	opts = SessionOptions{Ephemeral: true, Proxy: &cfg}
	a, err := NewSessionWithOptions(opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSessionWithOptions(opts)
	if err != nil || a == b || !a.IsEphemeral() {
		t.Fatal("ephemeral identity", err)
	}
	plain := NewSession(t.Name() + "/plain")
	if got, err := NewSessionWithOptions(SessionOptions{Name: plain.Name()}); got != plain || err != nil {
		t.Fatal("existing plain session", err)
	}
}

func TestSessionProxyValidation(t *testing.T) {
	for _, endpoint := range []string{
		"https://127.0.0.1:80", "http://localhost:80", "http://127.0.0.1",
		"http://127.0.0.1:0", "http://[fe80::1%25en0]:80", "http://alice@127.0.0.1:80",
		"http://127.0.0.1:80/", "http://127.0.0.1:80?", "http://127.0.0.1:80#",
	} {
		_, err := NewSessionWithOptions(SessionOptions{Ephemeral: true, Proxy: &SessionProxyConfig{HTTPProxy: endpoint}})
		if err == nil {
			t.Errorf("accepted endpoint %q", endpoint)
		}
	}
	for _, cfg := range []SessionProxyConfig{
		{HTTPProxy: "http://127.0.0.1:80", Username: "a\x00b"},
		{HTTPProxy: "http://127.0.0.1:80", Password: "a\x00b"},
	} {
		if _, err := NewSessionWithOptions(SessionOptions{Ephemeral: true, Proxy: &cfg}); err == nil {
			t.Error("accepted NUL in credentials")
		}
	}
}
