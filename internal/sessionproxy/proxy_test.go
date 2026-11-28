package sessionproxy

import (
	"errors"
	"testing"
)

func TestMatches(t *testing.T) {
	for _, tc := range []struct {
		endpoint, host string
		port           int
		proxy, want    bool
	}{
		{"127.0.0.1:80", "127.0.0.1", 0, true, true},
		{"[::1]:8080", "[::1]", 8080, true, true},
		{"[::1]:8080", "::1", 8080, true, true},
		{"127.0.0.1:80", "127.0.0.2", 80, true, false},
		{"127.0.0.1:80", "127.0.0.1", 81, true, false},
		{"127.0.0.1:80", "127.0.0.1", 80, false, false},
	} {
		if Matches("http://"+tc.endpoint, tc.host, tc.port, tc.proxy) != tc.want {
			t.Errorf("%+v", tc)
		}
	}
}

func TestChallenge(t *testing.T) {
	a := Attempts{}
	const endpoint = "http://127.0.0.1:80"
	c := Challenge{"Server", "Basic", "http://127.0.0.1"}
	if got := a.Answer("1", c, endpoint, "u", "p"); got != (Answer{Response: "Default"}) {
		t.Fatal(got)
	}
	c.Source = "Proxy"
	if got := a.Answer("1", c, endpoint, "u", "p"); got != (Answer{"ProvideCredentials", "u", "p"}) {
		t.Fatal(got)
	}
	for _, got := range []Answer{a.Answer("1", c, endpoint, "u", "p"), a.Answer("2", c, endpoint, "", ""), a.Answer("3", c, "http://127.0.0.2:80", "u", "p")} {
		if got != (Answer{Response: "CancelAuth"}) {
			t.Fatal(got)
		}
	}
}

func TestOverrides(t *testing.T) {
	clean := func(string) string { return "" }
	none := func() error { return nil }
	for _, key := range []string{"WEBVIEW2_USER_DATA_FOLDER", "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"} {
		if CheckOverrides(func(k string) string {
			if k == key {
				return "x"
			}
			return ""
		}, nil, none) == nil {
			t.Fatal(key)
		}
	}
	if CheckOverrides(clean, []string{"app", "--edge-webview-switches=x"}, none) == nil {
		t.Fatal("command line")
	}
	for _, machine := range []bool{true, false} {
		for _, setting := range []string{"UserDataFolder", "AdditionalBrowserArguments"} {
			for _, id := range []string{"app.exe", "app.id", "*"} {
				err := CheckOverrides(clean, nil, func() error {
					return CheckPolicy([]string{"app.exe", "app.id"}, func(m bool, s, n string) (bool, error) { return m == machine && s == setting && n == id, nil })
				})
				if (err == nil) != (id == "*" && setting == "UserDataFolder") {
					t.Fatal(machine, setting, id, err)
				}
			}
		}
	}
	denied := errors.New("denied")
	if !errors.Is(CheckOverrides(clean, nil, func() error {
		return CheckPolicy([]string{"app"}, func(bool, string, string) (bool, error) { return false, denied })
	}), denied) {
		t.Fatal("registry error")
	}
}
