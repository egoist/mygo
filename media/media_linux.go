//go:build linux && (amd64 || arm64)

package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/media/internal/linux"
)

const (
	soundRate     = 48000
	soundChannels = 2
)

// The limit on the pictures a player reuses buffers for. A player hands out
// one frame at a time and releases it after drawing it, so a handful of
// buffers covers the frames in flight.
const pixBuffers = 4

// How long a pull waits before looking at the pipeline again: long enough
// that a frame is usually there, short enough that a seek is not held up by
// a pipeline that has nothing to hand out yet.
const pullWait = 100 * time.Millisecond

// The ends of the pipeline a file's picture and sound come out of. Both ask
// for the formats media works in, on the platform's side: 8-bit RGBA
// pictures, which is what the UI draws, and 48 kHz stereo float samples,
// which is what the audio package mixes. Nothing decodes into a window, and
// nothing is handed over as it arrives: an appsink with no signals connected
// says nothing until it is pulled from, which is what [Source.NextFrame]
// promises.
//
// The sinks hold a couple of buffers and then make the pipeline wait, rather
// than dropping what they hold: a file is not live, and a frame lost to a
// queue is a frame the app never draws.
const (
	videoSink = "videoconvert ! video/x-raw,format=RGBA ! appsink name=video sync=false max-buffers=2"
	soundSink = "audioconvert ! audioresample ! audio/x-raw,format=F32LE,rate=48000,channels=2 " +
		"! appsink name=sound sync=false max-buffers=4"
)

// pluginHint is what a person can do about a missing plugin. It is advice,
// not a contract: which package holds an element is the distribution's
// business, and the media documentation lists a few of them.
const pluginHint = "install the GStreamer plugins this file needs, for example on Debian or Ubuntu: " +
	"apt install gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad " +
	"gstreamer1.0-plugins-ugly gstreamer1.0-libav"

