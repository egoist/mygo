//go:build windows && (amd64 || arm64)

package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/egoist/mygo/audio"
	windows "github.com/egoist/mygo/media/internal/windows"
)

// A file's sound is decoded as what the audio package mixes: float samples,
// 48 kHz, two channels, so that the sound's own frames count the media's
// time and the audio device is the player's clock.
const (
	soundRate     = 48000
	soundChannels = 2
)

// The limit on the pictures a player reuses buffers for. A player hands out
// one frame at a time and releases it after drawing it, so a handful of
// buffers covers the frames in flight.
const pixBuffers = 4

func openSource(target string, o options) (Source, error) {
	windows.Enter()
	if err := windows.Load(); err != nil {
		return nil, unsupported("Media Foundation: %v", err)
	}
	return openFile(target, o)
}

// An mfSource reads a file's tracks with Media Foundation's own source
// reader: one reader per chosen track, read a sample at a time, rather than
// handed to a player engine, so that MyGo keeps the clock, the seek and the
// choice of tracks, as media promises.
//
// A reader cannot be rewound, so a seek asks it to move: only a reader that
// refuses, or a change of track, is a reader made again.
type mfSource struct {
	// mu guards the readers: a pull holds it while it reads, and a seek
	// or a change of tracks replaces them.
	mu     sync.RWMutex
	closed bool

	target string // what was opened
	path   bool   // a file on this machine rather than a URL

	tracks   []Track // what the file has
	videos   []int   // stream indices of the picture tracks
	audios   []int   // and of the sound tracks
	video    int     // the chosen picture stream, or -1
	audio    int     // and the chosen sound stream, or -1
	pictures bool    // whether pictures are read at all

	vr windows.SourceReader // the reader of the chosen picture stream
	ar windows.SourceReader // and of the chosen sound stream
	vw int                  // the picture's width, as the platform
	vh int                  // negotiated it

	sound *mfSound // the sound as the audio package's decoder

	start time.Duration // where the readers start
	dur   time.Duration // how long the file is
	pool  pixPool       // buffers for the pictures handed out
}

