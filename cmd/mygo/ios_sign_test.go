package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A simulator's executable carries the app's identifier, which the keychain
// needs, with no plugin asking for anything too.
func TestSimulatorEntitlements(t *testing.T) {
	for _, ps := range []mobilePlugins{
		nil,
		{{ID: "push", IOS: &pluginIOS{Entitlements: map[string]any{"aps-environment": "development"}}}},
	} {
		work := t.TempDir()
		flags, err := ps.simulatorEntitlements(work, "dev.mygo.x")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(work, "simulator.entitlements")
		if len(flags) == 0 || flags[len(flags)-1] != path {
			t.Fatalf("flags: %v", flags)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, want := range []string{
			"<key>application-identifier</key>\n\t<string>dev.mygo.x</string>",
			"<key>keychain-access-groups</key>\n\t<array>\n\t\t<string>dev.mygo.x</string>",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%d plugins: no %q in\n%s", len(ps), want, s)
			}
		}
		if len(ps) > 0 && !strings.Contains(s, "<key>aps-environment</key>") {
			t.Errorf("the plugin's entitlement is gone:\n%s", s)
		}
	}
}
