//go:build darwin

package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/egoist/mygo/audio"
	darwin "github.com/egoist/mygo/media/internal/darwin"
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
	if err := darwin.Load(); err != nil {
		return nil, unsupported("AVFoundation: %v", err)
	}
	var (
		s   *avSource
		err error
	)
	darwin.WithPool(func() { s, err = openFile(target, o) })
	if err != nil {
		return nil, err
	}
	return s, nil
}

// An avSource reads a file's tracks with AVAssetReader: one reader per
// track, pulled for its samples rather than handed to AVPlayer, so that
// MyGo keeps the clock, the seek and the choice of tracks, as media
// promises. A seek is a reader made again at the new time, as a reader
// cannot be rewound.
type avSource struct {
	// mu guards the readers: a pull holds it while it reads, a seek
	// replaces them.
	mu     sync.RWMutex
	closed bool

	asset    uintptr         // AVURLAsset
	tracks   []Track         // what the file has
	handles  map[int]uintptr // track index -> retained AVAssetTrack
	videos   []int           // track indices of the video tracks
	audios   []int           // and of the sound tracks
	video    int             // the chosen video track index, or -1
	audio    int             // and the chosen sound track index, or -1
	pictures bool            // whether pictures are read at all
	vreader  uintptr         // AVAssetReader of the chosen video track
	vout     uintptr         // its AVAssetReaderTrackOutput
	areader  uintptr         // the same for the chosen sound track
	aout     uintptr         //
	start    time.Duration   // where the readers start
	dur      time.Duration   // how long the file is
	pool     pixPool         // buffers for the pictures handed out
}

