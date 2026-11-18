//go:build windows && (amd64 || arm64)

package windows

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// TestOpenDialogOptions checks the options of open dialogs: a folder
// picker never forces items of the file system, which freezes it on WSL
// shares (#2024), even when the dialog has it by default; a file picker
// still does.
func TestOpenDialogOptions(t *testing.T) {
	const fosNoChangeDir = 0x8
	for _, tc := range []struct {
		name    string
		current uint32
		o       platform.OpenDialogOptions
		want    uint32
	}{
		{"files", 0, platform.OpenDialogOptions{OpenFiles: true}, fosForceFileSystem | fosPathMustExist | fosFileMustExist},
		{"files, several, hidden", fosNoChangeDir, platform.OpenDialogOptions{OpenFiles: true, Multiple: true, ShowHiddenFiles: true},
			fosNoChangeDir | fosForceFileSystem | fosPathMustExist | fosFileMustExist | fosAllowMultiselect | fosForceShowHidden},
		{"folders", 0, platform.OpenDialogOptions{OpenDirectories: true}, fosPickFolders | fosPathMustExist},
		{"folders, forced by default", fosNoChangeDir | fosForceFileSystem, platform.OpenDialogOptions{OpenDirectories: true},
			fosNoChangeDir | fosPickFolders | fosPathMustExist},
		{"folders, several", 0, platform.OpenDialogOptions{OpenDirectories: true, Multiple: true}, fosPickFolders | fosPathMustExist | fosAllowMultiselect},
	} {
		if got := openDialogOptions(tc.current, &tc.o); got != tc.want {
			t.Errorf("%s: options %#x, want %#x", tc.name, got, tc.want)
		}
	}
}

// TestFileSystemPaths checks that items without a path, such as This PC
// in a folder picker, are left out, and that a choice of only those
// counts as canceled.
func TestFileSystemPaths(t *testing.T) {
	for _, tc := range []struct {
		in, want []string
	}{
		{nil, nil},
		{[]string{""}, nil},
		{[]string{"", ""}, nil},
		{[]string{`C:\a`}, []string{`C:\a`}},
		{[]string{`C:\a`, "", `\\wsl.localhost\Ubuntu\home\b`}, []string{`C:\a`, `\\wsl.localhost\Ubuntu\home\b`}},
	} {
		got := fileSystemPaths(tc.in)
		if !slices.Equal(got, tc.want) || (tc.want == nil) != (got == nil) {
			t.Errorf("fileSystemPaths(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}
