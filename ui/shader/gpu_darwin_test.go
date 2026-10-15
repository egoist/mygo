package shader

import (
	"testing"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/gpu/metal"
)

// TestMetal checks that Metal draws a shader, its uniforms and its
// images, a bitmap and a canvas, as the CPU does.
func TestMetal(t *testing.T) {
	s := MustCompile(testSource)
	sc := testScene(s)
	pix, err := metal.RenderOffscreen(sc)
	if err != nil {
		t.Skip("no Metal:", err)
	}
	gputest.Compare(t, "ui-shader-metal", pix, sc.Width*4, sc)
}
