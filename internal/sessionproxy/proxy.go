// Package sessionproxy validates native proxy challenges and WebView2 overrides.
package sessionproxy

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

func Matches(endpoint, host string, port int, isProxy bool) bool {
	if !isProxy || port < 0 || port > 65535 {
		return false
	}
	if port == 0 {
		port = 80
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	ip, err := netip.ParseAddr(host)
	want, err2 := netip.ParseAddrPort(strings.TrimPrefix(endpoint, "http://"))
	return err == nil && err2 == nil && netip.AddrPortFrom(ip, uint16(port)) == want
}

type Challenge struct{ Source, Scheme, Origin string }
type Answer struct {
	Response string `json:"response"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type Attempts struct {
	seen  map[string]struct{}
	order []string
}

// maxTrackedAttempts far exceeds Chromium's per-proxy socket limit (32), so
// FIFO eviction only retires long-completed requests while keeping every
// in-flight 407 retry tracked without pausing response stages.
const maxTrackedAttempts = 1024

func (a *Attempts) Delete(id string) {
	if a.seen == nil {
		return
	}
	delete(a.seen, id)
}

func (a *Attempts) Answer(id string, c Challenge, endpoint, user, password string) Answer {
	if c.Source != "Proxy" {
		return Answer{Response: "Default"}
	}
	cancel := Answer{Response: "CancelAuth"}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Scheme != "http" || !strings.EqualFold(c.Scheme, "basic") || user == "" && password == "" {
		return cancel
	}
	if _, tried := a.seen[id]; tried {
		return cancel
	}
	port := 80
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port == 0 {
			return cancel
		}
	}
	if !Matches(endpoint, u.Hostname(), port, true) {
		return cancel
	}
	if a.seen == nil {
		a.seen = map[string]struct{}{}
	}
	for len(a.order) >= maxTrackedAttempts {
		oldest := a.order[0]
		a.order = a.order[1:]
		delete(a.seen, oldest)
	}
	a.seen[id] = struct{}{}
	a.order = append(a.order, id)
	return Answer{Response: "ProvideCredentials", Username: user, Password: password}
}

func CheckOverrides(getenv func(string) string, args []string, registry func() error) error {
	for _, name := range []string{"WEBVIEW2_USER_DATA_FOLDER", "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"} {
		if getenv(name) != "" {
			return errors.New("mygo: proxied session conflicts with " + name)
		}
	}
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		switch strings.ToLower(name) {
		case "--edge-webview-switches", "-edge-webview-switches", "/edge-webview-switches":
			return errors.New("mygo: proxied session conflicts with edge-webview-switches")
		}
	}
	return registry()
}

func CheckPolicy(names []string, read func(bool, string, string) (bool, error)) error {
	for _, machine := range []bool{true, false} {
		for _, setting := range []string{"AdditionalBrowserArguments", "UserDataFolder"} {
			ids := names
			if setting == "AdditionalBrowserArguments" {
				ids = append(append([]string(nil), names...), "*")
			}
			for _, id := range ids {
				present, err := read(machine, setting, id)
				if err != nil {
					return err
				}
				if present {
					return errors.New("mygo: proxied session conflicts with WebView2 " + setting + " policy")
				}
			}
		}
	}
	return nil
}
