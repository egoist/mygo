package media

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// The timescales the muxer writes the tracks of a file at: 90 kHz is a
// common video clock and divides the frame rates video comes in, and 48 kHz
// is the sound's own rate, so that samples of a second divide evenly and a
// timestamp is not rounded twice.
const (
	muxVideoTimescale = 90000
	muxAudioTimescale = 48000
)

// mp4Muxer writes a fragmented MP4: an init segment with the tracks, then
// one fragment per keyframe's worth of samples.
//
// Fragmented rather than one movie at the end, because a recorder writes
// while it records: the file is playable as it grows, and a recording cut
// short is still a file with the pictures taken so far in it. Players that
// know MP4 read both.
type mp4Muxer struct {
	w    *bufio.Writer
	seq  uint32
	init *mp4.InitSegment

	tracks []*mp4Track

	// wroteInit is set once the init segment has gone out; tracks cannot
	// be added after that, since their description is in that segment.
	wroteInit bool
	closed    bool
	err       error
}

// mp4Track is one track's part of the file: its configuration, the samples
// waiting for a fragment, and the decode time the next one starts at.
type mp4Track struct {
	cfg       CodecConfig
	trackID   uint32
	timescale uint32

	// lengthSize is how many bytes an NALU's length is, as the avcC says.
	lengthSize int

	frag *mp4.Fragment

	// nextDecode is the decode time of the next sample, in ticks.
	nextDecode uint64
}

func newMP4Muxer(w io.Writer) *mp4Muxer {
	return &mp4Muxer{w: bufio.NewWriterSize(w, 1<<16), init: mp4.CreateEmptyInit()}
}

// AddTrack declares a track. Only H.264 video is written so far; anything
// else says what is missing rather than writing a track nothing decodes.
func (m *mp4Muxer) AddTrack(cfg CodecConfig) (int, error) {
	if m.closed {
		return 0, errors.New("media: adding a track to a closed muxer")
	}
	if m.wroteInit {
		return 0, errors.New("media: adding a track after the first packet")
	}
	t := &mp4Track{cfg: cfg, timescale: muxVideoTimescale}
	switch cfg.Codec {
	case CodecH264:
		sps, pps, lengthSize, err := parseAVCConfig(cfg.Extra)
		if err != nil {
			return 0, fmt.Errorf("media: the H.264 configuration record: %w", err)
		}
		t.lengthSize = lengthSize
		trak := m.init.AddEmptyTrack(t.timescale, "video", "und")
		if err := trak.SetAVCDescriptor("avc1", sps, pps, true); err != nil {
			return 0, fmt.Errorf("media: describing the H.264 track: %w", err)
		}
		t.trackID = trak.Tkhd.TrackID
	default:
		return 0, unsupported("writing %s in an MP4", cfg.Codec)
	}
	m.tracks = append(m.tracks, t)
	return len(m.tracks) - 1, nil
}

// WritePacket writes one sample. The first sample of a track must be a
// keyframe: a fragment that starts in the middle of a picture stream is a
// file a player cannot start at.
func (m *mp4Muxer) WritePacket(p EncodedPacket) error {
	if m.err != nil {
		return m.err
	}
	if m.closed {
		return errors.New("media: writing to a closed muxer")
	}
	if p.Track < 0 || p.Track >= len(m.tracks) {
		return fmt.Errorf("media: no track %d in this file", p.Track)
	}
	if len(p.Data) == 0 {
		return fmt.Errorf("media: track %d: an empty packet", p.Track)
	}
	t := m.tracks[p.Track]
	if !m.wroteInit && !p.Keyframe {
		return fmt.Errorf("media: track %d: the first picture is not a keyframe", p.Track)
	}
	if err := m.writeInit(); err != nil {
		return err
	}
	nalus, err := nalusOf(p.Data)
	if err != nil {
		return fmt.Errorf("media: track %d: %w", p.Track, err)
	}
	dur := uint64(ticksOf(p.Duration, t.timescale))
	if p.Keyframe && t.frag != nil && t.frag.Moof.Traf.Trun.SampleCount() > 0 {
		if err := m.flushTrack(t); err != nil {
			return err
		}
	}
	if t.frag == nil {
		m.seq++
		f, err := mp4.CreateFragment(m.seq, t.trackID)
		if err != nil {
			return fmt.Errorf("media: starting a fragment: %w", err)
		}
		t.frag = f
	}
	decode := p.DTS
	composition := ticksOf(p.PTS, t.timescale) - ticksOf(decode, t.timescale)
	flags := mp4.SetNonSyncSampleFlags(0)
	if p.Keyframe {
		flags = mp4.SetSyncSampleFlags(0)
	}
	size := 0
	for _, n := range nalus {
		size += t.lengthSize + len(n)
	}
	data := make([]byte, 0, size)
	for _, n := range nalus {
		var length [4]byte
		for i := 0; i < t.lengthSize; i++ {
			length[t.lengthSize-1-i] = byte(uint32(len(n)) >> (8 * i))
		}
		data = append(data, length[:t.lengthSize]...)
		data = append(data, n...)
	}
	t.frag.AddFullSample(mp4.FullSample{
		Sample:     mp4.Sample{Flags: flags, Dur: uint32(dur), Size: uint32(len(data)), CompositionTimeOffset: int32(composition)},
		DecodeTime: uint64(ticksOf(decode, t.timescale)),
		Data:       data,
	})
	t.nextDecode = uint64(ticksOf(decode, t.timescale)) + dur
	return nil
}

