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
	soundOn  bool    // and whether the sound is

	// The readers read asynchronously: a read is a request the platform
	// answers on a callback, which is handed to a pump and waited for
	// there. A seek or a close can then take the request back instead of
	// waiting for a picture that is not coming.
	vr    windows.SourceReader    // the reader of the chosen picture stream
	vcb   *windows.AsyncCallback  // and the callback it reports reads to
	vpump *readerPump[readResult] // and where those reports wait to be taken
	ar    windows.SourceReader    // the reader of the chosen sound stream
	acb   *windows.AsyncCallback
	apump *readerPump[readResult]
	vw    int // the picture's width, as the platform
	vh    int // negotiated it

	sound *mfSound // the sound as the audio package's decoder

	start time.Duration // where the readers start
	dur   time.Duration // how long the file is
	pool  pixPool       // buffers for the pictures handed out
}

// A readResult is one read an asynchronous reader completed: the platform's
// status for it, the flags of the read, and the sample, whose reference the
// pump holds and releases when nobody takes it.
type readResult struct {
	hr     uint32
	flags  uint32
	sample windows.Sample
}

// openAsyncReader makes a reader of the opened file that reports its reads
// instead of waiting for them: the pump is where the pull waits for one, and
// the callback hands each over as the platform completes it.
func (s *mfSource) openAsyncReader(video bool) (windows.SourceReader, *windows.AsyncCallback, *readerPump[readResult], error) {
	pump := newReaderPump(func(r readResult) { r.sample.Release() })
	cb := windows.NewAsyncCallback(func(r windows.ReadResult) {
		pump.deliver(readResult{hr: r.HR, flags: r.Flags, sample: r.Sample})
	})
	var (
		r   windows.SourceReader
		err error
	)
	if s.path {
		r, err = windows.OpenReaderFromPathAsync(s.target, video, cb)
	} else {
		r, err = windows.OpenReaderFromURLAsync(s.target, video, cb)
	}
	if err != nil {
		return windows.SourceReader{}, nil, nil, readerError(err, FeaturePlayback, "")
	}
	return r, cb, pump, nil
}

