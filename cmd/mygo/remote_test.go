package main

import (
	"slices"
	"strings"
	"testing"
)

func TestStripRemoteArgs(t *testing.T) {
	tests := []struct {
		name    string
		rawArgs []string
		posArgs []string
		want    []string
	}{
		{
			name:    "separate flag and positional dir",
			rawArgs: []string{"-remote", "wayamac", "-platform", "darwin/arm64", "examples/counter-native"},
			posArgs: []string{"examples/counter-native"},
			want:    []string{"-platform", "darwin/arm64"},
		},
		{
			name:    "equals flag",
			rawArgs: []string{"--remote=wayamac", "-debug", "."},
			posArgs: []string{"."},
			want:    []string{"-debug"},
		},
		{
			name:    "env remote only",
			rawArgs: []string{"-skip-dmg", "examples/hello"},
			posArgs: []string{"examples/hello"},
			want:    []string{"-skip-dmg"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripRemoteArgs(tt.rawArgs, tt.posArgs)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("stripRemoteArgs(%v, %v) = %v, want %v", tt.rawArgs, tt.posArgs, got, tt.want)
			}
		})
	}
}

func TestRemoteWorkDir(t *testing.T) {
	dir := remoteWorkDir("/home/waya/Projects/Community/mygo/examples/counter-native")
	if !strings.HasPrefix(dir, "~/.mygo-remote/counter-native-") {
		t.Fatalf("unexpected remoteWorkDir: %s", dir)
	}
	if dir != remoteWorkDir("/home/waya/Projects/Community/mygo/examples/counter-native") {
		t.Fatalf("remoteWorkDir is not deterministic")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("darwin/arm64"); got != "'darwin/arm64'" {
		t.Fatalf("shellQuote = %s", got)
	}
	if got := shellQuote("a'b"); got != `'a'\''b'` {
		t.Fatalf("shellQuote with single quote = %s", got)
	}
}
