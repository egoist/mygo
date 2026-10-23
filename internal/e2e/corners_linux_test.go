//go:build linux && (amd64 || arm64)

package e2e

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/linux"
	"github.com/egoist/mygo/ui"
)

func windowPixels(w *mygo.Window) image.Image {
	var data []byte
	mygo.RunOnMain(func() { data = linux.TestWindowOnScreen(w.NativeHandle()) })
	img, _ := png.Decode(bytes.NewReader(data))
	return img
}

func TestRoundedHiddenTitleBar(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "webview"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			for _, style := range []mygo.TitleBarStyle{mygo.TitleBarHidden, mygo.TitleBarHiddenInset} {
				opts := mygo.WindowOptions{Width: 320, Height: 240, TitleBarStyle: style, HideWindowButtons: true}
				if native {
					opts.Content = ui.View(func(c *ui.Context) {
						ui.Box(c).Fill().Background(ui.RGB(240, 40, 30)).Children(func() {
							ui.Box(c).Absolute().Top(0).Left(0).Size(24, 24).Background(ui.RGB(20, 180, 40))
							ui.Box(c).Absolute().Top(0).Right(0).Size(24, 24).Background(ui.RGB(30, 60, 230))
							ui.Box(c).Absolute().Bottom(0).Left(0).Size(24, 24).Background(ui.RGB(230, 180, 10))
							ui.Box(c).Absolute().Bottom(0).Right(0).Size(24, 24).Background(ui.RGB(160, 40, 210))
						})
					})
				}
				w := newWindow(t, opts)
				if !native {
					w.Page().LoadHTML(`<body style="margin:0;background:rgb(240,40,30)">
<div style="position:fixed;top:0;left:0;width:24px;height:24px;background:rgb(20,180,40)"></div>
<div style="position:fixed;top:0;right:0;width:24px;height:24px;background:rgb(30,60,230)"></div>
<div style="position:fixed;bottom:0;left:0;width:24px;height:24px;background:rgb(230,180,10)"></div>
<div style="position:fixed;bottom:0;right:0;width:24px;height:24px;background:rgb(160,40,210)"></div></body>`, "")
					waitFor(t, w, "document.body && getComputedStyle(document.body).backgroundColor === 'rgb(240, 40, 30)'")
				}
				var x11, composited bool
				mygo.RunOnMain(func() { x11, composited = linux.TestWindowOutlineEnvironment(w.NativeHandle()) })
				if !x11 {
					t.Skip("reading the window's backing pixels needs X11")
				}
				if os.Getenv("MYGO_E2E_COMPOSITED") != "" {
					eventually(t, "the compositing manager", func() bool {
						mygo.RunOnMain(func() { _, composited = linux.TestWindowOutlineEnvironment(w.NativeHandle()) })
						return composited
					})
				}
				check := func(rounded bool) {
					t.Helper()
					var last image.Image
					eventually(t, "the window's painted outline", func() bool {
						last = windowPixels(w)
						if last == nil {
							return false
						}
						b := last.Bounds()
						r, _, _, a := last.At(b.Dx()/2, b.Dy()/2).RGBA()
						if r < 50000 || a != 65535 {
							return false
						}
						for _, p := range []image.Point{{0, 0}, {b.Dx() - 1, 0}, {0, b.Dy() - 1}, {b.Dx() - 1, b.Dy() - 1}} {
							_, _, _, a := last.At(p.X, p.Y).RGBA()
							if rounded && a != 0 || !rounded && a != 65535 {
								return false
							}
						}
						// Different colors at each corner catch wrong source
						// offsets and flipped GL rows, including at scale 2.
						scale := deviceScale(w)
						in := int(8 * scale)
						points := []image.Point{{in, in}, {b.Dx() - 1 - in, in}, {in, b.Dy() - 1 - in}, {b.Dx() - 1 - in, b.Dy() - 1 - in}}
						colors := []color.NRGBA{{20, 180, 40, 255}, {30, 60, 230, 255}, {230, 180, 10, 255}, {160, 40, 210, 255}}
						for i, p := range points {
							if got := color.NRGBAModel.Convert(last.At(p.X, p.Y)).(color.NRGBA); got != colors[i] {
								return false
							}
						}
						return true
					})
					var corner, center, available bool
					mygo.RunOnMain(func() {
						corner, available = linux.TestWindowInputAt(w.NativeHandle(), 0, 0)
						center, _ = linux.TestWindowInputAt(w.NativeHandle(), last.Bounds().Dx()/2, last.Bounds().Dy()/2)
					})
					if available && (corner == rounded || !center) {
						t.Errorf("input shape: corner=%v center=%v, rounded=%v", corner, center, rounded)
					}
					if rounded {
						soft := false
						for y := 0; y < int(12*deviceScale(w)); y++ {
							for x := 0; x < int(12*deviceScale(w)); x++ {
								_, _, _, a := last.At(x, y).RGBA()
								soft = soft || a > 0 && a < 65535
							}
						}
						if !soft {
							t.Error("the curved edge has no antialiased pixels")
						}
					}
					if out := os.Getenv("MYGO_CORNERS_SNAPSHOT"); out != "" {
						f, err := os.Create(out + "-" + name + ".png")
						if err != nil {
							t.Fatal(err)
						}
						defer f.Close()
						png.Encode(f, last)
					}
				}
				check(composited)
				if native && os.Getenv("MYGO_GPU") == "1" {
					if how, pix, _, _, _ := glSurface(w); how != "opengl" || len(pix) == 0 {
						t.Fatalf("renderer = %q, want OpenGL", how)
					}
				}
				w.SetSize(420, 280)
				eventually(t, "the resized buffer", func() bool {
					img := windowPixels(w)
					return img != nil && img.Bounds().Dx() == int(420*deviceScale(w)) && img.Bounds().Dy() == int(280*deviceScale(w))
				})
				check(composited)
				// Xvfb alone has no window manager. The optional run with
				// one exercises the real maximize/full-screen transitions.
				if os.Getenv("MYGO_E2E_WINDOW_MANAGER") != "" {
					w.Maximize()
					eventually(t, "maximized", w.IsMaximized)
					check(false)
					w.Unmaximize()
					eventually(t, "restored", func() bool { return !w.IsMaximized() })
					check(composited)
					w.SetFullScreen(true)
					eventually(t, "full screen", w.IsFullScreen)
					check(false)
					w.SetFullScreen(false)
					eventually(t, "left full screen", func() bool { return !w.IsFullScreen() })
					check(composited)
				}
				w.Destroy()
			}
		})
	}
}