// writeInit writes the init segment, giving each track its track number.
func (m *mp4Muxer) writeInit() error {
	if m.wroteInit {
		return nil
	}
	if len(m.tracks) == 0 {
		return errors.New("media: a file with no tracks")
	}
	if err := m.init.Encode(m.w); err != nil {
		return m.fail(fmt.Errorf("media: writing the init segment: %w", err))
	}
	m.wroteInit = true
	return nil
}

// flushTrack writes the samples waiting for a fragment.
func (m *mp4Muxer) flushTrack(t *mp4Track) error {
	if t.frag == nil || t.frag.Moof.Traf.Trun.SampleCount() == 0 {
		t.frag = nil
		return nil
	}
	if err := t.frag.Encode(m.w); err != nil {
		return m.fail(fmt.Errorf("media: writing a fragment: %w", err))
	}
	t.frag = nil
	return nil
}

// Close flushes the last fragments and the writer.
func (m *mp4Muxer) Close() error {
	if m.closed {
		return m.err
	}
	m.closed = true
	if m.err != nil {
		return m.err
	}
	for _, t := range m.tracks {
		if err := m.flushTrack(t); err != nil {
			return err
		}
	}
	if err := m.w.Flush(); err != nil {
		return m.fail(fmt.Errorf("media: finishing the file: %w", err))
	}
	return m.err
}

// fail remembers a write error: a file being written cannot be recovered
// from one, so every later write returns it rather than pretend.
func (m *mp4Muxer) fail(err error) error {
	if m.err == nil {
		m.err = err
	}
	return m.err
}

// parseAVCConfig reads an avcC configuration record: the SPS and PPS a
// decoder needs, and how long the sample's NALU lengths are.
func parseAVCConfig(extra []byte) (sps, pps [][]byte, lengthSize int, err error) {
	if len(extra) < 7 {
		return nil, nil, 0, errors.New("it is too short")
	}
	if extra[0] != 1 {
		return nil, nil, 0, fmt.Errorf("version %d", extra[0])
	}
	lengthSize = int(extra[4]&3) + 1
	// The SPS count is the byte after the length size; the sets follow it.
	at := 6
	read := func() ([]byte, error) {
		if at+2 > len(extra) {
			return nil, errors.New("it ends before its parameter sets")
		}
		n := int(extra[at])<<8 | int(extra[at+1])
		at += 2
		if at+n > len(extra) {
			return nil, errors.New("a parameter set runs past the end")
		}
		nalu := extra[at : at+n]
		at += n
		return nalu, nil
	}
	nSPS := int(extra[5] & 0x1f)
	for i := 0; i < nSPS; i++ {
		v, err := read()
		if err != nil {
			return nil, nil, 0, err
		}
		sps = append(sps, v)
	}
	if at >= len(extra) {
		return nil, nil, 0, errors.New("it holds no PPS count")
	}
	nPPS := int(extra[at])
	at++
	for i := 0; i < nPPS; i++ {
		v, err := read()
		if err != nil {
			return nil, nil, 0, err
		}
		pps = append(pps, v)
	}
	return sps, pps, lengthSize, nil
}

// nalusOf splits one access unit's start codes into its NALUs, which is the
// form that travels between an encoder and a muxer.
func nalusOf(data []byte) ([][]byte, error) {
	var nalus [][]byte
	at := -1
	for i := 0; i+2 < len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 {
			continue
		}
		start := 0
		switch {
		case data[i+2] == 1:
			start = i + 3
		case data[i+2] == 0 && i+3 < len(data) && data[i+3] == 1:
			start = i + 4
		default:
			continue
		}
		if at >= 0 {
			n := i - at
			for n > 0 && data[at+n-1] == 0 {
				n--
			}
			if n > 0 {
				nalus = append(nalus, data[at:at+n])
			}
		}
		at = start
		i = start - 1
	}
	if at < 0 {
		return nil, errors.New("a sample without start codes")
	}
	if at < len(data) {
		nalus = append(nalus, data[at:])
	}
	if len(nalus) == 0 {
		return nil, errors.New("a sample with no NALU in it")
	}
	return nalus, nil
}

// ticksOf turns a duration into a track's own ticks.
func ticksOf(d time.Duration, timescale uint32) int64 {
	// Split whole seconds from the remainder before multiplying: nanoseconds
	// times a 90 kHz scale otherwise overflow after roughly 28 hours.
	return int64(d/time.Second)*int64(timescale) + int64(d%time.Second)*int64(timescale)/int64(time.Second)
}
