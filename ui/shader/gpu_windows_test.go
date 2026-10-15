package shader

import (
	"testing"

	"github.com/egoist/mygo/internal/gpu/d3d11"
	"github.com/egoist/mygo/internal/gpu/gputest"
)

// TestDirect3D checks that Direct3D 11 draws a shader, its uniforms and
// its images, a bitmap and a canvas, as the CPU does.
func TestDirect3D(t *testing.T) {
	s := MustCompile(testSource)
	if _, err := d3d11.CompileEffect(s.fx.HLSL.Source); err != nil {
		t.Fatal(err)
	}
	sc := testScene(s)
	pix, stride, err := d3d11.RenderOffscreen(sc)
	if err != nil {
		t.Skip("no Direct3D:", err)
	}
	gputest.Compare(t, "ui-shader-d3d11", pix, stride, sc)
}
