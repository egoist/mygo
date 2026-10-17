//go:build linux && (amd64 || arm64)

package media

import (
	"errors"
	"sync"
	"time"

	"github.com/egoist/mygo/media/internal/linux"
)

// gstPullWait is how long a pull from a live GStreamer source waits before it
// looks at whether the source is still wanted. A live source is not always
// ready with a picture, and a pull that waits is a pull that does not spin.
const gstPullWait = 100 * time.Millisecond

// A gstLive is a live source read through a GStreamer pipeline: the pipeline
// pushes pictures into an appsink, and a goroutine of its own hands them to the
// queue the caller reads. Cameras and screens share it: what differs between
// them is the pipeline and the words their failures are put in.
type gstLive struct {
	q    *liveFrames
	pipe linux.Element
	sink linux.AppSink
	bus  *linux.Bus
	done chan struct{}
	pool pixPool

	// wrap turns what GStreamer said into what media promises, for the
	// feature the pipeline is for.
	wrap func(error) error

	mu     sync.Mutex
	closed bool
}

// startGSTLive builds the pipeline description, starts it, and reads its
// pictures into q from a goroutine of its own. wrap maps GStreamer's failures
// to media's.
func startGSTLive(description string, q *liveFrames, wrap func(error) error) (*gstLive, error) {
	pipe, err := linux.NewPipelineFrom(description)
	if err != nil {
		return nil, wrap(err)
	}
	sink := pipe.ByName("video")
	if !sink.Valid() {
		pipe.Release()
		return nil, errors.New("media: the pipeline has no sink to read pictures from")
	}
	l := &gstLive{
		q:    q,
		pipe: pipe,
		sink: linux.AppSink{Element: sink},
		bus:  pipe.Bus(),
		done: make(chan struct{}),
		wrap: wrap,
	}
	if pipe.SetState(linux.StatePlaying) == linux.StateChangeFailure {
		err := l.busError()
		l.release()
		if err == nil {
			err = errors.New("media: the pipeline would not start")
		}
		return nil, err
	}
	go l.run()
	return l, nil
}

// run reads the source's pictures until it stops or breaks.
func (l *gstLive) run() {
	defer close(l.done)
	for {
		if l.stopped() {
			return
		}
		sample := l.sink.PullSample(gstPullWait)
		if sample == nil {
			// Closing takes the pipeline down, which reads as an end: that is
			// the caller's own doing, not the source stopping under it.
			if l.stopped() {
				return
			}
			if l.sink.IsEOS() {
				l.q.fail(errors.New("media: the source stopped"))
				return
			}
			if err := l.busError(); err != nil {
				l.q.fail(err)
				return
			}
			continue
		}
		f, err := pictureFrame(sample, &l.pool)
		sample.Release()
		if err != nil {
			l.q.fail(err)
			return
		}
		l.q.push(f)
	}
}

// busError is what the pipeline said went wrong, when it said anything.
func (l *gstLive) busError() error {
	if l.bus == nil {
		return nil
	}
	message := l.bus.Pop(linux.MessageError, 0)
	if message == nil {
		return nil
	}
	defer message.Release()
	return l.wrap(message.PipelineError())
}

// stopped reports whether the source is being closed, which is what a pull
// that came back with nothing looks at.
func (l *gstLive) stopped() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

// close stops the pipeline, which is what lets the reading goroutine go, and
// waits for it before letting go of the pipeline it reads from.
func (l *gstLive) close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()
	l.pipe.SetState(linux.StateNull)
	<-l.done
	l.release()
	return nil
}

// release drops what the source holds of the pipeline.
func (l *gstLive) release() {
	l.sink.Element.Release()
	if l.bus != nil {
		l.bus.Release()
		l.bus = nil
	}
	l.pipe.Release()
}