// Explicit transparency and frameless windows belong to the app, which
// can draw its own outline. Do not silently round a full rectangular view.
func TestRoundedWindowOptOut(t *testing.T) {
	for _, transparent := range []bool{false, true} {
		opts := mygo.WindowOptions{Width: 320, Height: 240, TitleBarStyle: mygo.TitleBarHidden,
			Transparent: transparent, Frameless: !transparent, HideWindowButtons: true,
			Content: ui.View(func(c *ui.Context) { ui.Box(c).Fill().Background(ui.RGB(240, 40, 30)) })}
		w := newWindow(t, opts)
		var x11 bool
		mygo.RunOnMain(func() { x11, _ = linux.TestWindowOutlineEnvironment(w.NativeHandle()) })
		if !x11 {
			t.Skip("reading the window's backing pixels needs X11")
		}
		eventually(t, "the app's rectangular outline", func() bool {
			img := windowPixels(w)
			if img == nil {
				return false
			}
			b := img.Bounds()
			for _, p := range []image.Point{{0, 0}, {b.Dx() - 1, 0}, {0, b.Dy() - 1}, {b.Dx() - 1, b.Dy() - 1}} {
				r, _, _, a := img.At(p.X, p.Y).RGBA()
				if r < 50000 || a != 65535 {
					return false
				}
			}
			return true
		})
		w.Destroy()
	}
}

func TestRoundedWindowCompositorChanges(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 320, Height: 240, TitleBarStyle: mygo.TitleBarHidden,
		HideWindowButtons: true, Content: ui.View(func(c *ui.Context) { ui.Box(c).Fill().Background(ui.RGB(240, 40, 30)) })})
	var x11, composited bool
	mygo.RunOnMain(func() { x11, composited = linux.TestWindowOutlineEnvironment(w.NativeHandle()) })
	if !x11 || composited {
		t.Skip("needs an X11 display without a compositing manager")
	}
	program, err := exec.LookPath("xcompmgr")
	if err != nil {
		t.Skip("xcompmgr is not installed")
	}
	check := func(rounded bool) {
		t.Helper()
		eventually(t, "the visual and input outline after a compositor change", func() bool {
			var corner, available bool
			mygo.RunOnMain(func() {
				_, composited = linux.TestWindowOutlineEnvironment(w.NativeHandle())
				corner, available = linux.TestWindowInputAt(w.NativeHandle(), 0, 0)
			})
			if composited != rounded || available && corner == rounded {
				return false
			}
			img := windowPixels(w)
			if img == nil {
				return false
			}
			r, _, _, a := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
			if r < 50000 || a != 65535 {
				return false
			}
			_, _, _, a = img.At(0, 0).RGBA()
			return rounded && a == 0 || !rounded && a == 65535
		})
	}
	check(false)
	cmd := exec.Command(program, "-a")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	check(true)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	check(false)
}
