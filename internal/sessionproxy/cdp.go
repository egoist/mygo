package sessionproxy

import "encoding/json"

// Attach enables proxy authentication before navigation and before newly
// attached debuggable targets start running. Only HTTP/HTTPS request-stage
// interception is enabled (Chromium requires a request-stage pattern for
// handleAuthRequests), avoiding response-stage pauses and non-HTTP schemes.
// Flattened sessions share one event receiver but each has its own Fetch request IDs.
func Attach(endpoint, user, password string, subscribe func(string, func(string, []byte)) error, call func(string, string, string, func(error)), ready func(), fail func(error)) {
	const fetch = `{"patterns":[{"urlPattern":"http://*","requestStage":"Request"},{"urlPattern":"https://*","requestStage":"Request"}],"handleAuthRequests":true}`
	const attach = `{"autoAttach":true,"waitForDebuggerOnStart":true,"flatten":true}`
	attempts := map[string]*Attempts{}
	type event struct {
		SessionID          string
		WaitingForDebugger bool
		RequestID          string
		AuthChallenge      Challenge
	}
	on := func(name string, handler func(string, event)) error {
		return subscribe(name, func(session string, data []byte) {
			var e event
			if json.Unmarshal(data, &e) == nil {
				handler(session, e)
			}
		})
	}
	send := func(session, method string, params any) {
		b, err := json.Marshal(params)
		if err != nil {
			fail(err)
			return
		}
		call(session, method, string(b), nil)
	}
	if err := on("Fetch.requestPaused", func(session string, e event) {
		if a := attempts[session]; a != nil {
			a.Delete(e.RequestID)
		}
		send(session, "Fetch.continueRequest", map[string]string{"requestId": e.RequestID})
	}); err != nil {
		fail(err)
		return
	}
	if err := on("Fetch.authRequired", func(session string, e event) {
		a := attempts[session]
		if a == nil {
			a = &Attempts{}
			attempts[session] = a
		}
		answer := a.Answer(e.RequestID, e.AuthChallenge, endpoint, user, password)
		send(session, "Fetch.continueWithAuth", map[string]any{"requestId": e.RequestID, "authChallengeResponse": answer})
	}); err != nil {
		fail(err)
		return
	}
	if err := on("Target.detachedFromTarget", func(_ string, e event) { delete(attempts, e.SessionID) }); err != nil {
		fail(err)
		return
	}
	enable := func(session string, done func(error)) {
		call(session, "Fetch.enable", fetch, func(err error) {
			if session == "" && err != nil {
				done(err)
				return
			}
			call(session, "Target.setAutoAttach", attach, done)
		})
	}
	if err := on("Target.attachedToTarget", func(_ string, e event) {
		if e.SessionID == "" {
			return
		}
		enable(e.SessionID, func(error) {
			if e.WaitingForDebugger {
				call(e.SessionID, "Runtime.runIfWaitingForDebugger", "{}", nil)
			}
		})
	}); err != nil {
		fail(err)
		return
	}
	enable("", func(err error) {
		if err != nil {
			fail(err)
			return
		}
		ready()
	})
}