func openSource(target string, o options) (Source, error) {
	if err := linux.Load(); err != nil {
		return nil, &MissingComponentError{
			Component: "GStreamer",
			Feature:   FeaturePlayback,
			Hint: "install it, for example on Debian or Ubuntu: " +
				"apt install libgstreamer1.0-0 gstreamer1.0-plugins-base",
		}
	}
	s := &gstSource{wantAudio: o.sound(), wantVideo: o.pictures(), video: -1, audio: -1}
	if err := s.open(target); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// A gstSource reads a file with GStreamer: a playbin pipeline that decodes
// the chosen tracks into appsinks, which are pulled for their samples rather
// than played. The pipeline is the platform's, but the clock, the seek and
// the choice of tracks are MyGo's, as media promises.
//
// A seek is a flush seek when the pipeline can do one, and a pipeline made
// again when it cannot: one that has already ended, or one that refused the
// move. That is what makes seeking back from the end of a file work.
type gstSource struct {
	// mu guards the pipeline: a pull holds it while it reads, a seek
	// replaces the pipeline.
	mu     sync.RWMutex
	closed bool

	uri       string
	tracks    []Track // what the file has
	videos    []int   // track indices of the picture streams
	audios    []int   // and of the sound streams
	video     int     // the chosen picture track index, or -1
	audio     int     // and the chosen sound track index, or -1
	wantAudio bool    // whether the sound is wanted at all
	wantVideo bool    // and whether the pictures are

	pipe      linux.Element // the playbin
	videoSink linux.AppSink // where pictures come out
	soundSink linux.AppSink // and where sound does
	bus       *linux.Bus    //
	start     time.Duration // where the pipeline starts
	dur       time.Duration // how long the file is
	pool      pixPool       // buffers for the pictures handed out
}

func (s *gstSource) open(target string) error {
	if !strings.Contains(target, "://") {
		// A path that is not there is an ordinary error, not this
		// machine being unable to read the file.
		if _, err := os.Stat(target); err != nil {
			return err
		}
	}
	uri, err := linux.FileURI(target)
	if err != nil {
		return unsupported("reading %q", target)
	}
	streams, err := linux.Describe(uri, 10*time.Second)
	if err != nil {
		return mediaError(err)
	}
	s.uri = uri
	s.listTracks(streams)
	if s.video < 0 && s.audio < 0 {
		return unsupported("%q has no picture or sound", target)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.build()
}

// listTracks records what the file has, and chooses the first stream of each
// kind to play. A file whose picture is not the main one, or whose sound is
// not in the language the app wants, is what SelectTracks is for.
func (s *gstSource) listTracks(streams []linux.Stream) {
	for i, stream := range streams {
		format := stream.Caps.Structure(0)
		caps := format.Name()
		t := Track{Index: i}
		switch {
		case strings.HasPrefix(caps, "video/"), strings.HasPrefix(caps, "image/"):
			t.Kind = TrackVideo
			t.Width, _ = format.Int("width")
			t.Height, _ = format.Int("height")
			t.Codec = codecOf(caps, format)
			s.videos = append(s.videos, i)
			if s.video < 0 {
				s.video = i
			}
		case strings.HasPrefix(caps, "audio/"):
			t.Kind = TrackAudio
			t.SampleRate, _ = format.Int("rate")
			t.Channels, _ = format.Int("channels")
			t.Codec = codecOf(caps, format)
			s.audios = append(s.audios, i)
			if s.audio < 0 {
				s.audio = i
			}
		default:
			t.Kind = TrackOther
		}
		s.tracks = append(s.tracks, t)
		stream.Caps.Release()
	}
}

// build makes the pipeline for the chosen tracks, from the current start.
// Its caller holds mu.
func (s *gstSource) build() error {
	s.unbuild()
	play, err := linux.NewElement("playbin")
	if err != nil {
		return mediaError(err)
	}
	s.pipe = play
	if err := play.Set("uri", s.uri); err != nil {
		s.unbuild()
		return err
	}
	// A sink is always set, never left to the platform's default: the
	// default would open a window of its own, and a stream that is not
	// wanted still has to go somewhere.
	if s.wantVideo && s.video >= 0 {
		sink, err := s.appsink(play, "video-sink", videoSink, "video")
		if err != nil {
			s.unbuild()
			return err
		}
		s.videoSink = sink
	} else if err := fake(play, "video-sink"); err != nil {
		s.unbuild()
		return err
	}
	if s.wantAudio && s.audio >= 0 {
		sink, err := s.appsink(play, "audio-sink", soundSink, "sound")
		if err != nil {
			s.unbuild()
			return err
		}
		s.soundSink = sink
	} else if err := fake(play, "audio-sink"); err != nil {
		s.unbuild()
		return err
	}
	// A kind of stream that is not read is not played at all: its track is
	// turned off as well as sunk, so that the pipeline does not decode what
	// nobody looks at.
	if err := play.Set("current-video", s.ordinal(s.videos, s.chosen(s.video, s.wantVideo))); err != nil {
		s.unbuild()
		return err
	}
	if err := play.Set("current-audio", s.ordinal(s.audios, s.chosen(s.audio, s.wantAudio))); err != nil {
		s.unbuild()
		return err
	}
	if s.bus = play.Bus(); s.bus == nil {
		s.unbuild()
		return errors.New("media: the pipeline has no bus")
	}
	// The pipeline is taken to its first frame before it is handed back:
	// what the machine cannot read or decode is said now, when the file is
	// opened, and not on the first frame.
	if err := s.preroll(); err != nil {
		s.unbuild()
		return err
	}
	if d, ok := play.Duration(); ok {
		s.dur = d
	}
	for i := range s.tracks {
		s.tracks[i].Duration = s.dur
	}
	if play.SetState(linux.StatePlaying) == linux.StateChangeFailure {
		s.unbuild()
		return errors.New("media: the pipeline would not play")
	}
	if s.start > 0 {
		if err := play.Seek(s.start); err != nil {
			s.unbuild()
			return err
		}
	}
	return nil
}

// appsink adds a bin of conversions ending in an appsink to the pipeline,
// and returns the sink, which the caller releases.
func (s *gstSource) appsink(play linux.Element, property, description, name string) (linux.AppSink, error) {
	bin, err := linux.NewBin(description)
	if err != nil {
		return linux.AppSink{}, mediaError(err)
	}
	sink := bin.ByName(name)
	if !sink.Valid() {
		bin.Release()
		return linux.AppSink{}, &MissingComponentError{
			Component: name,
			Feature:   FeaturePlayback,
			Hint:      pluginHint,
		}
	}
	if err := play.Set(property, bin); err != nil {
		sink.Release()
		bin.Release()
		return linux.AppSink{}, err
	}
	bin.Release() // the pipeline owns it now
	return linux.AppSink{Element: sink}, nil
}

// fake sets a stream's sink to one that reads and throws away what arrives,
// for a stream the pipeline decodes but media does not read.
func fake(play linux.Element, property string) error {
	element, err := linux.NewElement("fakesink")
	if err != nil {
		return mediaError(err)
	}
	element.Set("sync", 0) // a fakesink that does not wait for the clock
	if err := play.Set(property, element); err != nil {
		element.Release()
		return err
	}
	element.Release()
	return nil
}

// preroll takes the pipeline to its first frame, and reports what went
// wrong when it cannot get there.
func (s *gstSource) preroll() error {
	if s.pipe.SetState(linux.StatePaused) == linux.StateChangeFailure {
		return errors.New("media: the pipeline would not start")
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		message := s.bus.Pop(linux.MessageError|linux.MessageAsyncDone, pullWait)
		if message == nil {
			if time.Now().After(deadline) {
				return errors.New("media: this file did not open")
			}
			continue
		}
		if message.Type == linux.MessageError {
			err := mediaError(message.PipelineError())
			message.Release()
			return err
		}
		message.Release()
		return nil
	}
}

// unbuild gives up the pipeline, which a seek or another choice of tracks
// replaces. Its caller holds mu.
func (s *gstSource) unbuild() {
	if s.pipe.Valid() {
		s.pipe.SetState(linux.StateNull)
	}
	s.videoSink.Release()
	s.soundSink.Release()
	s.videoSink, s.soundSink = linux.AppSink{}, linux.AppSink{}
	s.bus.Release()
	s.bus = nil
	s.pipe.Release()
	s.pipe = linux.Element{}
}

// chosen is the track the pipeline plays of a kind, or -1 for none: a kind of
// stream the app does not read is not decoded.
func (s *gstSource) chosen(track int, wanted bool) int {
	if !wanted {
		return -1
	}
	return track
}

// ordinal is the position of a track among the streams of its own kind,
// which is how the pipeline is told which one to play.
func (s *gstSource) ordinal(kind []int, index int) int {
	for i, track := range kind {
		if track == index {
			return i
		}
	}
	return -1
}

// ended reports whether the pipeline has run out of what it reads. A
// pipeline that has is not moved but made again.
func (s *gstSource) ended() bool {
	if s.videoSink.Valid() {
		return s.videoSink.IsEOS()
	}
	if s.soundSink.Valid() {
		return s.soundSink.IsEOS()
	}
	return false
}

// NextFrame returns the next picture of the file, and hands its ownership to
// the caller, which releases it. The end of the file is [ErrEnd].
func (s *gstSource) NextFrame(ctx context.Context) (*Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || !s.videoSink.Valid() {
		return nil, ErrEnd
	}
	for {
		sample := s.videoSink.PullSample(pullWait)
		if sample == nil {
			if s.videoSink.IsEOS() {
				return nil, ErrEnd
			}
			// A pull that comes back with nothing is not
			// necessarily the end: the pipeline may be
			// between a flush and the frame that follows it.
			if err := s.failed("the picture"); err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			continue
		}
		// What is read after a seek is read from the time asked for:
		// a pipeline that reads a file from its key frames hands out
		// the pictures before that time as well, and they are not
		// this caller's.
		if pts, ok := sample.Time(); ok && pts < s.start {
			sample.Release()
			continue
		}
		f, err := s.picture(sample)
		sample.Release()
		return f, err
	}
}

// failed reports what the pipeline said went wrong, which is the other way a
// stream stops: the end of a file is [ErrEnd], a stream that breaks is the
// error itself.
func (s *gstSource) failed(what string) error {
	if s.bus == nil {
		return nil
	}
	message := s.bus.Pop(linux.MessageError, 0)
	if message == nil {
		return nil
	}
	err := mediaError(message.PipelineError())
	message.Release()
	if err == nil {
		return fmt.Errorf("media: reading %s", what)
	}
	return err
}

// picture copies a sample's picture out of its buffer, as the packed 8-bit
// RGBA that the UI draws.
func (s *gstSource) picture(sample *linux.Sample) (*Frame, error) {
	return pictureFrame(sample, &s.pool)
}

// pictureFrame copies a sample's picture out of its buffer, as the packed
// 8-bit RGBA that the UI draws, with the buffer it is copied into taken from
// pool and given back when the frame is released.
func pictureFrame(sample *linux.Sample, pool *pixPool) (*Frame, error) {
	caps := sample.Caps()
	defer caps.Release()
	if !caps.Valid() {
		return nil, unsupported("a picture in this sample")
	}
	format := caps.Structure(0)
	if name := format.Name(); name != "video/x-raw" {
		return nil, unsupported("a picture that is %s", name)
	}
	pixels, _ := format.String("format")
	if pixels != "RGBA" && pixels != "BGRA" {
		return nil, unsupported("a picture in %s", pixels)
	}
	w, _ := format.Int("width")
	h, _ := format.Int("height")
	if w <= 0 || h <= 0 {
		return nil, unsupported("a picture of %dx%d", w, h)
	}
	mapped, err := sample.Map()
	if err != nil {
		return nil, err
	}
	defer mapped.Unmap()
	// GStreamer's rows are padded to its own alignment, so a picture is
	// not always one run of w*4 bytes per row: the row length is what the
	// buffer's own size says it is.
	stride := w * 4
	if n := len(mapped.Data); n != stride*h {
		if n%h != 0 || n/h < stride {
			return nil, unsupported("a picture of %d bytes", n)
		}
		stride = n / h
	}
	buf := pool.get(w * h * 4)
	copyRows(buf, mapped.Data, w, h, stride, pixels == "RGBA")
	pts, _ := sample.Time()
	dur, _ := sample.Duration()
	return NewFrame(PixelRGBA, w, h, []Plane{{Pix: buf, Stride: w * 4}},
		pts, dur, func() { pool.put(buf) }), nil
}

// copyRows copies a picture into dst as 8-bit RGBA, a row at a time. A
// picture of a video is opaque here, and its alpha is set rather than taken,
// as a picture format with an alpha channel of its own is not one this
// version reads.
func copyRows(dst, src []byte, w, h, stride int, rgba bool) {
	for y := 0; y < h; y++ {
		in := src[y*stride : y*stride+w*4]
		out := dst[y*w*4 : (y+1)*w*4]
		if rgba {
			copy(out, in)
		} else {
			for x := 0; x < w; x++ {
				out[x*4+0] = in[x*4+2]
				out[x*4+1] = in[x*4+1]
				out[x*4+2] = in[x*4+0]
			}
		}
		for x := 3; x < w*4; x += 4 {
			out[x] = 0xff
		}
	}
}

// Sound returns the file's sound, decoded by the pipeline's sound sink, or
// nil when the file has none or it was not chosen.
func (s *gstSource) Sound() audio.Decoder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.soundSink.Valid() {
		return nil
	}
	return &gstSound{s: s}
}

// Tracks returns the file's tracks, in the order the demuxer has them: a
// file's streams are in the order the container holds them, which is the
// platform's order and not MyGo's.
func (s *gstSource) Tracks() []Track {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Track, len(s.tracks))
	copy(out, s.tracks)
	return out
}

