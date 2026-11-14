//go:build darwin

package ui

import "github.com/egoist/mygo/internal/gpu/metal"

func enablePolyline() { metal.EnablePolyline() }