func openFile(target string, o options) (*avSource, error) {
	var url uintptr
	if strings.Contains(target, "://") {
		url = darwin.URL(target)
	} else {
		url = darwin.FileURL(target)
	}
	if url == 0 {
		return nil, unsupported("reading %q", target)
	}
	asset := darwin.SendID2(darwin.Alloc(darwin.Class("AVURLAsset")), darwin.Sel("initWithURL:options:"), url, 0)
	if asset == 0 {
		return nil, unsupported("reading %q", target)
	}
	s := &avSource{
		asset:    asset,
		handles:  make(map[int]uintptr),
		video:    -1,
		audio:    -1,
		pictures: o.pictures(),
	}
	s.dur = darwin.Time(darwin.SendTime(asset, darwin.Sel("duration")))
	s.listTracks()
	if s.video < 0 && s.audio < 0 {
		s.Close()
		return nil, unsupported("%q has no picture or sound", target)
	}
	if err := s.build(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// listTracks records what the file has, and chooses the first track of each
// kind to play. A file whose video is not the main one, or whose sound is
// not in the language the app wants, is what SelectTracks is for.
func (s *avSource) listTracks() {
	all := darwin.SendID(s.asset, darwin.Sel("tracks"))
	for i, n := 0, darwin.ArrayCount(all); i < n; i++ {
		track := darwin.ArrayAt(all, i)
		t := Track{Index: i, Duration: s.dur}
		if r := darwin.SendTimeRangeOf(track, darwin.Sel("timeRange")); darwin.Time(r.Duration) > 0 {
			t.Duration = darwin.Time(r.Duration)
		}
		switch darwin.String(darwin.SendID(track, darwin.Sel("mediaType"))) {
		case "vide":
			t.Kind = TrackVideo
			size := darwin.SendSize(track, darwin.Sel("naturalSize"))
			t.Width, t.Height = int(size.Width), int(size.Height)
			t.Codec = codecOf(track)
			s.videos = append(s.videos, i)
			if s.video < 0 {
				s.video = i
			}
		case "soun":
			t.Kind = TrackAudio
			t.Codec = codecOf(track)
			if fd := firstFormat(track); fd != 0 {
				if asbd := darwin.SoundFormat(fd); asbd != nil {
					t.SampleRate = int(asbd.SampleRate)
					t.Channels = int(asbd.ChannelsPerFrame)
				}
			}
			s.audios = append(s.audios, i)
			if s.audio < 0 {
				s.audio = i
			}
		}
		s.tracks = append(s.tracks, t)
		s.handles[i] = darwin.Retain(track)
	}
}

// build makes the readers for the chosen tracks, from the current start.
// Its caller holds mu, and has an autorelease pool of its own: everything
// the calls inside make is autoreleased.
func (s *avSource) build() error {
	s.unbuild()
	if s.video >= 0 && s.pictures {
		reader, out, err := s.reader(s.handles[s.video], videoSettings())
		if err != nil {
			return err
		}
		s.vreader, s.vout = reader, out
	}
	if s.audio >= 0 {
		reader, out, err := s.reader(s.handles[s.audio], audioSettings())
		if err != nil {
			return err
		}
		s.areader, s.aout = reader, out
	}
	return nil
}

// reader makes a reader of one track, from the current start.
func (s *avSource) reader(track, settings uintptr) (reader, out uintptr, err error) {
	reader = darwin.SendID2(darwin.Alloc(darwin.Class("AVAssetReader")), darwin.Sel("initWithAsset:error:"), s.asset, 0)
	if reader == 0 {
		return 0, 0, unsupported("a reader for this file")
	}
	// The output comes autoreleased from a factory method: keep a
	// reference of our own, so that giving it up again is ours to do and
	// the pool's later release is not a second one.
	out = darwin.Retain(darwin.SendID2(darwin.Class("AVAssetReaderTrackOutput"), darwin.Sel("assetReaderTrackOutputWithTrack:outputSettings:"), track, settings))
	if out == 0 {
		darwin.Release(reader)
		return 0, 0, unsupported("an output for a track")
	}
	// The pictures are copied out of the decoder's own buffers straight
	// away, so it need not copy them for us.
	darwin.SendVoid1(out, darwin.Sel("setAlwaysCopiesSampleData:"), 0)
	// addOutput: returns nothing: it raises an exception for an output it
	// will not take rather than saying no, so ask canAddOutput: first.
	if !darwin.SendBool1(reader, darwin.Sel("canAddOutput:"), out) {
		darwin.Release(out)
		darwin.Release(reader)
		return 0, 0, unsupported("an output for this track")
	}
	darwin.SendVoid1(reader, darwin.Sel("addOutput:"), out)
	if s.start > 0 || s.dur > 0 {
		length := s.dur - s.start
		if length <= 0 {
			// The file's length is not known: read on, as the
			// reader does by default.
			length = 0
		}
		if length > 0 {
			darwin.SendTimeRange(reader, darwin.Sel("setTimeRange:"), darwin.TimeRange(s.start, length))
		}
	}
	if !darwin.SendBool(reader, darwin.Sel("startReading")) {
		err := darwin.ErrorString(darwin.SendID(reader, darwin.Sel("error")))
		darwin.Release(out)
		darwin.Release(reader)
		return 0, 0, fmt.Errorf("media: reading this file: %s", err)
	}
	return reader, out, nil
}

// unbuild gives up the readers, which a seek replaces.
func (s *avSource) unbuild() {
	if s.vreader != 0 {
		darwin.SendVoid(s.vreader, darwin.Sel("cancelReading"))
		darwin.Release(s.vout)
		darwin.Release(s.vreader)
		s.vreader, s.vout = 0, 0
	}
	if s.areader != 0 {
		darwin.SendVoid(s.areader, darwin.Sel("cancelReading"))
		darwin.Release(s.aout)
		darwin.Release(s.areader)
		s.areader, s.aout = 0, 0
	}
}

// NextFrame returns the next picture of the file, and hands its ownership to
// the caller, which releases it. The end of the file is [ErrEnd].
func (s *avSource) NextFrame(ctx context.Context) (*Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || s.vout == 0 {
		return nil, ErrEnd
	}
	var sb uintptr
	darwin.WithPool(func() { sb = darwin.SendID(s.vout, darwin.Sel("copyNextSampleBuffer")) })
	if sb == 0 {
		return nil, s.why(s.vreader, "picture")
	}
	f, err := s.picture(sb)
	darwin.CFReleaseItem(sb)
	return f, err
}

// why says why a reader gave up its samples: the end of what it reads, or
// what the platform said was wrong.
func (s *avSource) why(reader uintptr, what string) error {
	if darwin.SendInt(reader, darwin.Sel("status")) == 3 { // Failed
		return fmt.Errorf("media: reading the %s: %s", what, darwin.ErrorString(darwin.SendID(reader, darwin.Sel("error"))))
	}
	return ErrEnd
}

// picture copies a sample's picture out of its pixel buffer, as the packed
// 8-bit RGBA that the UI draws.
func (s *avSource) picture(sb uintptr) (*Frame, error) {
	pb := darwin.SampleImage(sb)
	if pb == 0 {
		return nil, unsupported("a picture in this sample")
	}
	if status := darwin.PixelLock(pb); status != 0 {
		return nil, fmt.Errorf("media: locking a picture: %d", status)
	}
	defer darwin.PixelUnlock(pb)
	w, h := darwin.PixelSize(pb)
	base := darwin.PixelBase(pb)
	if w <= 0 || h <= 0 || base == nil {
		return nil, unsupported("a picture of %dx%d", w, h)
	}
	buf := s.pool.get(w * h * 4)
	if err := copyPicture(buf, base, w, h, darwin.PixelStride(pb), darwin.PixelFormat(pb)); err != nil {
		s.pool.put(buf)
		return nil, err
	}
	return NewFrame(PixelRGBA, w, h, []Plane{{Pix: buf, Stride: w * 4}},
		darwin.SamplePTS(sb), darwin.SampleDuration(sb), func() { s.pool.put(buf) }), nil
}

// copyPicture copies a locked picture into dst as 8-bit RGBA: the bytes of a
// BGRA picture are swapped. A picture of a video is opaque here, and its
// alpha is set rather than taken, as a picture format with an alpha channel
// of its own is not one this version reads.
func copyPicture(dst []byte, base unsafe.Pointer, w, h, stride int, format uint32) error {
	if stride < w*4 {
		return unsupported("a picture with a stride of %d", stride)
	}
	src := unsafe.Slice((*byte)(base), stride*h)
	for y := 0; y < h; y++ {
		in := src[y*stride : y*stride+w*4]
		out := dst[y*w*4 : (y+1)*w*4]
		switch format {
		case darwin.PixelFormat32BGRA:
			for x := 0; x < w; x++ {
				out[x*4+0] = in[x*4+2]
				out[x*4+1] = in[x*4+1]
				out[x*4+2] = in[x*4+0]
				out[x*4+3] = 0xff
			}
		case darwin.PixelFormat32RGBA:
			copy(out, in)
		default:
			return unsupported("a picture of format %s", fourCC(format))
		}
	}
	return nil
}

// fourCC writes a codec's four bytes as text, for saying what was not read.
func fourCC(v uint32) string {
	b := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = '.'
		}
	}
	return string(b)
}

