package media

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

type pacingSource struct{ frame *Frame }

func (s pacingSource) NextFrame(context.Context) (*Frame, error) { return s.frame, nil }
func (s pacingSource) Close() error                              { return nil }

// Observing Done establishes that NextFrame reached its pacing select.
type pacingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *pacingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestPlayerCloseUnblocksPacing(t *testing.T) {
	for _, playing := range []bool{false, true} {
		name := "paused"
		if playing {
			name = "playing"
		}
		t.Run(name, func(t *testing.T) {
			p := &Player{src: pacingSource{&Frame{PTS: time.Hour}}, playing: playing, wake: make(chan struct{}, 1), resume: make(chan struct{})}
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &pacingContext{Context: base, waiting: make(chan struct{})}
			done := make(chan error, 1)
			go func() { _, err := p.NextFrame(ctx); done <- err }()
			wait.For(t, "player pacing", ctx.waiting)
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			if err := wait.For(t, "closed player returning its frame", done); !errors.Is(err, ErrEnd) {
				t.Fatalf("NextFrame = %v, want ErrEnd", err)
			}
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := p.NextFrame(context.Background()); !errors.Is(err, ErrEnd) {
				t.Fatalf("NextFrame after Close = %v, want ErrEnd", err)
			}
		})
	}
}