// SelectTracks chooses the picture and sound track by their index in
// [Source.Tracks], or -1 for none, and starts reading them from where
// playback is.
func (s *gstSource) SelectTracks(video, audio int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return io.EOF
	}
	if video >= 0 && !contains(s.videos, video) {
		return fmt.Errorf("media: track %d is not a picture", video)
	}
	if audio >= 0 && !contains(s.audios, audio) {
		return fmt.Errorf("media: track %d is not sound", audio)
	}
	if video == s.video && audio == s.audio {
		return nil
	}
	wasVideo, wasAudio := s.video, s.audio
	s.video, s.audio = video, audio
	if err := s.build(); err != nil {
		s.video, s.audio = wasVideo, wasAudio
		_ = s.build()
		return err
	}
	return nil
}

// Duration returns the file's length, or 0 when it is not known.
func (s *gstSource) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dur
}

// Seek starts reading at t from the file's start.
func (s *gstSource) Seek(t time.Duration) error {
	_, err := s.seek(t)
	return err
}

// seek starts reading at t, and says where it ended up: past the end of the
// file is the end of the file.
func (s *gstSource) seek(t time.Duration) (time.Duration, error) {
	if t < 0 {
		t = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.EOF
	}
	if s.dur > 0 && t > s.dur {
		t = s.dur
	}
	if s.start == t && s.pipe.Valid() && !s.ended() {
		return t, nil
	}
	// A pipeline that has not ended can be moved where it is, which is
	// what a flush seek is for: it drops what it has read and reads on
	// from there.
	if s.pipe.Valid() && !s.ended() {
		if err := s.pipe.Seek(t); err == nil {
			s.start = t
			return t, nil
		}
	}
	// Anything else is a pipeline made again at the new time.
	s.start = t
	if err := s.build(); err != nil {
		return s.start, err
	}
	return t, nil
}