// Sound returns the file's sound, decoded by a reader of its own, or nil
// when the file has none.
func (s *avSource) Sound() audio.Decoder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.aout == 0 {
		return nil
	}
	return &avSound{s: s}
}

// Tracks returns the file's tracks.
func (s *avSource) Tracks() []Track {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Track, len(s.tracks))
	copy(out, s.tracks)
	return out
}

// SelectTracks chooses the video and sound track by their index in
// [Source.Tracks], or -1 for none, and starts reading them from where
// playback is.
func (s *avSource) SelectTracks(video, audio int) error {
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
	var err error
	darwin.WithPool(func() { err = s.build() })
	if err != nil {
		s.video, s.audio = wasVideo, wasAudio
		darwin.WithPool(func() { err = s.build() })
		return err
	}
	return nil
}

// Duration returns the file's length, or 0 when it is not known.
func (s *avSource) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dur
}

// Seek starts reading at t from the file's start.
func (s *avSource) Seek(t time.Duration) error {
	_, err := s.seek(t)
	return err
}

// seek starts reading at t, and says where it ended up: past the end of the
// file is the end of the file.
func (s *avSource) seek(t time.Duration) (time.Duration, error) {
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
	if s.start == t && (s.vreader != 0 || s.areader != 0) {
		return t, nil
	}
	s.start = t
	var err error
	darwin.WithPool(func() { err = s.build() })
	return t, err
}

