package media

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/egoist/mygo/audio"
)

// A soundSource is a source that also has sound, which it decodes as the
// audio package's decoder so that the player can play it through the same
// mixer and device as everything else in the app. A platform source with
// sound implements it.
type soundSource interface {
	// Sound returns the decoder of the source's sound, or nil when it has
	// none. The player calls it once, before pulling any frame.
	Sound() audio.Decoder
}

// An Option changes how a source is opened.
type Option func(*options)

// options carries what a source is asked to read. Its zero value reads
// everything, so a caller that builds one directly, as a platform's own tests
// do, gets a source that reads both its sound and its pictures.
type options struct {
	noSound bool
	noVideo bool
}

// sound reports whether the sound of a source is read.
func (o options) sound() bool { return !o.noSound }

// pictures reports whether the pictures of a source are read.
func (o options) pictures() bool { return !o.noVideo }

// WithAudio sets whether a file's sound plays. It is on by default, and off
// leaves the sound track undecoded.
func WithAudio(on bool) Option {
	return func(o *options) { o.noSound = !on }
}

// WithVideo sets whether a file's pictures are decoded. They are, by default,
// and off leaves the picture tracks undecoded, which is what an app that
// only wants the sound of a file should ask for.
//
// A source decodes what is read from it. On Linux the platform's pipeline
// reads every chosen track as fast as it can, so a track nobody reads stops
// the file for the tracks that are read: a player whose [Player.NextFrame] is
// never called needs this, while one that draws its pictures does not.
func WithVideo(on bool) Option {
	return func(o *options) { o.noVideo = !on }
}

func newOptions(opts []Option) options {
	o := options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// A Player plays a file: its pictures, its sound, and the clock between
// them. Its pictures come from [Player.NextFrame], and they are handed out
// when they are due, not before the sound at their time has been heard and
// not long after it: a picture whose time has passed while the app was busy
// is dropped rather than shown late.
//
// A player starts playing. Its methods are safe to call from any goroutine.
type Player struct {
	src   Source
	sound *audio.Player
	dur   time.Duration

	mu      sync.Mutex
	playing bool
	// wake carries a seek: what is waiting for a picture's time gives up
	// on it, as that picture is no longer the one coming.
	wake   chan struct{}
	resume chan struct{}
	wall   wallClock
}

// Open opens a media file and returns a player of it. What decodes the file
// and how is the platform's own media stack's choice, as it is for a web
// page's video: a machine that cannot read the file says so with an error
// that names what was missing.
func Open(path string, opts ...Option) (*Player, error) {
	return open(path, newOptions(opts))
}

// OpenURL opens a location rather than a path: what the platform's media
// stack can read over the network, such as an HTTP or RTSP URL. A location
// it cannot read fails as [Open] does.
func OpenURL(url string, opts ...Option) (*Player, error) {
	return open(url, newOptions(opts))
}

func open(target string, o options) (*Player, error) {
	src, err := openSource(target, o)
	if err != nil {
		return nil, err
	}
	p := &Player{
		src:     src,
		playing: true,
		wake:    make(chan struct{}, 1),
		resume:  make(chan struct{}),
	}
	if d, ok := src.(DurationSource); ok {
		p.dur = d.Duration()
	}
	if o.sound() {
		if ss, ok := src.(soundSource); ok {
			if d := ss.Sound(); d != nil {
				p.sound = audio.NewPlayer(d)
				p.sound.Play()
			}
		}
	}
	p.wall.play()
	return p, nil
}

// NextFrame returns the picture that is due now, blocking until its time
// comes, the player is closed, ctx is done, or the file ends with [ErrEnd].
// The caller owns the frame and releases it.
//
// A player whose sound is playing keeps its pictures to the sound: a
// picture is late only because the app was. From a source with no sound the
// player keeps a clock of its own, counting the time it has played.
func (p *Player) NextFrame(ctx context.Context) (*Frame, error) {
	for {
		f, err := p.src.NextFrame(ctx)
		if err != nil {
			return nil, err
		}
		now := p.clock()
		if f.Duration > 0 && now >= f.PTS+f.Duration {
			// Its time came and went while the caller was drawing
			// another: showing it now would only push the next one
			// further behind.
			f.Release()
			continue
		}
		if f.PTS > now {
			err := p.wait(ctx, f.PTS)
			if errors.Is(err, errSeeked) {
				f.Release()
				continue
			}
			if err != nil {
				f.Release()
				return nil, err
			}
		}
		return f, nil
	}
}

var errSeeked = errors.New("media: seeked")

// wait sleeps until the clock reaches until, or something makes the frame
// no longer the one coming: a seek, a pause, ctx.
func (p *Player) wait(ctx context.Context, until time.Duration) error {
	t := time.NewTimer(time.Hour)
	if !t.Stop() {
		<-t.C
	}
	defer t.Stop()
	for {
		d := until - p.clock()
		if d <= 0 {
			return nil
		}
		if !p.Playing() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-p.wake:
				return errSeeked
			case <-p.resume:
			}
			continue
		}
		t.Reset(min(d, 10*time.Millisecond))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.wake:
			return errSeeked
		case <-t.C:
		}
	}
}

