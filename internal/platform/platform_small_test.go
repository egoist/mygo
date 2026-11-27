package platform

import (
	"bytes"
	"io"
	"testing"
)

// TestSiteDataAll checks which kinds of site data ask for all of it.
func TestSiteDataAll(t *testing.T) {
	if !SiteDataAll.All() {
		t.Error("SiteDataAll is not all")
	}
	if SiteDataCookies.All() || SiteDataCaches.All() || SiteData(0).All() {
		t.Error("a kind of site data is all of it")
	}
	if !(SiteDataAll | SiteData(1<<31)).All() {
		t.Error("every bit is not all")
	}
}

// TestBytesBody checks the body of a scheme response: read whole, in
// parts, and gone at the end.
func TestBytesBody(t *testing.T) {
	b := BytesBody([]byte("hello"))
	if n := b.(interface{ Len() int }).Len(); n != 5 {
		t.Errorf("Len = %d, want 5", n)
	}
	p := make([]byte, 3)
	if n, err := b.Read(p); n != 3 || err != nil || string(p) != "hel" {
		t.Errorf("Read = %d bytes %q, %v", n, p[:n], err)
	}
	if n := b.(interface{ Len() int }).Len(); n != 2 {
		t.Errorf("Len after reading = %d, want 2", n)
	}
	if n, err := b.Read(p); n != 2 || err != nil || string(p[:n]) != "lo" {
		t.Errorf("Read = %d bytes %q, %v", n, p[:n], err)
	}
	if n, err := b.Read(p); n != 0 || err != io.EOF {
		t.Errorf("Read at the end = %d, %v", n, err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}

	// A body read whole at once.
	b = BytesBody([]byte("data"))
	p = make([]byte, 10)
	n, err := b.Read(p)
	if n != 4 || err != nil || !bytes.Equal(p[:n], []byte("data")) {
		t.Errorf("Read = %d bytes %q, %v", n, p[:n], err)
	}
}

// TestPluginError checks what a plugin's error says: its code and message,
// or just its message.
func TestPluginError(t *testing.T) {
	if got := (&PluginError{Code: "SQLITE_CANTOPEN", Message: "cannot open"}).Error(); got != "SQLITE_CANTOPEN: cannot open" {
		t.Errorf("Error = %q", got)
	}
	if got := (&PluginError{Message: "cannot open"}).Error(); got != "cannot open" {
		t.Errorf("Error without a code = %q", got)
	}
}
