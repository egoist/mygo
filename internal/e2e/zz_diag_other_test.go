//go:build !darwin

package e2e

import "github.com/egoist/mygo"

func responderState(*mygo.Window) string { return "" }
