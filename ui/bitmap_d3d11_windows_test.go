//go:build windows

package ui

import (
	"image"
	"testing"
)

func TestUpdateD3D11Texture(t *testing.T) {
	fallback := image.NewRGBA(image.Rect(0, 0, 8, 6))
	fallback.Pix[0] = 0x7f
	b := NewBitmap(fallback)

	if err := b.UpdateD3D11Texture(123, 456, 8, 6, nil); err != nil {
		t.Fatal(err)
	}
	if b.img.Surface == nil || b.img.Surface.Native != 123 || b.img.Surface.NativeID != 456 {
		t.Fatalf("shared surface = %#v, want handle 123 and id 456", b.img.Surface)
	}
	if b.img.W != 8 || b.img.H != 6 || b.img.Pix[0] != 0x7f {
		t.Fatalf("bitmap lost its fallback or dimensions: %dx%d, first pixel %#x", b.img.W, b.img.H, b.img.Pix[0])
	}

	next := image.NewRGBA(image.Rect(0, 0, 10, 4))
	next.Pix[0] = 0x35
	if err := b.UpdateD3D11Texture(789, 1011, 10, 4, next); err != nil {
		t.Fatal(err)
	}
	if b.img.W != 10 || b.img.H != 4 || b.img.Pix[0] != 0x35 {
		t.Fatalf("bitmap did not take the resized fallback: %dx%d, first pixel %#x", b.img.W, b.img.H, b.img.Pix[0])
	}
}

func TestUpdateD3D11TextureRejectsInvalidInput(t *testing.T) {
	b := NewBitmap(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	for _, tc := range []struct {
		name string
		handle uintptr
		id uint64
		width, height int
		fallback image.Image
	}{
		{name: "no handle", handle: 0, id: 1, width: 2, height: 2},
		{name: "no id", handle: 1, id: 0, width: 2, height: 2},
		{name: "zero width", handle: 1, id: 1, width: 0, height: 2},
		{name: "mismatched fallback", handle: 1, id: 1, width: 3, height: 2, fallback: image.NewRGBA(image.Rect(0, 0, 2, 2))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := b.UpdateD3D11Texture(tc.handle, tc.id, tc.width, tc.height, tc.fallback); err == nil {
				t.Fatal("expected invalid input to be rejected")
			}
		})
	}
}