// Close gives up everything the file holds. Closing twice is fine.
func (s *avSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.unbuild()
	darwin.WithPool(func() { // the tracks live in the pool of the file's own
		for _, track := range s.handles {
			darwin.Release(track)
		}
		darwin.Release(s.asset)
	})
	s.handles = nil
	s.asset = 0
	return nil
}

// An avSound is a file's sound track as the audio package's decoder: the
// reader is asked for float samples at 48 kHz in two channels, which is
// what the audio package mixes.
type avSound struct {
	s       *avSource
	base    time.Duration // the media time the decoder's frame 0 is at
	known   bool          // whether the first buffer has been seen
	held    []float32     // frames read and not handed out yet
	scratch []byte        // the bytes those frames are in
	pad     int64         // frames of silence before the track's own start
	ended   bool          // whether the track's reader has ended
	empty   int           // buffers in a row that held no sound
}

func (d *avSound) SampleRate() int { return soundRate }

func (d *avSound) Channels() int { return soundChannels }

// Len returns the file's length in frames, or 0 when it is not known.
func (d *avSound) Len() int64 {
	dur := d.s.Duration()
	if dur <= 0 {
		return 0
	}
	return int64(dur) * soundRate / int64(time.Second)
}

func (d *avSound) Read(dst []float32) (int, error) {
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

// fill reads a sample buffer's sound into held, and works out how much
// silence comes before the track's first frame: a sound that starts later
// than the picture does is silent until it does, so that the sound's own
// frames are the media's time.
func (d *avSound) fill() error {
	sb, err := d.pull()
	if err != nil {
		return err
	}
	defer darwin.CFReleaseItem(sb)
	if darwin.SampleSamples(sb) <= 0 {
		// A buffer with no sound in it is normal at the start of a
		// track; a run of them is not.
		if d.empty++; d.empty > 8 {
			return io.EOF
		}
		return nil
	}
	if !d.known {
		d.known = true
		if pts := darwin.SamplePTS(sb); pts > d.base {
			d.pad = int64(pts-d.base) * soundRate / int64(time.Second)
		}
	}
	bb := darwin.SampleData(sb)
	length := darwin.BlockLength(bb)
	if bb == 0 || length < soundChannels*4 {
		return nil
	}
	if cap(d.scratch) < length {
		d.scratch = make([]byte, length)
	}
	d.scratch = d.scratch[:length]
	if darwin.BlockCopy(bb, length, unsafe.Pointer(&d.scratch[0])) != 0 {
		return errors.New("media: reading the sound")
	}
	frames := length / (soundChannels * 4)
	d.held = unsafe.Slice((*float32)(unsafe.Pointer(&d.scratch[0])), frames*soundChannels)
	d.empty = 0
	return nil
}

// pull reads the next sample of the sound track.
func (d *avSound) pull() (uintptr, error) {
	d.s.mu.RLock()
	defer d.s.mu.RUnlock()
	if d.s.closed || d.s.aout == 0 {
		return 0, ErrEnd
	}
	var sb uintptr
	darwin.WithPool(func() { sb = darwin.SendID(d.s.aout, darwin.Sel("copyNextSampleBuffer")) })
	if sb == 0 {
		return 0, d.s.why(d.s.areader, "sound")
	}
	return sb, nil
}

// SetPosition starts the sound at a frame of the media's timeline, which
// with a reader means reading from there: the reader is made again.
func (d *avSound) SetPosition(frame int64) error {
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

// videoSettings asks the reader for the picture format the UI draws, and
// lets AVFoundation do the colour conversion.
func videoSettings() uintptr {
	settings := darwin.MutableDict()
	darwin.DictSet(settings, darwin.NSNumberUint(darwin.PixelFormat32BGRA), darwin.PixelFormatKey())
	return settings
}

// audioSettings asks the reader for the sound format the audio package
// mixes, and lets AVFoundation do the sample rate conversion.
func audioSettings() uintptr {
	settings := darwin.MutableDict()
	set := func(key string, value uintptr) {
		darwin.DictSet(settings, value, darwin.Global(key))
	}
	set("AVFormatIDKey", darwin.NSNumberInt(darwin.FormatLinearPCM))
	set("AVSampleRateKey", darwin.NSNumberDouble(soundRate))
	set("AVNumberOfChannelsKey", darwin.NSNumberInt(soundChannels))
	set("AVLinearPCMBitDepthKey", darwin.NSNumberInt(32))
	set("AVLinearPCMIsFloatKey", darwin.NSNumberBool(true))
	set("AVLinearPCMIsBigEndianKey", darwin.NSNumberBool(false))
	// AVLinearPCMIsNonInterleaved is the one of these whose name has no
	// "Key" at the end: the header spells the other one as a macro.
	set("AVLinearPCMIsNonInterleaved", darwin.NSNumberBool(false))
	return settings
}

// firstFormat returns a track's first format description.
func firstFormat(track uintptr) uintptr {
	fds := darwin.SendID(track, darwin.Sel("formatDescriptions"))
	if darwin.ArrayCount(fds) == 0 {
		return 0
	}
	return darwin.ArrayAt(fds, 0)
}

// codecOf names a track's codec from its format description's four bytes.
func codecOf(track uintptr) Codec {
	fd := firstFormat(track)
	if fd == 0 {
		return ""
	}
	return codecFor(darwin.MediaSubType(fd))
}

// codecFor maps a codec's four bytes to a codec. The ones that are not here
// are ones this version does not name.
func codecFor(cc uint32) Codec {
	switch cc {
	case ccOf("avc1"), ccOf("avc3"):
		return CodecH264
	case ccOf("hvc1"), ccOf("hev1"):
		return CodecHEVC
	case ccOf("av01"):
		return CodecAV1
	case ccOf("vp08"):
		return CodecVP8
	case ccOf("vp09"):
		return CodecVP9
	case ccOf("jpeg"):
		return CodecMJPEG
	case ccOf("ap4h"), ccOf("ap4x"), ccOf("apcn"), ccOf("apch"), ccOf("apcs"), ccOf("apco"):
		return CodecProRes
	case ccOf("mp4a"), ccOf("aac "):
		// A file's sound is 'mp4a' in the container and 'aac ' in the
		// format description AVFoundation hands out for it.
		return CodecAAC
	case ccOf("Opus"):
		return CodecOpus
	case ccOf("fLaC"):
		return CodecFLAC
	case ccOf(".mp3"):
		return CodecMP3
	case ccOf("lpcm"), ccOf("sowt"), ccOf("twos"), ccOf("in24"), ccOf("in32"), ccOf("fl32"), ccOf("fl64"):
		return CodecPCM
	}
	return ""
}

func ccOf(s string) uint32 {
	if len(s) != 4 {
		panic("media: a codec is four bytes")
	}
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// platformCapabilities reports what this platform's own media stack reads.
// A Mac always has these codecs: the system's own, which need no plugin of
// the app's.
func platformCapabilities() Caps {
	if err := darwin.Load(); err != nil {
		return Caps{}
	}
	return Caps{
		Containers: []Container{ContainerMP4, ContainerWAV, ContainerMP3},
		Decoders:   []Codec{CodecH264, CodecHEVC, CodecAAC, CodecMP3, CodecPCM, CodecProRes},
		Encoders:   []Codec{CodecH264, CodecAAC},
		Features:   []Feature{FeaturePlayback},
	}
}
