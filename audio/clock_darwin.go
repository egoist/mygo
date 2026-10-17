package audio

// lag returns the frames the Audio Queue still holds: the buffers it has
// not called back for yet. The queue calls back for a buffer once it has
// played it, so the buffers still queued are the sound not yet heard.
func (d *aqDevice) lag() (int64, bool) {
	return int64(aqBuffers-len(d.free)) * aqFrames, true
}