// Close gives up everything the file holds. Closing twice is fine.
func (s *gstSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.unbuild()
	return nil
}

// A gstSound is a file's sound track as the audio package's decoder: the
// pipeline is asked for float samples at 48 kHz in two channels, which is
// what the audio package mixes.
type gstSound struct {
	s       *gstSource
	base    time.Duration // the media time the decoder's frame 0 is at
	known   bool          // whether the first buffer has been seen
	held    []float32     // frames read and not handed out yet
	scratch []byte        // the bytes those frames are in
	pad     int64         // frames of silence before the track's own start
	ended   bool          // whether the track's sink has ended
	empty   int           // buffers in a row that held no sound
}

func (d *gstSound) SampleRate() int { return soundRate }

func (d *gstSound) Channels() int { return soundChannels }

// Len returns the file's length in frames, or 0 when it is not known.
func (d *gstSound) Len() int64 {
	dur := d.s.Duration()
	if dur <= 0 {
		return 0
	}
	return int64(dur) * soundRate / int64(time.Second)
}

func (d *gstSound) Read(dst []float32) (int, error) {
	if len(dst) < soundChannels {
		return 0, nil
	}
	for {
		if len(d.held) > 0 {
			n := copy(dst, d.held)
			n -= n % soundChannels
			d.held = d.held[n:]
			return n / soundChannels, nil
		}
		if d.pad > 0 {
			frames := int64(len(dst) / soundChannels)
			if frames > d.pad {
				frames = d.pad
			}
			clear(dst[:frames*soundChannels])
			d.pad -= frames
			return int(frames), nil
		}
		if d.ended {
			// The sound track has run out, so the stream is over.
			// The pictures are timed by the wall clock by then, which
			// has been running alongside the sound: a sound track
			// shorter than the file does not stop it playing to the
			// end.
			return 0, io.EOF
		}
		err := d.fill()
		if errors.Is(err, ErrEnd) {
			d.ended = true
			continue
		}
		if err != nil {
			return 0, err
		}
	}
}

