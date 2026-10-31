package ui

import (
	"reflect"
	"testing"
)

func TestFilesDropCallbackUsesCommittedIdentityAndOwnedPaths(t *testing.T) {
	show, disabled, building := true, false, false
	version := 1
	var got []int
	var paths []string
	tt := NewTester(func(c *Context) {
		building = true
		defer func() { building = false }()
		if show {
			v := version
			Box(c).Key("zone").Size(100, 100).Disabled(disabled).OnFilesDrop(func(files []string) {
				if building {
					t.Fatal("drop callback ran during construction")
				}
				got = append(got, v)
				paths = files
				files[0] = "owned"
			})
		}
	}, 200, 200)
	input := []string{"a.txt", "b.txt"}
	if !tt.DropFiles(50, 50, input...) {
		t.Fatal("drop target did not accept files")
	}
	if input[0] != "a.txt" || !reflect.DeepEqual(paths, []string{"owned", "b.txt"}) {
		t.Fatal("paths were not copied for the callback")
	}
	tt.Frame()
	version = 2
	tt.Frame()
	tt.DropFiles(50, 50, input...)
	disabled = true
	tt.Frame()
	if tt.DropFiles(50, 50, input...) {
		t.Fatal("disabled element accepted files")
	}
	show = false
	tt.Frame()
	if tt.DropFiles(50, 50, input...) {
		t.Fatal("removed element accepted files")
	}
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("drop callback calls = %v", got)
	}
}

func TestFilesDropCallbackDoesNotRepeatAsViewQuery(t *testing.T) {
	calls := 0
	queried := 0
	tt := NewTester(func(c *Context) {
		zone := Box(c).Size(100, 100).OnFilesDrop(func(files []string) { calls++ })
		queried += len(zone.DroppedFiles())
	}, 200, 200)
	tt.DropFiles(50, 50, "a.txt")
	tt.Frame()
	tt.Frame()
	if calls != 1 || queried != 0 {
		t.Fatalf("callback calls %d, queried duplicate paths %d", calls, queried)
	}
}