func openFile(target string, o options) (*mfSource, error) {
	url := strings.Contains(target, "://")
	if !url {
		// A file that is not there is an ordinary failure rather than a
		// missing piece of the platform, and saying so here keeps the
		// HRESULT out of it.
		if _, err := os.Stat(target); err != nil {
			return nil, err
		}
	}
	s := &mfSource{target: target, path: !url, video: -1, audio: -1, pictures: o.pictures()}
	if err := s.listTracks(); err != nil {
		return nil, err
	}
	if s.video < 0 && s.audio < 0 {
		return nil, unsupported("%q has no picture or sound", target)
	}
	if err := s.build(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// openReader makes a reader over what was opened. A picture reader is set up
// with the platform's own video processing, which is what turns a file's
// colour into the packed RGB32 the UI draws without a pass of our own.
func (s *mfSource) openReader(video bool) (windows.SourceReader, error) {
	if s.path {
		return windows.OpenReaderFromPath(s.target, video)
	}
	return windows.OpenReaderFromURL(s.target, video)
}

// listTracks records what the file has, and chooses the first track of each
// kind to play. A file whose picture is not the main one, or whose sound is
// not in the language the app wants, is what SelectTracks is for.
func (s *mfSource) listTracks() error {
	r, err := s.openReader(false)
	if err != nil {
		return readerError(err, FeaturePlayback, "")
	}
	defer r.Release()
	defer r.Shutdown()
	if d, err := r.Duration(); err == nil {
		s.dur = d
	}
	for i := 0; ; i++ {
		mt, err := r.NativeMediaType(i, 0)
		if err != nil {
			// No more streams: MF_E_INVALIDSTREAMNUMBER is how the
			// platform says the file has no stream with that number.
			break
		}
		t := Track{Index: i, Duration: s.dur}
		if major, err := mt.MajorType(); err == nil {
			switch major {
			case windows.MajorVideo:
				t.Kind = TrackVideo
				t.Codec = codecOf(mt)
				if w, h, ok := mt.FrameSize(); ok {
					t.Width, t.Height = w, h
				}
				s.videos = append(s.videos, i)
				if s.video < 0 {
					s.video = i
				}
			case windows.MajorAudio:
				t.Kind = TrackAudio
				t.Codec = codecOf(mt)
				if rate, ok := mt.SampleRate(); ok {
					t.SampleRate = rate
				}
				if channels, ok := mt.Channels(); ok {
					t.Channels = channels
				}
				s.audios = append(s.audios, i)
				if s.audio < 0 {
					s.audio = i
				}
			}
		}
		mt.Release()
		s.tracks = append(s.tracks, t)
	}
	return nil
}

// build makes the readers for the chosen tracks, from the current start. Its
// caller holds mu.
func (s *mfSource) build() error {
	s.unbuild()
	if s.video >= 0 && s.pictures {
		r, w, h, err := s.openPicture(s.video)
		if err != nil {
			return err
		}
		s.vr, s.vw, s.vh = r, w, h
	}
	if s.audio >= 0 {
		r, f, err := s.openSound(s.audio)
		if err != nil {
			s.unbuild()
			return err
		}
		s.ar = r
		if s.sound == nil {
			s.sound = &mfSound{s: s}
		}
		s.sound.f = f
	}
	if s.start > 0 {
		return s.move(s.start)
	}
	return nil
}

// openPicture makes the reader of one picture stream, asked for the packed
// 8-bit colour the UI draws: the platform's video processor does the
// conversion, on the way to us.
func (s *mfSource) openPicture(stream int) (windows.SourceReader, int, int, error) {
	r, err := s.openReader(true)
	if err != nil {
		return windows.SourceReader{}, 0, 0, readerError(err, FeaturePlayback, "")
	}
	keep := false
	defer func() {
		if !keep {
			r.Shutdown()
			r.Release()
		}
	}()
	if err := selectStream(r, stream); err != nil {
		return windows.SourceReader{}, 0, 0, err
	}
	codec, err := nativeCodec(r, stream)
	if err != nil {
		return windows.SourceReader{}, 0, 0, err
	}
	mt, err := windows.NewMediaType()
	if err != nil {
		return windows.SourceReader{}, 0, 0, err
	}
	ok := mt.SetGUID(&windows.KeyMajorType, &windows.MajorVideo) == nil &&
		mt.SetGUID(&windows.KeySubtype, &windows.FormatRGB32) == nil
	if !ok {
		mt.Release()
		return windows.SourceReader{}, 0, 0, errors.New("media: asking for a picture format")
	}
	err = r.SetCurrentMediaType(stream, mt)
	mt.Release()
	if err != nil {
		return windows.SourceReader{}, 0, 0, readerError(err, FeaturePlayback, codec)
	}
	current, err := r.CurrentMediaType(stream)
	if err != nil {
		return windows.SourceReader{}, 0, 0, readerError(err, FeaturePlayback, codec)
	}
	defer current.Release()
	if sub, err := current.Subtype(); err == nil && sub != windows.FormatRGB32 && sub != windows.FormatARGB32 {
		return windows.SourceReader{}, 0, 0, unsupported("a picture of format %s", sub)
	}
	w, h, has := current.FrameSize()
	if !has || w <= 0 || h <= 0 {
		return windows.SourceReader{}, 0, 0, unsupported("a picture of %dx%d", w, h)
	}
	keep = true
	return r, w, h, nil
}

// openSound makes the reader of one sound stream, asked for the float samples
// the audio package mixes: the platform's own converter does the sample rate
// and channel conversion where it can, and where it cannot the sound is read
// as it is and converted here.
func (s *mfSource) openSound(stream int) (windows.SourceReader, soundFormat, error) {
	r, err := s.openReader(false)
	if err != nil {
		return windows.SourceReader{}, soundFormat{}, readerError(err, FeaturePlayback, "")
	}
	keep := false
	defer func() {
		if !keep {
			r.Shutdown()
			r.Release()
		}
	}()
	if err := selectStream(r, stream); err != nil {
		return windows.SourceReader{}, soundFormat{}, err
	}
	codec, rate, channels, err := nativeSound(r, stream)
	if err != nil {
		return windows.SourceReader{}, soundFormat{}, err
	}
	asks := []soundFormat{
		{rate: soundRate, channels: soundChannels, bits: 32, float: true},
		{rate: rate, channels: channels, bits: 32, float: true},
		{rate: rate, channels: channels, bits: 16},
		{rate: rate, channels: channels, bits: 8},
	}
	var last error
	for _, ask := range asks {
		if ask.rate <= 0 || ask.channels <= 0 {
			continue
		}
		mt, err := soundType(ask)
		if err != nil {
			return windows.SourceReader{}, soundFormat{}, err
		}
		err = r.SetCurrentMediaType(stream, mt)
		mt.Release()
		if err == nil {
			last = nil
			break
		}
		last = err
	}
	if last != nil {
		return windows.SourceReader{}, soundFormat{}, readerError(last, FeaturePlayback, codec)
	}
	current, err := r.CurrentMediaType(stream)
	if err != nil {
		return windows.SourceReader{}, soundFormat{}, readerError(err, FeaturePlayback, codec)
	}
	defer current.Release()
	f := soundFormat{rate: rate, channels: channels, bits: 16}
	if v, ok := current.SampleRate(); ok && v > 0 {
		f.rate = v
	}
	if v, ok := current.Channels(); ok && v > 0 {
		f.channels = v
	}
	if v, ok := current.BitsPerSample(); ok && v > 0 {
		f.bits = v
	}
	if sub, err := current.Subtype(); err == nil {
		f.float = sub == windows.FormatFloat
	}
	if !f.float && f.bits != 8 && f.bits != 16 && f.bits != 24 && f.bits != 32 {
		return windows.SourceReader{}, soundFormat{}, unsupported("sound of %d bits", f.bits)
	}
	keep = true
	return r, f, nil
}

// selectStream turns every stream off and the one asked for on, so that one
// reader reads one stream and the frames of the others are never decoded.
func selectStream(r windows.SourceReader, stream int) error {
	if err := r.SetStreamSelection(windows.StreamAll, false); err != nil {
		return readerError(err, FeaturePlayback, "")
	}
	if err := r.SetStreamSelection(stream, true); err != nil {
		return readerError(err, FeaturePlayback, "")
	}
	return nil
}

// unbuild gives up the readers, which a change of tracks or a seek the
// platform refuses replaces.
func (s *mfSource) unbuild() {
	if s.vr.Valid() {
		s.vr.Shutdown()
		s.vr.Release()
		s.vr, s.vw, s.vh = windows.SourceReader{}, 0, 0
	}
	if s.ar.Valid() {
		s.ar.Shutdown()
		s.ar.Release()
		s.ar = windows.SourceReader{}
	}
}

// NextFrame returns the next picture of the file, and hands its ownership to
// the caller, which releases it. The end of the file is [ErrEnd].
func (s *mfSource) NextFrame(ctx context.Context) (*Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	windows.Enter()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || !s.vr.Valid() {
		return nil, ErrEnd
	}
	for empty := 0; ; {
		sample, flags, _, err := s.vr.ReadSample(s.video)
		if err != nil {
			return nil, readerError(err, FeaturePlayback, s.videoCodec())
		}
		if flags&windows.FlagEndOfStream != 0 {
			sample.Release()
			return nil, ErrEnd
		}
		if sample.Empty() {
			// A stream with nothing at this time: a live source does
			// that, a file does not. A run of them is the end.
			if flags&windows.FlagError != 0 {
				return nil, errors.New("media: reading the picture")
			}
			if empty++; empty > 8 {
				return nil, ErrEnd
			}
			continue
		}
		f, err := s.picture(sample)
		sample.Release()
		return f, err
	}
}

// videoCodec is the codec of the chosen picture stream, for saying what a
// failure to decode it was about.
func (s *mfSource) videoCodec() Codec {
	if s.video < 0 || s.video >= len(s.tracks) {
		return ""
	}
	return s.tracks[s.video].Codec
}

// picture copies a sample's picture out of its buffer as the packed 8-bit
// RGBA that the UI draws.
func (s *mfSource) picture(sample windows.Sample) (*Frame, error) {
	return pictureOf(sample, &s.pool, s.vw, s.vh, 0, FeaturePlayback)
}

// pictureOf copies a sample's picture out of its buffer as the packed 8-bit
// RGBA that the UI draws, with the buffer it is copied into taken from pool.
// stride is the distance between two of the picture's rows when its buffer
// does not say, or zero for one packed the way the UI wants it.
func pictureOf(sample windows.Sample, pool *pixPool, w, h, stride int, feature Feature) (*Frame, error) {
	buf, err := sample.Buffer()
	if err != nil {
		return nil, unsupported("a picture in this sample")
	}
	defer buf.Release()
	if w <= 0 || h <= 0 {
		return nil, unsupported("a picture of %dx%d", w, h)
	}
	out := pool.get(w * h * 4)
	if err := copyPicture(out, buf, w, h, stride, feature); err != nil {
		pool.put(out)
		return nil, err
	}
	pts, _ := sample.Time()
	dur, _ := sample.Duration()
	return NewFrame(PixelRGBA, w, h, []Plane{{Pix: out, Stride: w * 4}}, pts, dur,
		func() { pool.put(out) }), nil
}

// copyPicture copies a locked sample buffer into dst as 8-bit RGBA, its rows
// top down: Media Foundation stores its colour formats from the bottom row
// up, which its stride says with a sign, and its rows may be further apart
// than the pixels need. stride is what a buffer that says nothing of its own
// is read with, or zero for one packed the way the UI wants it.
func copyPicture(dst []byte, buf windows.Buffer, w, h, stride int, feature Feature) error {
	if twoD, ok := buf.TwoD(); ok {
		defer twoD.Release()
		base, pitch, err := twoD.Lock2D()
		if err != nil {
			return readerError(err, feature, "")
		}
		defer twoD.Unlock2D()
		return copyRows(dst, base, w, h, int(pitch))
	}
	// A buffer with no rows of its own is one flat stretch of pixels.
	base, length, err := buf.Lock()
	if err != nil {
		return readerError(err, feature, "")
	}
	defer buf.Unlock()
	span := stride
	if span < 0 {
		span = -span
	}
	if span < w*4 || length < span*h {
		return unsupported("a picture of %d bytes", length)
	}
	if stride == 0 {
		stride = w * 4
	}
	return copyRows(dst, base, w, h, stride)
}

// copyRows copies rows of 32-bit blue, green, red and unused pixels into dst
// as RGBA, with stride the distance between two rows in memory: negative
// when the picture is stored from its bottom row up, which is how Media
// Foundation stores its colour formats.
func copyRows(dst []byte, base unsafe.Pointer, w, h, stride int) error {
	span := stride
	if span < 0 {
		span = -span
	}
	if span < w*4 {
		return unsupported("a picture with a stride of %d", stride)
	}
	if base == nil {
		return unsupported("a picture with no memory")
	}
	src := unsafe.Slice((*byte)(base), span*h)
	for y := range h {
		row := y
		if stride < 0 {
			row = h - 1 - y
		}
		in := src[row*span : row*span+w*4]
		out := dst[y*w*4 : (y+1)*w*4]
		for x := range w {
			out[x*4+0] = in[x*4+2]
			out[x*4+1] = in[x*4+1]
			out[x*4+2] = in[x*4+0]
			out[x*4+3] = 0xff
		}
	}
	return nil
}

// Sound returns the file's sound, decoded by a reader of its own, or nil
// when the file has none, or none is chosen.
func (s *mfSource) Sound() audio.Decoder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || !s.ar.Valid() || s.sound == nil {
		return nil
	}
	return s.sound
}

// Tracks returns the file's tracks.
func (s *mfSource) Tracks() []Track {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Track, len(s.tracks))
	copy(out, s.tracks)
	return out
}

