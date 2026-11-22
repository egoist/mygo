package platform

import (
	"errors"
	"fmt"
	"testing"
)

// An app tells a call it can live without by checking its error against
// ErrUnsupported, so every error a backend returns for what it doesn't have
// has to answer that check, and every one says in its own words what the
// platform is missing.
func TestUnsupportedErrors(t *testing.T) {
	browsers := Unsupported("mygo: browsers do not support this")
	phones := Unsupported("mygo: android does not support this")
	tests := []struct {
		name string
		err  error
		is   bool
		want string
	}{
		{"the error itself", ErrUnsupported, true, "not supported on this platform"},
		{"a backend's error", browsers, true, "mygo: browsers do not support this"},
		{"another backend's error", phones, true, "mygo: android does not support this"},
		{"a wrapped backend's error", fmt.Errorf("opening a tray: %w", browsers), true, ""},
		{"a wrapped error itself", fmt.Errorf("opening a tray: %w", ErrUnsupported), true, ""},
		{"an error of another kind", errors.New("mygo: no such file"), false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, ErrUnsupported); got != tt.is {
				t.Errorf("errors.Is(err, ErrUnsupported) = %v, want %v", got, tt.is)
			}
			if got := errors.Is(tt.err, errors.ErrUnsupported); got != tt.is {
				t.Errorf("errors.Is(err, errors.ErrUnsupported) = %v, want %v", got, tt.is)
			}
			if tt.want != "" && tt.err.Error() != tt.want {
				t.Errorf("Error() = %q, want %q", tt.err.Error(), tt.want)
			}
		})
	}
}

// Two of these errors are the same kind of error, whatever they say, as two
// files that are missing are both missing: an app that compares an error
// against another error from the same platform doesn't have to know its words.
func TestUnsupportedErrorsAreOneKind(t *testing.T) {
	a, b := Unsupported("mygo: no tray here"), Unsupported("mygo: no menu here")
	if !errors.Is(a, b) || !errors.Is(b, a) {
		t.Error("errors from Unsupported are not errors.Is of each other")
	}
	if errors.Is(a, errors.New("mygo: no tray here")) {
		t.Error("an error of the same words is errors.Is of an unsupported one")
	}
}
