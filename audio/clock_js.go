package audio

// lag reports nothing: the worklet that plays in the browser keeps its own
// position and does not send it back, so the clock there is the mixer's
// read position, up to the three queued chunks ahead of what is heard.
func (jsDevice) lag() (int64, bool) { return 0, false }