// SelectTracks chooses the picture and sound stream by their index in
// [Source.Tracks], or -1 for none, and starts reading them from where
// playback is.
func (s *mfSource) SelectTracks(video, audio int) error {
	windows.Enter()
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
		s.build()
		return err
	}
	return nil
}

// Duration returns the file's length, or 0 when it is not known.
func (s *mfSource) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dur
}

// Seek starts reading at t from the file's start.
func (s *mfSource) Seek(t time.Duration) error {
	_, err := s.seek(t)
	return err
}

// seek starts reading at t, and says where it ended up: past the end of the
// file is the end of the file.
func (s *mfSource) seek(t time.Duration) (time.Duration, error) {
	windows.Enter()
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
	s.start = t
	if err := s.move(t); err != nil {
		// A reader whose read is still pending refuses to move, and a
		// file is only read one sample at a time: read it again from
		// the top, which is what a reader made again does.
		if err := s.build(); err != nil {
			return t, err
		}
	}
	return t, nil
}

// move asks the readers to start at t. Media Foundation does not promise to
// land exactly there: it goes to the key frame before it and reads on.
func (s *mfSource) move(t time.Duration) error {
	if s.vr.Valid() {
		if err := s.vr.SetCurrentPosition(t); err != nil {
			return readerError(err, FeaturePlayback, s.videoCodec())
		}
	}
	if s.ar.Valid() {
		if err := s.ar.SetCurrentPosition(t); err != nil {
			return readerError(err, FeaturePlayback, "")
		}
	}
	return nil
}