// flushReader takes back the read a reader may have in flight and waits for
// the platform to confirm it, so that the reader can be moved or let go: a
// reader let go with a read pending would call back into a source that is
// gone. A reader with no read pending needs no flush, and waiting for a
// confirmation that has nothing to confirm would wait on the platform's word
// alone.
//
// The pump is reset before the flush, so the read being taken back is dropped
// and released if the platform still reports it while the flush completes. It
// is not reported after that: the platform calls OnFlush when the flush is
// done, which is when the request is over. And a new read cannot join it,
// because the caller holds the source's write lock and a read is only started
// under its read lock, so no result can be taken for a later request.
//
// A flush the platform cannot start leaves the request in the pump's past but
// possibly still to be reported: the reader is not moved then but made again,
// which lets the old reader go and with it whatever it would have called back
// with. The error is only the platform's word that the flush did not start; a
// caller that lets the reader go ignores it, since it has nothing better to
// try and the pump has already dropped what the read would have been for.
func flushReader(r windows.SourceReader, cb *windows.AsyncCallback, pump *readerPump[readResult], stream int) error {
	if !r.Valid() {
		return nil
	}
	pending := pump != nil && pump.hasRead()
	if pump != nil {
		pump.reset()
	}
	if !pending || cb == nil {
		return nil
	}
	// FlushAsync forgets a confirmation left by an earlier flush, so that
	// the wait below is for this flush's own.
	return r.FlushAsync(context.Background(), stream)
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
	s := &mfSource{target: target, path: !url, video: -1, audio: -1, pictures: o.pictures(), soundOn: o.sound()}
	if err := s.listTracks(); err != nil {
		return nil, err
	}
	if (s.video < 0 || !s.pictures) && (s.audio < 0 || !s.soundOn) {
		return nil, unsupported("%q has no picture or sound", target)
	}
	if err := s.build(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// openReader makes a reader over what was opened that reads synchronously,
// which is what reading a file's tracks needs: it asks one question of every
// stream and has the answer to go on with. Playing reads asynchronously.
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
		r, cb, pump, w, h, err := s.openPicture(s.video)
		if err != nil {
			return err
		}
		s.vr, s.vcb, s.vpump, s.vw, s.vh = r, cb, pump, w, h
	}
	if s.audio >= 0 && s.soundOn {
		r, cb, pump, f, err := s.openSound(s.audio)
		if err != nil {
			s.unbuild()
			return err
		}
		s.ar, s.acb, s.apump = r, cb, pump
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
func (s *mfSource) openPicture(stream int) (windows.SourceReader, *windows.AsyncCallback, *readerPump[readResult], int, int, error) {
	r, cb, pump, err := s.openAsyncReader(true)
	if err != nil {
		return windows.SourceReader{}, nil, nil, 0, 0, err
	}
	keep := false
	defer func() {
		if !keep {
			r.Shutdown()
			r.Release()
		}
	}()
	if err := selectStream(r, stream); err != nil {
		return windows.SourceReader{}, nil, nil, 0, 0, err
	}
	codec, err := nativeCodec(r, stream)
	if err != nil {
		return windows.SourceReader{}, nil, nil, 0, 0, err
	}
	mt, err := windows.NewMediaType()
	if err != nil {
		return windows.SourceReader{}, nil, nil, 0, 0, err
	}
	ok := mt.SetGUID(&windows.KeyMajorType, &windows.MajorVideo) == nil &&
		mt.SetGUID(&windows.KeySubtype, &windows.FormatRGB32) == nil
	if !ok {
		mt.Release()
		return windows.SourceReader{}, nil, nil, 0, 0, errors.New("media: asking for a picture format")
	}
	err = r.SetCurrentMediaType(stream, mt)
	mt.Release()
	if err != nil {
		return windows.SourceReader{}, nil, nil, 0, 0, readerError(err, FeaturePlayback, codec)
	}
	current, err := r.CurrentMediaType(stream)
	if err != nil {
		return windows.SourceReader{}, nil, nil, 0, 0, readerError(err, FeaturePlayback, codec)
	}
	defer current.Release()
	if sub, err := current.Subtype(); err == nil && sub != windows.FormatRGB32 && sub != windows.FormatARGB32 {
		return windows.SourceReader{}, nil, nil, 0, 0, unsupported("a picture of format %s", sub)
	}
	w, h, has := current.FrameSize()
	if !has || w <= 0 || h <= 0 {
		return windows.SourceReader{}, nil, nil, 0, 0, unsupported("a picture of %dx%d", w, h)
	}
	keep = true
	return r, cb, pump, w, h, nil
}

// openSound makes the reader of one sound stream, asked for the float samples
// the audio package mixes: the platform's own converter does the sample rate
// and channel conversion where it can, and where it cannot the sound is read
// as it is and converted here.
func (s *mfSource) openSound(stream int) (windows.SourceReader, *windows.AsyncCallback, *readerPump[readResult], soundFormat, error) {
	r, cb, pump, err := s.openAsyncReader(false)
	if err != nil {
		return windows.SourceReader{}, nil, nil, soundFormat{}, err
	}
	keep := false
	defer func() {
		if !keep {
			r.Shutdown()
			r.Release()
		}
	}()
	if err := selectStream(r, stream); err != nil {
		return windows.SourceReader{}, nil, nil, soundFormat{}, err
	}
	codec, rate, channels, err := nativeSound(r, stream)
	if err != nil {
		return windows.SourceReader{}, nil, nil, soundFormat{}, err
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
			return windows.SourceReader{}, nil, nil, soundFormat{}, err
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
		return windows.SourceReader{}, nil, nil, soundFormat{}, readerError(last, FeaturePlayback, codec)
	}
	current, err := r.CurrentMediaType(stream)
	if err != nil {
		return windows.SourceReader{}, nil, nil, soundFormat{}, readerError(err, FeaturePlayback, codec)
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
		return windows.SourceReader{}, nil, nil, soundFormat{}, unsupported("sound of %d bits", f.bits)
	}
	keep = true
	return r, cb, pump, f, nil
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
// platform refuses replaces. A reader is only let go once the read it may have
// in flight is taken back and the platform has confirmed it, so that nothing
// calls back into the source after it is gone.
func (s *mfSource) unbuild() {
	if s.vr.Valid() {
		// A flush that does not start is no reason to keep the reader:
		// letting it go is all that is left, and the pump has dropped
		// what the read would have been for.
		flushReader(s.vr, s.vcb, s.vpump, s.video)
		s.vr.Shutdown()
		s.vr.Release()
		s.vr, s.vcb, s.vpump, s.vw, s.vh = windows.SourceReader{}, nil, nil, 0, 0
	}
	if s.ar.Valid() {
		flushReader(s.ar, s.acb, s.apump, s.audio)
		s.ar.Shutdown()
		s.ar.Release()
		s.ar, s.acb, s.apump = windows.SourceReader{}, nil, nil
	}
}

// readNeed is what a read the platform reported asks of the source: a sample
// to show, another read, the end, or a failure to say.
type readNeed int

const (
	// readTakes is a read with a sample: it is the next picture.
	readTakes readNeed = iota
	// readAgain is a read with nothing: a live source has moments
	// without a picture.
	readAgain
	// readEnds is the end of the stream.
	readEnds
	// readFails is a read that failed, or one that says the stream did.
	readFails
)

// readNeedOf says what a reported read asks of the source, and what a run of
// reads with nothing in them means: empty is how many such reads it has been.
// A stream with nothing at this time is read again, up to nine times, and
// then it is the end: a live source has gaps between its pictures, a file does
// not.
func readNeedOf(hr, flags uint32, hasSample bool, empty int) readNeed {
	switch {
	case int32(hr) < 0:
		return readFails
	case flags&windows.FlagEndOfStream != 0:
		return readEnds
	case hasSample:
		return readTakes
	case flags&windows.FlagError != 0:
		return readFails
	case empty >= 8:
		return readEnds
	}
	return readAgain
}

// NextFrame returns the next picture of the file, and hands its ownership to
// the caller, which releases it. The end of the file is [ErrEnd].
//
// The read is a request the platform answers on a callback, and the waiting
// for it is the pump's, not a lock's: a seek or a close can take the request
// back while this waits, and the wait says to start again from wherever the
// file is then. Only the choosing of the reader and the starting of the read
// hold the lock, and only for as long as that takes: everything the reported
// read is said against is taken from under the lock with it, so that a change
// of tracks while it is waited for cannot be read into this one's picture.
func (s *mfSource) NextFrame(ctx context.Context) (*Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	windows.Enter()
	for empty := 0; ; {
		s.mu.RLock()
		if s.closed || !s.vr.Valid() || s.vpump == nil {
			s.mu.RUnlock()
			return nil, ErrEnd
		}
		vr, stream, pump, w, h := s.vr, s.video, s.vpump, s.vw, s.vh
		codec := s.videoCodec()
		if err := pump.read(func() error { return vr.ReadSampleAsync(stream) }); err != nil {
			s.mu.RUnlock()
			if errors.Is(err, errPumpClosed) {
				return nil, ErrEnd
			}
			return nil, readerError(err, FeaturePlayback, codec)
		}
		s.mu.RUnlock()
		res, err := pump.wait(ctx)
		switch {
		case errors.Is(err, errPumpRestarted):
			// The reader was flushed and moved under the read: it no
			// longer belongs here, and the next read does.
			continue
		case errors.Is(err, errPumpClosed):
			return nil, ErrEnd
		case err != nil:
			return nil, err
		}
		switch readNeedOf(res.hr, res.flags, !res.sample.Empty(), empty) {
		case readTakes:
			f, err := pictureOf(res.sample, &s.pool, w, h, 0, FeaturePlayback)
			res.sample.Release()
			return f, err
		case readAgain:
			res.sample.Release()
			empty++
			continue
		case readEnds:
			res.sample.Release()
			return nil, ErrEnd
		}
		// The read failed, or the stream said it did while bringing no
		// sample to show.
		res.sample.Release()
		if int32(res.hr) < 0 {
			return nil, readerError(&windows.Error{What: "IMFSourceReader::ReadSample", HR: res.hr}, FeaturePlayback, codec)
		}
		return nil, errors.New("media: reading the picture")
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
		// A 2D buffer is locked as the picture's first displayed row
		// and the distance to the row below it, whose sign says which
		// way that is: the rows are not one stretch to slice from a
		// start, since the start is the top row, not the bottom of the
		// memory.
		defer twoD.Release()
		first, pitch, err := twoD.Lock2D()
		if err != nil {
			return readerError(err, feature, "")
		}
		defer twoD.Unlock2D()
		return copyRGB32Rows(dst, first, w, h, int(pitch))
	}
	// A buffer with no rows of its own is one flat stretch of pixels,
	// stored as the stride the caller knows it by says.
	base, length, err := buf.Lock()
	if err != nil {
		return readerError(err, feature, "")
	}
	defer buf.Unlock()
	return copyRGB32Flat(dst, base, length, w, h, stride)
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
	// A reader with a read pending refuses to move, and a read that waited
	// for a picture that the move makes wrong is not worth keeping: take
	// back what the readers were asked for, then move them. A reader whose
	// read could not be taken back may still report it, so its report could
	// be taken for one of the new position's: it is not moved then but made
	// again, which lets the old reader go and with it whatever it would
	// have called back with.
	if err := flushReader(s.vr, s.vcb, s.vpump, s.video); err == nil {
		if err := flushReader(s.ar, s.acb, s.apump, s.audio); err == nil {
			if err := s.move(t); err == nil {
				return t, nil
			}
		}
	}
	// A reader the platform could not take a read back from, or still
	// refuses to move, is made again, which reads the file from the top
	// and lands on the key frame before t.
	if err := s.build(); err != nil {
		return t, err
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
	// Giving up the readers takes back the reads in flight, so the
	// goroutine waiting on one hears the end rather than the platform.
	pumps := [...]*readerPump[readResult]{s.vpump, s.apump}
	s.unbuild()
	// The pumps are closed after their readers are gone: a caller still
	// waiting on one, or one that comes back to it, hears that the source
	// is closed instead of waiting for a read that would be started on a
	// reader that no longer exists. A result still held there is released,
	// and so is one the platform reports after this.
	for _, pump := range pumps {
		if pump != nil {
			pump.close()
		}
	}
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

// pull reads the next sample of the sound track, the same way NextFrame reads
// a picture: as a request the platform reports, waited for without the lock, so
// that a seek or a close can take it back. The sound has no context of its own
// to be canceled by: a close says so with the pump.
func (d *mfSound) pull() (windows.Sample, error) {
	windows.Enter()
	for {
		d.s.mu.RLock()
		if d.s.closed || !d.s.ar.Valid() || d.s.apump == nil {
			d.s.mu.RUnlock()
			return windows.Sample{}, ErrEnd
		}
		ar, stream, pump := d.s.ar, d.s.audio, d.s.apump
		if err := pump.read(func() error { return ar.ReadSampleAsync(stream) }); err != nil {
			d.s.mu.RUnlock()
			if errors.Is(err, errPumpClosed) {
				return windows.Sample{}, ErrEnd
			}
			return windows.Sample{}, readerError(err, FeaturePlayback, "")
		}
		d.s.mu.RUnlock()
		res, err := pump.wait(context.Background())
		switch {
		case errors.Is(err, errPumpRestarted):
			continue
		case errors.Is(err, errPumpClosed):
			return windows.Sample{}, ErrEnd
		case err != nil:
			return windows.Sample{}, err
		}
		if int32(res.hr) < 0 {
			res.sample.Release()
			return windows.Sample{}, readerError(&windows.Error{What: "IMFSourceReader::ReadSample", HR: res.hr}, FeaturePlayback, "")
		}
		if res.flags&windows.FlagEndOfStream != 0 {
			res.sample.Release()
			return windows.Sample{}, ErrEnd
		}
		if res.sample.Empty() {
			return windows.Sample{}, ErrEnd
		}
		return res.sample, nil
	}
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
