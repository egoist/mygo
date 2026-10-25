//go:build windows && (amd64 || arm64)

package windows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileName checks that every storage ID is a profile name WebView2
// takes: at most 64 of its ASCII characters, not ending with a period,
// never the Default profile, and different for IDs that differ by case,
// which WebView2 would otherwise read as one profile.
func TestProfileName(t *testing.T) {
	seen := map[string]string{}
	for _, id := range []string{"a", "A", "acct-a", "acct-A", "ACCT-A", "default", "Default", "x.", "x", ".", "..", "con", strings.Repeat("a", 64), strings.Repeat("A", 64)} {
		name := profileName(id)
		if name != profileName(id) {
			t.Errorf("profileName(%q) is not stable", id)
		}
		if len(name) == 0 || len(name) > 64 {
			t.Errorf("profileName(%q) = %q, length %d", id, name, len(name))
		}
		if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
			t.Errorf("profileName(%q) = %q ends with a period or a space", id, name)
		}
		if strings.EqualFold(name, "Default") || !strings.HasPrefix(name, storageProfilePrefix) {
			t.Errorf("profileName(%q) = %q may be the Default profile", id, name)
		}
		for _, r := range name {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				t.Errorf("profileName(%q) = %q has the character %q", id, name, r)
			}
		}
		key := strings.ToLower(name)
		if other, ok := seen[key]; ok {
			t.Errorf("profileName(%q) and profileName(%q) are one profile, %q", id, other, name)
		}
		seen[key] = id
	}
}

// TestProfileStored checks how ClearStorage finds the directory WebView2
// made for a profile: WV2Profile_ and the profile's name, in EBWebView of
// the user data folder, and nothing else.
func TestProfileStored(t *testing.T) {
	userData := t.TempDir()
	if profileStored(userData, "acct-a") {
		t.Error("a user data folder with no EBWebView has the profile")
	}
	ebWebView := filepath.Join(userData, "EBWebView")
	for _, dir := range []string{"Default", "WV2Profile_" + strings.ToUpper(profileName("acct-a")), "WV2Profile_" + profileName("acct-b")} {
		if err := os.MkdirAll(filepath.Join(ebWebView, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ebWebView, "WV2Profile_"+profileName("acct-c")), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"acct-a": true, "acct-b": true, "acct-c": false, "acct-d": false, "default": false} {
		if got := profileStored(userData, id); got != want {
			t.Errorf("profileStored(%q) = %v, want %v", id, got, want)
		}
	}
}
