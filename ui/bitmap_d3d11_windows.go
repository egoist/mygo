//go:build windows

package ui

import (
	"errors"
	"image"

	"github.com/egoist/mygo/internal/scene"
)

// UpdateD3D11Texture shows a frame from a shared Direct3D 11 texture, without
// copying its pixels through the CPU. The texture must be BGRA, shareable by an
// NT handle, and created with a keyed mutex. id uniquely identifies that
// texture for as long as any renderer may keep it open; do not reuse it for a
// different texture.
//
// Keep handle open while the bitmap may be drawn. The producer and renderer
// synchronize access with keyed mutex key 0: the producer acquires it before
// writing and releases it after writing; the renderer acquires it while
// drawing and releases it once its GPU commands have been issued.
//
// fallback, when non-nil, is a CPU image to retain for software rendering or
// when the native texture cannot be opened. When nil, the bitmap's existing
// CPU pixels are retained as the fallback. Pass a new fallback when the
// texture's size changes if the old one is no longer suitable.
//
// Call on the UI thread, or from Window.Update. The bitmap's existing
// dimensions are changed to width×height.
func (b *Bitmap) UpdateD3D11Texture(handle uintptr, id uint64, width, height int, fallback image.Image) error {
	if b == nil || b.img == nil {
		return errors.New("ui: nil bitmap")
	}
	if handle == 0 || id == 0 {
		return errors.New("ui: D3D11 texture handle and id must be nonzero")
	}
	if width <= 0 || height <= 0 {
		return errors.New("ui: D3D11 texture dimensions must be positive")
	}
	if b.img.Scene != nil {
		return errors.New("ui: cannot update a canvas with a D3D11 texture")
	}
	if fallback != nil && (fallback.Bounds().Dx() != width || fallback.Bounds().Dy() != height) {
		return errors.New("ui: D3D11 fallback image dimensions do not match the texture")
	}

	b.remake, b.source = nil, nil
	if b.released {
		b.released = false
		b.img.Restore, b.img.Dropped = nil, nil
		for _, lvl := range b.levels {
			lvl.Restore, lvl.Dropped = nil, nil
		}
	}
	if b.img.W != width || b.img.H != height {
		b.img.Resize(width, height)
		b.w, b.h = width, height
		b.levels = nil
		if fallback == nil {
			clear(b.img.Pix)
		}
	}
	if fallback != nil {
		b.img.Pix = scene.NewImage(fallback).Pix
	}
	b.img.Surface = &scene.Surface{
		Format:   scene.SurfaceBGRA,
		Native:   handle,
		NativeID: id,
	}
	b.img.Changed()
	b.fresh = 0
	return nil
}
