//go:build !js

package e2e

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// errorsPage records what reaches its error handlers, the CSP violations
// it hears of and the code its AsyncFunction constructor is given, and
// replaces SyntaxError.
const errorsPage = `<script nonce="e2e">window.seen = [];
window.onerror = (m) => { seen.push("onerror: " + m) };
addEventListener("error", (e) => seen.push("error: " + e.message), true);
addEventListener("securitypolicyviolation", (e) => seen.push("csp: " + e.violatedDirective));
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
Object.defineProperty(AsyncFunction.prototype, "constructor", { value: function (s) { seen.push("source: " + s); return AsyncFunction(s) } });
window.SyntaxError = class {};</script>`

// TestEvalNoPageErrors runs statements and expressions with Eval: telling
// which one code is reaches none of the page's error handlers, and the
// page's own AsyncFunction constructor never sees the code. A page whose
// CSP forbids compiling strings runs both too, and hears of the violation
// at most once (WebView2's DevTools protocol, which Eval uses, lets it
// compile them); a Trusted Types default policy never sees the code.
func TestEvalNoPageErrors(t *testing.T) {
	evalForms := func(t *testing.T, p *mygo.Page) {
		t.Helper()
		for _, c := range []struct{ code, want string }{
			{"let a = 1; a += 1", "<nil>"},
			{"const b = 3; return b", "3"},
			{"1 + 1", "2"},
			{"document.title;", "errors"},
		} {
			if v, err := p.Eval(c.code); err != nil || fmt.Sprint(v) != c.want {
				t.Errorf("Eval(%q) = %v, %v; want %s", c.code, v, err, c.want)
			}
		}
		var evalErr *mygo.EvalError
		if _, err := p.Eval("throw new Error('boom')"); !errors.As(err, &evalErr) || evalErr.Message != "boom" {
			t.Errorf("Eval(throw) = %v, want the EvalError boom", err)
		}
		if _, err := p.Eval("let = = ;"); err == nil || errors.As(err, &evalErr) || !strings.HasPrefix(err.Error(), "SyntaxError: ") {
			t.Errorf("Eval of code that does not compile = %v, want its SyntaxError", err)
		}
	}
	// A timer lets an error event the calls queued arrive first.
	const seen = "new Promise(r => setTimeout(() => r(seen)))"
	const policy = `<script nonce="e2e">trustedTypes.createPolicy("default", { createScript: (s) => (seen.push("source: " + s), s) })</script>`

	for _, c := range []struct {
		name, head string
		// errors is whether statements may reach the page's error
		// handlers, as where Eval tries the forms in turn.
		errors bool
	}{
		{"no CSP", "", false},
		{"strict CSP", `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'nonce-e2e'">`, true},
		{"Trusted Types", `<meta http-equiv="Content-Security-Policy" content="require-trusted-types-for 'script'">`, true},
		{"Trusted Types default policy", `<meta http-equiv="Content-Security-Policy" content="require-trusted-types-for 'script'">` + policy, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWindow(t, mygo.WindowOptions{Hidden: true})
			p := w.Page()
			p.LoadHTML(c.head+"<title>errors</title>"+errorsPage, "")
			waitForPage(t, p, "Array.isArray(window.seen)")
			// Code nested too deep to compile is an error of its own, after
			// which the page still compiles code.
			deep := strings.Repeat("(", 20000) + "1" + strings.Repeat(")", 20000)
			if v, err := p.Eval(deep); err == nil && v != float64(1) {
				t.Errorf("Eval of code nested deep = %v", v)
			}
			evalForms(t, p)
			got, err := mygo.EvalAs[[]string](p, seen)
			violations := 0
			for _, s := range got {
				switch {
				case strings.HasPrefix(s, "csp: "):
					violations++
				case strings.HasPrefix(s, "source: "):
					t.Errorf("the page saw the code: %.80q", s)
				case !c.errors:
					t.Errorf("the page saw %q", s)
				}
			}
			if err != nil || violations > 1 {
				t.Errorf("the page saw %q (%v), want one violation at most", got, err)
			}
		})
	}

	// Code whose answer is not one of Eval's runs once all the same.
	t.Run("the code's own answer", func(t *testing.T) {
		w := newWindow(t, mygo.WindowOptions{Hidden: true})
		p := w.Page()
		p.LoadHTML("<title>errors</title><script>window.n = 0</script>", "")
		waitForPage(t, p, "window.n === 0")
		if _, err := p.Eval("(window.n++, window.stringify ??= JSON.stringify, JSON.stringify = () => '', 0)"); err == nil {
			t.Error("Eval of code that broke its answer succeeded")
		}
		p.Eval("JSON.stringify = window.stringify")
		if v, err := p.Eval("window.n"); err != nil || v != float64(1) {
			t.Errorf("the code ran %v times (%v), want once", v, err)
		}
	})
}
