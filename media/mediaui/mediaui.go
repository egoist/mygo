// Package mediaui shows what a media source plays in a window MyGo draws
// itself: the bridge between media, which hands out frames, and ui, which
// draws them.
//
// A [Video] owns a bitmap the frames are drawn into, and the goroutine that
// pulls them from the source when they are due. Pulling and pacing happen
// off the main thread, as everything media does; the bitmap is changed on
// the main thread through the window's Update, and the frame before is
// released only once the frame after it is shown, as the GPU has uploaded
// the pixels by then.
package mediaui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"

	"github.com/egoist/mygo/media"
	"github.com/egoist/mygo/ui"
)

// Window is where a video shows its frames: MyGo's window, whose Update
// runs a function on the main thread and then redraws what the window shows.
type Window interface {
	Update(fn func())
}

// A Video shows a source's frames: a bitmap the frames are drawn into, and
// the goroutine that pulls them and shows them when they are due. The source
// is a file being played or a camera being watched.
//
// Its methods are safe to call from any goroutine.
type Video struct {
	win    Window
	src    media.Source
	bitmap *ui.Bitmap
	fit    ui.Fit

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// An Option changes how a video shows its frames.
type Option func(*Video)

// WithFit sets how a frame fills the element: [ui.Contain] by default, which
// shows the whole picture with bars around it where the element is not the
// picture's shape.
func WithFit(f ui.Fit) Option {
	return func(v *Video) { v.fit = f }
}

// NewVideo shows a source's frames in a window. It shows a black picture
// until the first frame is due: pull the frames with [Video.Start], and show
// [Video.Element] where the picture belongs.
func NewVideo(w Window, src media.Source, opts ...Option) *Video {
	v := &Video{
		win:    w,
		src:    src,
		bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 16, 9))),
		fit:    ui.Contain,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(v)
		}
	}
	return v
}

// Element is where the frames show: an image of the video's bitmap, at the
// size of the picture, which the element's own options then scale.
func (v *Video) Element(c *ui.Context) ui.Element {
	v.mu.Lock()
	fit := v.fit
	v.mu.Unlock()
	return ui.Image(c, v.bitmap).Fit(fit)
}

// SetFit changes how the frames fill the element, as [WithFit] sets it.
func (v *Video) SetFit(f ui.Fit) {
	v.mu.Lock()
	v.fit = f
	v.mu.Unlock()
}

// Start pulls the source's frames and shows them as they are due. It returns
// at once; the frames are shown on the main thread as the source hands them
// out, and the video stops at the end of the source, on an error, or with
// [Video.Stop]. Starting a video that is already started does nothing.
func (v *Video) Start() {
	v.mu.Lock()
	if v.cancel != nil {
		v.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	v.ctx, v.cancel, v.done = ctx, cancel, done
	v.err = nil
	v.mu.Unlock()
	go func() {
		defer close(done)
		err := v.run(ctx)
		if errors.Is(err, context.Canceled) {
			err = nil // stopped, not ended
		}
		v.mu.Lock()
		v.err = err
		v.mu.Unlock()
	}()
}

// Stop stops showing frames, and waits for the frame in flight. The video can
// be started again, which continues from where the source is.
func (v *Video) Stop() {
	v.mu.Lock()
	cancel, done := v.cancel, v.done
	v.ctx, v.cancel, v.done = nil, nil, nil
	v.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// Err returns why the frames stopped, or nil while they are being shown and
// after a [Video.Stop]. The end of the file is [media.ErrEnd].
func (v *Video) Err() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.err
}

// releaseFrame gives a frame's memory back to its source. It is a variable so
// that a test can watch the order in which frames are shown and given up.
var releaseFrame = func(f *media.Frame) { f.Release() }

// run shows the frames the source hands out until it ends, something fails,
// or ctx is done.
//
// A frame's pixels become the bitmap's own, so they must stay valid until the
// main thread has drawn them: the frame before is released on the main thread
// once the frame after it has replaced it there, and the buffers the frames
// are copied into are reused in the order they were released, so a frame's
// pixels are never written again while they are still the bitmap's.
func (v *Video) run(ctx context.Context) error {
	var showing *media.Frame
	for {
		f, err := v.src.NextFrame(ctx)
		if err != nil {
			return err
		}
		img := f.Image()
		if img == nil {
			f.Release()
			return fmt.Errorf("mediaui: a frame of %s, which this version cannot show", f.Format)
		}
		shown := make(chan struct{})
		v.win.Update(func() {
			v.bitmap.Update(img)
			if showing != nil {
				// The frame before, which the bitmap no longer
				// shows and the GPU has uploaded by now.
				releaseFrame(showing)
			}
			showing = f
			close(shown)
		})
		select {
		case <-shown:
		case <-ctx.Done():
			// The update may still run, and it releases what it
			// shows; the frame it has not taken yet is the pool's
			// again when it does.
			return ctx.Err()
		}
	}
}