// fill reads a sample's sound into held, and works out how much silence
// comes before the track's first frame: a sound that starts later than the
// picture does is silent until it does, so that the sound's own frames are
// the media's time. A sample from before the time this decoder reads from is
// not the caller's, and is dropped or trimmed to it.
func (d *gstSound) fill() error {
	sample, err := d.pull()
	if err != nil {
		return err
	}
	defer sample.Release()
	mapped, err := sample.Map()
	if err != nil {
		return err
	}
	defer mapped.Unmap()
	data := mapped.Data
	if len(data) < soundChannels*4 {
		// A buffer with no sound in it is normal at the start of a
		// track; a run of them is not.
		if d.empty++; d.empty > 8 {
			return io.EOF
		}
		return nil
	}
	pts, hasPTS := sample.Time()
	dur, _ := sample.Duration()
	if hasPTS {
		if pts+dur <= d.base {
			return nil
		}
		if !d.known {
			d.known = true
			if pts > d.base {
				d.pad = int64(pts-d.base) * soundRate / int64(time.Second)
			}
		}
	}
	// The sink's own description asks for 32-bit float samples in little
	// endian order, interleaved, so what arrives is what the audio
	// package wants. It is copied out of the mapped buffer rather than
	// read from it where it lies: the pipeline reuses that memory as
	// soon as the buffer is unmapped.
	frames := len(data) / (soundChannels * 4)
	length := frames * soundChannels * 4
	if cap(d.scratch) < length {
		d.scratch = make([]byte, length)
	}
	d.scratch = d.scratch[:length]
	copy(d.scratch, data[:length])
	d.held = unsafe.Slice((*float32)(unsafe.Pointer(&d.scratch[0])), frames*soundChannels)
	if hasPTS && pts < d.base {
		// The sample straddles where reading started, so its head
		// belongs to the time before it.
		skip := int64(d.base-pts) * soundRate / int64(time.Second)
		if skip >= int64(len(d.held)/soundChannels) {
			d.held = nil
			return nil
		}
		d.held = d.held[skip*soundChannels:]
	}
	d.empty = 0
	return nil
}

