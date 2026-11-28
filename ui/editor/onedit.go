package editor

import "slices"

// editListener is a function OnEdit tells of edits, and the text it was
// last told of.
type editListener struct {
	fn   func(old, new Snapshot, changes []Change)
	text Snapshot
}

// OnEdit has fn told of each version of the text after the current one,
// in order, as a Highlighter's Edit is: the text before and after it,
// and the ranges it replaced. Whatever made it tells it: an editor,
// Edit, EditJoined, Set, Reset, Undo, Redo and EditRemote. It returns a
// function that stops it.
//
// fn is told after the edit, with the buffer unlocked, one call at a
// time. The goroutine that made the edit tells it, an editor's once the
// command or the typing that made it is done, before the edit's method
// returns. When another goroutine is telling the buffer's functions
// already, that goroutine tells it instead, as soon as it is made: before
// or after the edit's method returns, and between two edits of an
// editor's command. fn may read and edit the buffer, and hears of its own
// edits once it returns. A function so far behind that the buffer no
// longer has the versions in between, as after hundreds of its own edits,
// is told one change of the whole text. Once off returns, fn
// is not called again, but for a call another goroutine had already
// begun, which may still finish: off does not wait for it, so that fn
// can call off.
func (b *Buffer) OnEdit(fn func(old, new Snapshot, changes []Change)) (off func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := &editListener{fn: fn, text: b.snapshotLocked()}
	b.listeners = append(b.listeners, l)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.listeners = slices.DeleteFunc(b.listeners, func(x *editListener) bool { return x == l })
	}
}

// tell tells OnEdit's functions of the versions since they were last
// told; b.mu is not held. One goroutine tells at a time: an edit made
// while it does is left to it.
func (b *Buffer) tell() {
	b.mu.Lock()
	if b.telling || len(b.listeners) == 0 {
		b.mu.Unlock()
		return
	}
	b.telling = true
	// telling is cleared under the lock of the last look for versions to
	// tell, so that an edit made after it tells them itself; the deferred
	// function clears it when a listener panics.
	done := false
	defer func() {
		if !done {
			b.mu.Lock()
			b.telling = false
			b.mu.Unlock()
		}
	}()
	for {
		i := slices.IndexFunc(b.listeners, func(l *editListener) bool { return l.text.version != b.version })
		if i < 0 {
			b.telling, done = false, true
			b.mu.Unlock()
			return
		}
		l := b.listeners[i]
		old, next := l.text, b.snapshotLocked()
		// The versions are in a row in the log: the one after old's is at
		// its distance from the first.
		changes := []Change{{OldStart: 0, OldEnd: old.Len(), NewStart: 0, NewEnd: next.Len()}}
		if len(b.log) > 0 && old.version+1 >= b.log[0].version {
			e := b.log[old.version+1-b.log[0].version]
			next = Snapshot{t: e.text, version: e.version, ends: e.ends, restarts: e.restarts}
			changes = publicChanges(e.changes)
		}
		l.text = next
		b.mu.Unlock()
		l.fn(old, next, changes)
		b.mu.Lock()
	}
}
