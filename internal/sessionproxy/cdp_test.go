package sessionproxy

import (
	"errors"
	"strings"
	"testing"
)

func TestChildProxySession(t *testing.T) {
	for _, scenario := range []string{"supported", "worker", "detached"} {
		var childMethods []string
		handlers := map[string]func(string, []byte){}
		var route, payload string
		var pending func(error)
		var ready, failed bool
		Attach("http://127.0.0.1:8080", "alice", "pw", func(name string, h func(string, []byte)) error { handlers[name] = h; return nil }, func(id, method, params string, done func(error)) {
			route = id + "/" + method
			payload = params
			if id == "child" {
				childMethods = append(childMethods, method)
			}
			if id == "child" && method == "Fetch.enable" {
				pending = done
				return
			}
			if done != nil {
				if id == "child" && scenario == "detached" {
					done(errors.New("target detached"))
				} else {
					done(nil)
				}
			}
		}, func() { ready = true }, func(error) { failed = true })
		if !ready || route != "/Target.setAutoAttach" {
			t.Fatal(route)
		}
		handlers["Target.attachedToTarget"]("", []byte(`{"sessionId":"child","waitingForDebugger":true}`))
		if route != "child/Fetch.enable" {
			t.Fatal(route)
		}
		var err error
		if scenario != "supported" {
			err = errors.New("Fetch unavailable")
		}
		pending(err)
		if got := strings.Join(childMethods, ","); got != "Fetch.enable,Target.setAutoAttach,Runtime.runIfWaitingForDebugger" || failed {
			t.Fatal(scenario, got, failed)
		}
		if scenario != "supported" {
			continue
		}
		auth := func(id, answer string) {
			t.Helper()
			handlers["Fetch.authRequired"](id, []byte(`{"requestId":"1","authChallenge":{"source":"Proxy","scheme":"Basic","origin":"http://127.0.0.1:8080"}}`))
			if route != id+"/Fetch.continueWithAuth" || !strings.Contains(payload, answer) {
				t.Fatal(route, payload)
			}
		}
		auth("", "ProvideCredentials")
		auth("child", "ProvideCredentials")
		auth("child", "CancelAuth")
		handlers["Fetch.requestPaused"]("child", []byte(`{"requestId":"1"}`))
		if route != "child/Fetch.continueRequest" {
			t.Fatal(route)
		}
		auth("child", "ProvideCredentials")
		handlers["Target.detachedFromTarget"]("", []byte(`{"sessionId":"child"}`))
		auth("child", "ProvideCredentials")
		auth("", "CancelAuth")
	}
}

func TestTopLevelSetupFailure(t *testing.T) {
	for _, method := range []string{"Fetch.enable", "Target.setAutoAttach"} {
		t.Run(method, func(t *testing.T) {
			want := errors.New(method)
			var got error
			Attach("", "", "", func(string, func(string, []byte)) error { return nil }, func(_, name, _ string, done func(error)) {
				if name == method {
					done(want)
				} else {
					done(nil)
				}
			}, func() { t.Fatal("ready after setup failure") }, func(err error) { got = err })
			if got != want {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestAttemptsFIFOEvictionPreservesRecentAndOrigin(t *testing.T) {
	var a Attempts
	c := Challenge{Source: "Proxy", Scheme: "Basic", Origin: "http://127.0.0.1:8080"}
	for i := range maxTrackedAttempts {
		id := string(rune('a'+i%26)) + "-" + strings.Repeat("x", i/26) + string(rune('0'+i%10))
		if i == 0 {
			id = "oldest-0"
		}
		if i == maxTrackedAttempts-1 {
			id = "recent-last"
		}
		if ans := a.Answer(id, c, "http://127.0.0.1:8080", "alice", "pw"); ans.Response != "ProvideCredentials" {
			t.Fatalf("i=%d id=%q got %+v", i, id, ans)
		}
	}
	// Origin 401 uses Default and does not evict any tracked proxy attempts.
	origin := Challenge{Source: "Server", Scheme: "Basic", Origin: "http://127.0.0.1:8080"}
	if ans := a.Answer("origin-1", origin, "http://127.0.0.1:8080", "alice", "pw"); ans.Response != "Default" {
		t.Fatalf("origin got %+v", ans)
	}
	// A new proxy request after capacity evicts only the oldest entry, allowing
	// long-lived sessions to keep authenticating while preserving recent retries.
	if ans := a.Answer("next-after-cap", c, "http://127.0.0.1:8080", "alice", "pw"); ans.Response != "ProvideCredentials" {
		t.Fatalf("next-after-cap got %+v", ans)
	}
	if ans := a.Answer("recent-last", c, "http://127.0.0.1:8080", "alice", "pw"); ans.Response != "CancelAuth" {
		t.Fatalf("recent retry got %+v", ans)
	}
	if ans := a.Answer("next-after-cap", c, "http://127.0.0.1:8080", "alice", "pw"); ans.Response != "CancelAuth" {
		t.Fatalf("next-after-cap retry got %+v", ans)
	}
}
