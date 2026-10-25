//go:build darwin

package darwin

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// The UUIDs are the data stores' names on disk: they must never change, or
// the apps that kept data under a StorageID lose it. The values are what
// uuid.uuid5 of Python gives for the namespace 9f1c5a7e-3b0d-4c52-8a61-6d2e40b7f3a9.
func TestStorageUUID(t *testing.T) {
	for id, want := range map[string]string{
		"acct-a":                "d5d43e82-a33c-55fa-99a4-9fdecd191d2b",
		"acct-b":                "12dc7b02-7ab6-59fc-8f34-25742f6cb51f",
		"Acct-A":                "7fde0f76-9a1d-5787-924b-0217333e404d",
		strings.Repeat("x", 64): "abcc2270-37b5-5bc1-824e-0b232c8855e7",
	} {
		if got := storageUUID(id); got != want {
			t.Errorf("storageUUID(%q) = %s, want %s", id, got, want)
		}
	}
}

// The default store needs nothing, and a StorageID needs the data stores of
// an identifier, which macOS 14 has. WebKit's data stores belong to the
// main thread, so internal/e2e makes them.
func TestCheckStorage(t *testing.T) {
	load()
	if err := checkStorage(""); err != nil {
		t.Errorf("the default store: %v", err)
	}
	err := checkStorage("acct-a")
	if has := respondsTo(class("WKWebsiteDataStore"), "dataStoreForIdentifier:"); has != (err == nil) {
		t.Errorf("data stores of an identifier: %v, StorageID: %v", has, err)
	}
	if err != nil && !errors.Is(err, platform.ErrUnsupported) {
		t.Errorf("an error that is not ErrUnsupported: %v", err)
	}
}