// Close gives up everything the file holds. Closing twice is fine.
func (s *mfSource) Close() error {
	windows.Enter()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.unbuild()
	return nil
}

// A soundFormat is the format a sound stream was decoded as.
type soundFormat struct {
	rate     int
	channels int
	bits     int
	float    bool
}

// soundType is a media type asking for one sound format, interleaved.
func soundType(f soundFormat) (windows.MediaType, error) {
	mt, err := windows.NewMediaType()
	if err != nil {
		return windows.MediaType{}, err
	}
	sub := &windows.FormatPCM
	if f.float {
		sub = &windows.FormatFloat
	}
	block := f.channels * f.bits / 8
	errs := []error{
		mt.SetGUID(&windows.KeyMajorType, &windows.MajorAudio),
		mt.SetGUID(&windows.KeySubtype, sub),
		mt.SetUINT32(&windows.KeyAudioSamplesPerSecond, uint32(f.rate)),
		mt.SetUINT32(&windows.KeyAudioNumChannels, uint32(f.channels)),
		mt.SetUINT32(&windows.KeyAudioBitsPerSample, uint32(f.bits)),
		mt.SetUINT32(&windows.KeyAudioBlockAlignment, uint32(block)),
		mt.SetUINT32(&windows.KeyAudioAvgBytesPerSecond, uint32(f.rate*block)),
		mt.SetUINT32(&windows.KeyAllSamplesIndependent, 1),
	}
	for _, err := range errs {
		if err != nil {
			mt.Release()
			return windows.MediaType{}, err
		}
	}
	return mt, nil
}