// pull reads the next sample of the sound track.
func (d *gstSound) pull() (*linux.Sample, error) {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	if d.s.closed || !d.s.soundSink.Valid() {
		return nil, ErrEnd
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		sample := d.s.soundSink.PullSample(pullWait)
		if sample != nil {
			return sample, nil
		}
		if d.s.soundSink.IsEOS() {
			return nil, ErrEnd
		}
		if err := d.s.failed("the sound"); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("media: the sound stopped")
		}
	}
}

// SetPosition starts the sound at a frame of the media's timeline, which
// with a pipeline means reading from there.
func (d *gstSound) SetPosition(frame int64) error {
	if frame < 0 {
		frame = 0
	}
	t := time.Duration(frame) * time.Second / soundRate
	at, err := d.s.seek(t)
	if err != nil {
		return err
	}
	d.held = nil
	d.pad = 0
	d.ended = false
	d.empty = 0
	d.known = false
	d.base = at
	return nil
}

// A pixPool holds the buffers pictures are copied into, so that a player
// handing out frames does not make one per frame. A frame owns its buffer
// until it is released, which returns it here.
type pixPool struct {
	mu   sync.Mutex
	free [][]byte
}

// get takes the buffer that has been free the longest, so that a released
// picture is not written again while the GPU may still be reading it.
func (p *pixPool) get(n int) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) > 0 {
		buf := p.free[0]
		p.free = append(p.free[:0], p.free[1:]...)
		if cap(buf) >= n {
			return buf[:n]
		}
	}
	return make([]byte, n)
}

func (p *pixPool) put(buf []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.free) < pixBuffers {
		p.free = append(p.free, buf)
	}
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// codecOf names the codec a stream's caps carry.
func codecOf(caps string, format linux.Structure) Codec {
	switch caps {
	case "video/x-h264":
		return CodecH264
	case "video/x-h265":
		return CodecHEVC
	case "video/x-av1":
		return CodecAV1
	case "video/x-vp8":
		return CodecVP8
	case "video/x-vp9":
		return CodecVP9
	case "image/jpeg":
		return CodecMJPEG
	case "video/x-prores":
		return CodecProRes
	case "audio/mpeg":
		if version, ok := format.Int("mpegversion"); ok && version == 4 {
			return CodecAAC
		}
		return CodecMP3
	case "audio/x-aac":
		return CodecAAC
	case "audio/x-opus":
		return CodecOpus
	case "audio/x-vorbis":
		return CodecVorbis
	case "audio/x-flac":
		return CodecFLAC
	case "audio/x-raw", "audio/x-wav":
		return CodecPCM
	}
	return Codec("")
}