// clock returns where playback has reached: the sound that is playing is the
// clock, since the device is what decides when samples are heard. A source
// with no sound, or one whose sound has run out before its pictures, is timed
// by the wall, which runs alongside either way.
func (p *Player) clock() time.Duration {
	if p.sound != nil && p.sound.Playing() {
		return p.sound.Played()
	}
	return p.wall.position()
}

// Play starts or resumes playing the sound and the clock.
func (p *Player) Play() {
	p.mu.Lock()
	was := p.playing
	p.playing = true
	p.mu.Unlock()
	if was {
		return
	}
	if p.sound != nil {
		p.sound.Play()
	}
	p.wall.play()
	select {
	case p.resume <- struct{}{}:
	default:
	}
}

// Pause stops playing the sound and the clock where they are, so that
// [Player.NextFrame] waits rather than drops the pictures it cannot show.
func (p *Player) Pause() {
	p.mu.Lock()
	was := p.playing
	p.playing = false
	p.mu.Unlock()
	if !was {
		return
	}
	if p.sound != nil {
		p.sound.Pause()
	}
	p.wall.pause()
}

// Playing reports whether the player is playing rather than paused.
func (p *Player) Playing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// Position returns where playback has reached.
func (p *Player) Position() time.Duration { return p.clock() }

// Duration returns the file's length, or 0 when it is not known.
func (p *Player) Duration() time.Duration { return p.dur }

// Seek moves playback to t from the start. It returns
// [errors.ErrUnsupported] for a source that cannot seek.
func (p *Player) Seek(t time.Duration) error {
	if t < 0 {
		t = 0
	}
	if p.dur > 0 && t > p.dur {
		t = p.dur
	}
	if s, ok := p.src.(Seekable); ok {
		if err := s.Seek(t); err != nil {
			return err
		}
	} else {
		return unsupported("a source that cannot seek")
	}
	if p.sound != nil {
		p.sound.Seek(t)
	}
	p.wall.seek(t)
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return nil
}

// SetVolume sets the sound's volume, from 0 to 1. It does nothing for a
// source with no sound.
func (p *Player) SetVolume(v float64) {
	if p.sound != nil {
		p.sound.SetVolume(v)
	}
}

// Volume returns the sound's volume.
func (p *Player) Volume() float64 {
	if p.sound == nil {
		return 0
	}
	return p.sound.Volume()
}

// Tracks returns the file's tracks, as [TrackSource] does, or nothing for a
// source with nothing to choose.
func (p *Player) Tracks() []Track {
	if ts, ok := p.src.(TrackSource); ok {
		return ts.Tracks()
	}
	return nil
}

// SelectTracks picks the video and audio track by their index in
// [Player.Tracks], or -1 for none, as [TrackSource] does.
func (p *Player) SelectTracks(video, audio int) error {
	ts, ok := p.src.(TrackSource)
	if !ok {
		return unsupported("a source with one track")
	}
	if err := ts.SelectTracks(video, audio); err != nil {
		return err
	}
	if d, ok := p.src.(DurationSource); ok {
		p.dur = d.Duration()
	}
	if p.sound != nil {
		// The sound is a decoder of its own, holding what it read of
		// the track that was chosen before: start it again where
		// playback is, which the source reads from there too.
		p.sound.Seek(p.Position())
	}
	return nil
}

// Close stops playback, giving up the sound and the source, and with them
// the frames that were not released. Closing twice is fine.
func (p *Player) Close() error {
	if p.sound != nil {
		p.sound.Close()
	}
	if p.src != nil {
		return p.src.Close()
	}
	return nil
}

// A wallClock is the time a player with no sound to keep time with has
// played: it runs while the player plays and stands still while it is
// paused.
type wallClock struct {
	mu      sync.Mutex
	base    time.Duration
	started time.Time
}

func (c *wallClock) position() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started.IsZero() {
		return c.base
	}
	return c.base + time.Since(c.started)
}

func (c *wallClock) play() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started.IsZero() {
		c.started = time.Now()
	}
}

func (c *wallClock) pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started.IsZero() {
		c.base += time.Since(c.started)
		c.started = time.Time{}
	}
}

func (c *wallClock) seek(t time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.base = t
	if !c.started.IsZero() {
		c.started = time.Now()
	}
}