// An mfSound is a file's sound track as the audio package's decoder: a
// reader of its own, pulled a sample at a time, with the silence before the
// track's first frame filled in so that the sound's own frames are the
// media's time, as they are for the pictures.
type mfSound struct {
	s *mfSource
	f soundFormat

	base    time.Duration // the media time the decoder's frame 0 is at
	known   bool          // whether the first buffer has been seen
	held    []float32     // frames read and not handed out yet
	f32     []float32     // converted frames, for sound read as integers
	scratch []byte        // the bytes those frames are in
	pad     int64         // frames of silence before the track's own start
	ended   bool          // whether the track's reader has ended
	empty   int           // buffers in a row that held no sound
}

func (d *mfSound) SampleRate() int {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.f.rate
}

func (d *mfSound) Channels() int {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	return d.f.channels
}

// Len returns the file's length in frames, or 0 when it is not known.
func (d *mfSound) Len() int64 {
	rate := d.SampleRate()
	dur := d.s.Duration()
	if dur <= 0 || rate <= 0 {
		return 0
	}
	return int64(dur) * int64(rate) / int64(time.Second)
}

func (d *mfSound) Read(dst []float32) (int, error) {
	d.s.mu.RLock()
	f := d.f
	d.s.mu.RUnlock()
	if f.channels <= 0 || len(dst) < f.channels {
		return 0, nil
	}
	for {
		if len(d.held) > 0 {
			n := copy(dst, d.held)
			n -= n % f.channels
			d.held = d.held[n:]
			return n / f.channels, nil
		}
		if d.pad > 0 {
			frames := int64(len(dst) / f.channels)
			if frames > d.pad {
				frames = d.pad
			}
			clear(dst[:frames*int64(f.channels)])
			d.pad -= frames
			return int(frames), nil
		}
		if d.ended {
			// The sound track has run out, so the stream is over. The
			// pictures are timed by the wall clock by then, which has
			// been running alongside the sound: a sound track shorter
			// than the file does not stop it playing to the end.
			return 0, io.EOF
		}
		err := d.fill(f)
		if errors.Is(err, ErrEnd) {
			d.ended = true
			continue
		}
		if err != nil {
			return 0, err
		}
	}
}