// mediaError turns what GStreamer said into what media promises: a machine
// missing a plugin is a component that is missing, a file that cannot be
// read is not this machine's fault, and neither is this file broken.
func mediaError(err error) error {
	if err == nil {
		return nil
	}
	var failure *linux.PipelineError
	if errors.As(err, &failure) {
		switch {
		case failure.Missing():
			what := missingElement(failure.Message)
			if what == "" {
				what = "a GStreamer plugin"
			}
			return &MissingComponentError{Component: what, Feature: FeaturePlayback, Hint: pluginHint}
		case failure.NotFound():
			return fmt.Errorf("media: reading this file: %s", failure.Message)
		case failure.Unsupported():
			return unsupported("reading this file: %s", failure.Message)
		}
		return fmt.Errorf("media: %s", failure.Message)
	}
	var missing *linux.MissingError
	if errors.As(err, &missing) {
		return &MissingComponentError{Component: missing.Element, Feature: FeaturePlayback, Hint: pluginHint}
	}
	return err
}

// missingElement picks the element out of GStreamer's own words for a plugin
// it does not have, which read 'no element "avdec_h264"'.
func missingElement(message string) string {
	const marker = `no element "`
	i := strings.Index(message, marker)
	if i < 0 {
		return ""
	}
	rest := message[i+len(marker):]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return rest
}

// platformCapabilities asks the machine what its GStreamer has: a
// distribution installs the plugins its packages bring, so what can be
// decoded is a fact about the machine and not about MyGo, and a machine with
// no GStreamer reports nothing at all.
func platformCapabilities() Caps {
	if linux.Load() != nil {
		return Caps{}
	}
	caps := Caps{Features: []Feature{FeaturePlayback}}
	decoder := func(codec Codec, elements ...string) {
		if has(caps.Decoders, codec) {
			return
		}
		for _, name := range elements {
			if linux.Find(name) {
				caps.Decoders = append(caps.Decoders, codec)
				return
			}
		}
	}
	encoder := func(codec Codec, elements ...string) {
		if has(caps.Encoders, codec) {
			return
		}
		for _, name := range elements {
			if linux.Find(name) {
				caps.Encoders = append(caps.Encoders, codec)
				return
			}
		}
	}
	container := func(container Container, elements ...string) {
		if has(caps.Containers, container) {
			return
		}
		for _, name := range elements {
			if linux.Find(name) {
				caps.Containers = append(caps.Containers, container)
				return
			}
		}
	}

	container(ContainerMP4, "qtdemux")
	container(ContainerFragMP4, "qtdemux")
	container(ContainerMatroska, "matroskademux")
	container(ContainerWebM, "matroskademux")
	container(ContainerOgg, "oggdemux")
	container(ContainerWAV, "wavparse")
	container(ContainerMP3, "id3demux", "mp3parse")
	container(ContainerFLAC, "flacparse", "flacdec")

	decoder(CodecH264, "avdec_h264")
	decoder(CodecHEVC, "avdec_h265")
	decoder(CodecAV1, "av1dec", "dav1ddec")
	decoder(CodecVP8, "vp8dec")
	decoder(CodecVP9, "vp9dec")
	decoder(CodecMJPEG, "jpegdec")
	decoder(CodecProRes, "avdec_prores")
	decoder(CodecAAC, "avdec_aac", "faad")
	decoder(CodecOpus, "opusdec")
	decoder(CodecVorbis, "vorbisdec")
	decoder(CodecMP3, "mpg123audiodec", "mad")
	decoder(CodecFLAC, "flacdec")
	decoder(CodecPCM, "audioconvert")

	encoder(CodecH264, "x264enc")
	encoder(CodecVP8, "vp8enc")
	encoder(CodecVP9, "vp9enc")
	encoder(CodecAV1, "av1enc", "rav1enc")
	encoder(CodecAAC, "fdkaacenc", "voaacenc", "faac")
	encoder(CodecOpus, "opusenc")
	encoder(CodecVorbis, "vorbisenc")
	encoder(CodecFLAC, "flacenc")

	return caps
}
