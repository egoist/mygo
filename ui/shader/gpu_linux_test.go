//go:build amd64 || arm64

package shader

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/internal/gpu/gl"
	"github.com/egoist/mygo/internal/gpu/gputest"
)

// TestOpenGL checks that OpenGL 3.3 and OpenGL ES 3.0 draw a shader, its
// uniforms and its images, a bitmap and a canvas, as the CPU does.
func TestOpenGL(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	s := MustCompile(testSource)
	for _, es := range []bool{false, true} {
		sc := testScene(s)
		o, err := gl.NewOffscreen(es, sc.Width, sc.Height)
		if err != nil {
			t.Skip(err)
		}
		t.Log(o.Info())
		pix, err := o.Render(sc)
		o.Release()
		if err != nil {
			t.Fatal(err)
		}
		name := "ui-shader-gl"
		if es {
			name += "-es"
		}
		gputest.Compare(t, name, pix, sc.Width*4, sc)
	}
}