// fill reads one sample of the sound track into held.
func (d *mfSound) fill(f soundFormat) error {
	sample, err := d.pull()
	if err != nil {
		return err
	}
	pts, hasPTS := sample.Time()
	defer sample.Release()
	buf, err := sample.Buffer()
	if err != nil {
		// A buffer with no sound in it is normal at the start of a
		// track; a run of them is not.
		if d.empty++; d.empty > 8 {
			return io.EOF
		}
		return nil
	}
	defer buf.Release()
	base, length, err := buf.Lock()
	if err != nil {
		return readerError(err, FeaturePlayback, "")
	}
	enough := length >= f.channels*f.bits/8
	if enough {
		if !d.known {
			d.known = true
			if hasPTS && pts > d.base {
				d.pad = int64(pts-d.base) * int64(f.rate) / int64(time.Second)
			}
		}
		if cap(d.scratch) < length {
			d.scratch = make([]byte, length)
		}
		d.scratch = d.scratch[:length]
		copy(d.scratch, unsafe.Slice((*byte)(base), length))
	}
	buf.Unlock()
	if !enough {
		if d.empty++; d.empty > 8 {
			return io.EOF
		}
		return nil
	}
	d.held = d.convert(d.scratch, f)
	d.empty = 0
	return nil
}

// convert turns a buffer of interleaved samples into the float frames the
// audio package mixes.
func (d *mfSound) convert(raw []byte, f soundFormat) []float32 {
	if f.float && f.bits == 32 {
		// The bytes are the frames, as they are for the platform's own
		// float samples.
		return unsafe.Slice((*float32)(unsafe.Pointer(&raw[0])), len(raw)/4)
	}
	frames := len(raw) / (f.channels * f.bits / 8)
	n := frames * f.channels
	if cap(d.f32) < n {
		d.f32 = make([]float32, n)
	}
	d.f32 = d.f32[:n]
	switch f.bits {
	case 8:
		// Eight bit sound is unsigned, and silent is 128.
		for i, v := range raw[:n] {
			d.f32[i] = (float32(v) - 128) / 128
		}
	case 16:
		for i := range n {
			d.f32[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768
		}
	case 24:
		for i := range n {
			v := int32(raw[i*3]) | int32(raw[i*3+1])<<8 | int32(raw[i*3+2])<<16
			d.f32[i] = float32(v<<8) / 2147483648
		}
	case 32:
		for i := range n {
			d.f32[i] = float32(float64(int32(binary.LittleEndian.Uint32(raw[i*4:]))) / 2147483648)
		}
	}
	return d.f32
}

// pull reads the next sample of the sound track.
func (d *mfSound) pull() (windows.Sample, error) {
	windows.Enter()
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	if d.s.closed || !d.s.ar.Valid() {
		return windows.Sample{}, ErrEnd
	}
	sample, flags, _, err := d.s.ar.ReadSample(d.s.audio)
	if err != nil {
		return windows.Sample{}, readerError(err, FeaturePlayback, "")
	}
	if flags&windows.FlagEndOfStream != 0 {
		sample.Release()
		return windows.Sample{}, ErrEnd
	}
	if sample.Empty() {
		return windows.Sample{}, ErrEnd
	}
	return sample, nil
}

// SetPosition starts the sound at a frame of the media's timeline, which with
// a reader means asking it to move: the reader is made again only when it
// will not.
func (d *mfSound) SetPosition(frame int64) error {
	if frame < 0 {
		frame = 0
	}
	rate := d.SampleRate()
	t := time.Duration(frame) * time.Second / time.Duration(rate)
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

// nativeCodec is the codec of a stream as the file has it, and
// nativeSound is the same with the sound's own rate and channels, which is
// what a conversion is asked to start from.
func nativeCodec(r windows.SourceReader, stream int) (Codec, error) {
	mt, err := r.NativeMediaType(stream, 0)
	if err != nil {
		return "", readerError(err, FeaturePlayback, "")
	}
	defer mt.Release()
	return codecOf(mt), nil
}

func nativeSound(r windows.SourceReader, stream int) (Codec, int, int, error) {
	mt, err := r.NativeMediaType(stream, 0)
	if err != nil {
		return "", 0, 0, readerError(err, FeaturePlayback, "")
	}
	defer mt.Release()
	rate, _ := mt.SampleRate()
	channels, _ := mt.Channels()
	return codecOf(mt), rate, channels, nil
}

// codecOf names a media type's codec from its subtype. The ones that are not
// here are ones this version does not name.
func codecOf(mt windows.MediaType) Codec {
	sub, err := mt.Subtype()
	if err != nil {
		return ""
	}
	switch sub {
	case windows.FormatH264:
		return CodecH264
	case windows.FormatHEVC:
		return CodecHEVC
	case windows.FormatAV1:
		return CodecAV1
	case windows.FormatVP80:
		return CodecVP8
	case windows.FormatVP90:
		return CodecVP9
	case windows.FormatMJPEG:
		return CodecMJPEG
	case windows.FormatAAC, windows.FormatRawAAC:
		return CodecAAC
	case windows.FormatMP3:
		return CodecMP3
	case windows.FormatFLAC:
		return CodecFLAC
	case windows.FormatOpus:
		return CodecOpus
	case windows.FormatPCM, windows.FormatFloat:
		return CodecPCM
	}
	return ""
}

// readerError says why the platform would not read what was asked of it:
// something this Windows does not have, which an app can explain, or a real
// failure, which it cannot.
func readerError(err error, feature Feature, codec Codec) error {
	var e *windows.Error
	if !errors.As(err, &e) {
		return err
	}
	switch e.HR {
	case windows.NotImplemented:
		// E_NOTIMPL from MFStartup: no Media Foundation at all.
		return &MissingComponentError{
			Component: "Media Foundation",
			Feature:   feature,
			Hint:      "Windows N needs the Media Feature Pack",
		}
	case windows.CodecNotFound:
		if codec != "" {
			return &MissingComponentError{
				Component: "a " + string(codec) + " decoder",
				Feature:   feature,
				Hint:      "Windows may need its optional codec pack installed",
			}
		}
		return unsupported("decoding this file")
	case windows.UnsupportedByteStreamType, windows.UnsupportedScheme:
		return unsupported("reading this file")
	case windows.InvalidMediaType:
		return unsupported("converting this file to what media plays")
	case windows.AccessDenied:
		return &MissingComponentError{
			Component: "permission to read this file",
			Feature:   feature,
			Hint:      "check the file's permissions",
		}
	}
	return err
}

// contains reports whether xs holds x.
func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
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

// platformCapabilities reports what this Windows' own media stack reads. The
// platform's table is not a promise: H.265 and AV1 need a codec pack that
// may not be installed, and an app that needs one should ask rather than
// assume.
func platformCapabilities() Caps {
	if err := windows.Load(); err != nil {
		return Caps{}
	}
	return Caps{
		Containers: []Container{ContainerMP4, ContainerFragMP4, ContainerWAV, ContainerMP3, ContainerFLAC},
		Decoders:   []Codec{CodecH264, CodecAAC, CodecMP3, CodecFLAC, CodecPCM},
		Encoders:   []Codec{CodecH264, CodecAAC},
		Features: []Feature{
			FeaturePlayback, FeatureEncode,
			FeatureCamera, FeatureScreenCapture,
		},
	}
}
