package ui

import (
	"net/url"
	"path"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Router keeps the history of the pages of a window, or of a part of one,
// as a browser does for a tab: the page shown, those before it that Back
// returns to, and those after it that Forward goes to again. A page is a
// path, as "/notes/42?tab=info", and View builds the one shown:
//
//	app.router = ui.NewRouter("/notes")
//	…
//	app.router.View(c, func(r *ui.Route) {
//		switch {
//		case r.Match("/notes"):
//			r.Title("Notes")
//			app.notes(c)
//		case r.Match("/notes/{id}"):
//			r.Title("Note")
//			app.note(c, r.Param("id"))
//		default:
//			ui.Text(c, "Not found")
//		}
//	})
//
// Push goes to a page and Back comes back, as do Cmd+[ and Cmd+] on macOS,
// Alt+Left and Alt+Right elsewhere, the back and forward buttons of a
// mouse, BackButton and ForwardButton, and Links to paths in its pages.
//
// A page keeps the state of its elements while it is in the history:
// going back to one finds it scrolled where it was, with what was typed,
// and the keyboard focus where it was. Going to a page moves the focus
// into it, unless the focus is outside the router, as in a sidebar
// choosing the pages, where it stays: screen readers then hear the
// title of the page. Pages slide or fade in (Transition).
//
// The zero Router shows "/". As the rest of the app's state, it is read
// and changed on the main thread: from another goroutine, change it in
// Window.Update.
type Router struct {
	// Transition is how pages replace each other.
	Transition Transition

	entries []*routeEntry
	at      int
	pages   uint64
	// rt is the window showing the router, which a change asks for a
	// frame.
	rt *engine
	// shown is the entry View showed last, leaving the one going away
	// since start, toward deeper pages (dir 1), back up (-1) or across.
	shown, leaving *routeEntry
	dir            int
	start          time.Time
	// focusing moves the focus into the page shown, and announcing tells
	// screen readers its title.
	focusing, announcing bool
}

// routeEntry is an entry of the history: a location of a page. Entries
// pushed for the query of the page shown share its page.
type routeEntry struct {
	loc   string // the path, escaped, and the query
	page  uint64
	title string
	// focus is the element of the page that had the keyboard focus as the
	// entry went out of sight.
	focus uint64
}

// Transition is how a Router's pages replace each other.
type Transition uint8

const (
	// TransitionAuto slides a page in from the right going deeper, as from
	// "/notes" to "/notes/42", and from the left back up; pages across, as
	// from "/notes" to "/settings", fade in. Where the desktop asks for less
	// motion, they all fade.
	TransitionAuto Transition = iota
	// TransitionFade fades pages in.
	TransitionFade
	// TransitionNone shows pages at once.
	TransitionNone
)

const (
	// maxHistory is how many entries a history holds, and keptPages how
	// many pages of it besides the one shown keep their state.
	maxHistory = 100
	keptPages  = 10
	slideTime  = 250 * time.Millisecond
	fadeTime   = 150 * time.Millisecond
)

// NewRouter returns a router showing the page at path.
func NewRouter(path string) *Router {
	r := &Router{}
	r.Replace(path)
	return r
}

// current returns the entry shown, "/" for the zero Router.
func (r *Router) current() *routeEntry {
	if len(r.entries) == 0 {
		r.pages++
		r.entries, r.at = []*routeEntry{{loc: "/", page: r.pages}}, 0
	}
	return r.entries[r.at]
}

// resolve returns the location of target, relative to the page shown as
// a link in a web page is: its path made absolute and clean, and its
// query.
func (r *Router) resolve(target string) (loc, p string, ok bool) {
	ref, err := url.Parse(target)
	if err != nil {
		return "", "", false
	}
	base := &url.URL{Path: "/"}
	if len(r.entries) > 0 {
		base, _ = url.Parse(r.entries[r.at].loc)
	}
	u := base.ResolveReference(ref)
	p = path.Clean("/" + u.Path)
	return (&url.URL{Path: p, RawQuery: u.RawQuery}).String(), p, true
}

// Push goes to the page at target, after the page shown, whose later
// pages it forgets: an absolute path, as "/notes/42", or one relative to
// the page shown, as a link in a web page, as "?tab=info" or "../". Going
// to the page shown does nothing. The same path with another query is
// the same page, keeping its state, in another entry of the history.
func (r *Router) Push(target string) {
	cur := r.current()
	loc, p, ok := r.resolve(target)
	if !ok || loc == cur.loc {
		return
	}
	e := &routeEntry{loc: loc, page: cur.page}
	if p != pathOf(cur.loc) {
		r.pages++
		e.page = r.pages
	}
	r.entries = append(r.entries[:r.at+1], e)
	if len(r.entries) > maxHistory {
		r.entries = r.entries[len(r.entries)-maxHistory:]
	}
	r.at = len(r.entries) - 1
	r.changed()
}

// Replace shows the page at target in place of the page shown, in its
// entry of the history, as after a page moved: Back does not return to
// the page replaced. The same path with another query is the same page,
// keeping its state, as a search page following what is typed.
func (r *Router) Replace(target string) {
	loc, p, ok := r.resolve(target)
	if !ok || len(r.entries) > 0 && r.entries[r.at].loc == loc {
		return
	}
	e := &routeEntry{loc: loc}
	if len(r.entries) > 0 && p == pathOf(r.entries[r.at].loc) {
		e.page, e.title = r.entries[r.at].page, r.entries[r.at].title
	} else {
		r.pages++
		e.page = r.pages
	}
	if len(r.entries) == 0 {
		r.entries = []*routeEntry{e}
	} else {
		r.entries[r.at] = e
	}
	r.changed()
}

// Back shows the page before, if any.
func (r *Router) Back() { r.Go(-1) }

// Forward shows the page after, which Back left, if any.
func (r *Router) Forward() { r.Go(1) }

// Go moves n pages through the history: back for a negative n, forward
// for a positive one, as far as it goes.
func (r *Router) Go(n int) {
	r.current()
	at := max(0, min(r.at+n, len(r.entries)-1))
	if at != r.at {
		r.at = at
		r.changed()
	}
}

// CanGoBack reports whether there is a page to go back to, and
// CanGoForward one to go forward to.
func (r *Router) CanGoBack() bool    { return r.at > 0 }
func (r *Router) CanGoForward() bool { return r.at < len(r.entries)-1 }

// Path returns the path of the page shown, as "/notes/42", without its
// query.
func (r *Router) Path() string { return pathOf(r.current().loc) }

// Location returns the path and the query of the page shown, as
// "/notes/42?tab=info", which Push and Replace take, to show the page
// again when the app starts.
func (r *Router) Location() string { return r.current().loc }

// Query returns the value of the query parameter name of the page shown,
// "" if it has none.
func (r *Router) Query(name string) string { return queryOf(r.current().loc, name) }

// Title returns the title of the page shown, as its Route set it in the
// last frame.
func (r *Router) Title() string { return r.current().title }

// changed asks for a frame showing the page now current: another pass of
// the frame being built, or a frame.
func (r *Router) changed() {
	if rt := r.rt; rt != nil {
		if rt.inFrame {
			rt.consumed = true
		} else {
			rt.host.requestFrame()
		}
	}
}

// isPath reports whether a link's target is a path within the app, as
// "/notes/42", "edit" or "?tab=info", rather than a URL with a scheme.
func isPath(target string) bool {
	u, err := url.Parse(target)
	return err == nil && u.Scheme == "" && u.Host == "" && u.Opaque == ""
}

// pathOf returns the path of a location, unescaped.
func pathOf(loc string) string {
	if u, err := url.Parse(loc); err == nil {
		return u.Path
	}
	return loc
}

// queryOf returns the value of a query parameter of a location.
func queryOf(loc, name string) string {
	if u, err := url.Parse(loc); err == nil {
		return u.Query().Get(name)
	}
	return ""
}

// Route is a page a Router's View builds: Match tells its path apart and
// Param reads the parts of the path the pattern matched.
type Route struct {
	entry *routeEntry
	page  *Element
	// segments are the parts of the path, unescaped, and params those
	// the last pattern matched took.
	segments []string
	params   [][2]string
}

// Path returns the path of the page, as "/notes/42".
func (r *Route) Path() string { return "/" + strings.Join(r.segments, "/") }

// Match reports whether the page's path matches pattern, whose parts
// between slashes are either the same as the path's or wildcards: {name}
// takes any one part, and {name...}, at the end, the rest of the path,
// which may be empty. Param returns what they took. A slash at the end of
// the path or the pattern makes no difference.
//
//	case r.Match("/notes/{id}"):        // "/notes/42"
//	case r.Match("/files/{path...}"):   // "/files", "/files/docs/a.txt"
func (r *Route) Match(pattern string) bool {
	parts := pathParts(pattern)
	var params [][2]string
	for i, p := range parts {
		if len(p) < 2 || p[0] != '{' || p[len(p)-1] != '}' {
			if i >= len(r.segments) || r.segments[i] != p {
				return false
			}
			continue
		}
		name := p[1 : len(p)-1]
		if rest, ok := strings.CutSuffix(name, "..."); ok {
			if i != len(parts)-1 {
				panic("ui: {" + name + "} does not end the pattern " + pattern)
			}
			if i < len(r.segments) {
				params = append(params, [2]string{rest, strings.Join(r.segments[i:], "/")})
			} else {
				params = append(params, [2]string{rest, ""})
			}
			r.params = params
			return true
		}
		if i >= len(r.segments) {
			return false
		}
		params = append(params, [2]string{name, r.segments[i]})
	}
	if len(parts) != len(r.segments) {
		return false
	}
	r.params = params
	return true
}

// pathParts returns the parts of a path between slashes.
func pathParts(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// Param returns what the wildcard name of the pattern Match last matched
// took, unescaped, "" if it has none.
func (r *Route) Param(name string) string {
	for _, p := range r.params {
		if p[0] == name {
			return p[1]
		}
	}
	return ""
}

// Query returns the value of the query parameter name, "" if the page has
// none.
func (r *Route) Query(name string) string { return queryOf(r.entry.loc, name) }

// Title names the page: screen readers read it as the page shows, and
// BackButton and ForwardButton list it.
func (r *Route) Title(title string) { r.entry.title = title }

// Page returns the element holding the page, a column filling the router,
// for its style.
func (r *Route) Page() *Element { return r.page }

// View builds the page shown with fn, in a column taking the room it is
// given, and returns the column. As a page slides in, it builds the one
// going away too, which takes neither the pointer nor the keyboard.
func (r *Router) View(c *Context, fn func(r *Route)) *Element {
	rt := c.rt
	r.rt = rt
	box := Column(c).Grow(1).AlignSelf(Stretch).MinWidth(0).MinHeight(0)
	box.widget = "Router"
	r.keys(c, box)
	cur := r.current()
	switch {
	case r.shown == nil:
		r.shown = cur // the first page: nothing changes
	case r.shown.page != cur.page:
		r.show(c, box, cur)
	default:
		r.shown = cur
	}
	progress := float32(1)
	if r.leaving != nil {
		d := slideTime
		if r.dir == 0 {
			d = fadeTime
		}
		if t := c.now.Sub(r.start); t < d {
			progress = EaseOut(float32(t) / float32(d))
			c.AnimationFrame()
		} else {
			r.leaving = nil
		}
	}
	box.Children(func() {
		var page *Element
		switch {
		case r.leaving == nil:
			page = r.page(c, box, cur, fn, false)
		case r.dir == 0:
			// Across, the page fades in in place of the other, which goes
			// at once: the two would blur into each other.
			page = r.page(c, box, cur, fn, false).Opacity(progress)
		default:
			// The page going away and the one coming: the one in front in
			// the history slides over the other, which moves less, dimmed.
			box.Clip()
			bg := backdrop(box)
			old := r.page(c, box, r.leaving, fn, true)
			page = r.page(c, box, cur, fn, false)
			upper, lower, shown := old, page, 1-progress
			if r.dir > 0 {
				upper, lower, shown = page, old, progress
			}
			upper.Absolute().Top(0).Fill().Background(bg).LeftPercent(100*(1-shown)).Shadow(0, 0, 16, 0, RGBA(0, 0, 0, 0.15))
			lower.LeftPercent(-30 * shown).Background(bg).DrawOver(func(p *Painter, rc Rect) {
				p.Fill(rc, RGBA(0, 0, 0, 0.06*shown), 0)
			})
		}
		r.keep(c, box, cur)
		if r.focusing {
			r.focusing = false
			r.focusPage(c, page, cur)
		}
	})
	if r.announcing {
		r.announcing = false
		c.Announce(cur.title)
	}
	return box
}

// keys goes back and forward for the keys that do, with the keyboard
// focus in the router, or anywhere for the first router of the window.
func (r *Router) keys(c *Context, box *Element) {
	first := c.routers == 0
	c.routers++
	back, forward := [2]shortcut{{0, KeyBack}, {Alt, KeyLeft}}, [2]shortcut{{0, KeyForward}, {Alt, KeyRight}}
	if runtime.GOOS == "darwin" {
		back[1], forward[1] = shortcut{Super, KeyBracketLeft}, shortcut{Super, KeyBracketRight}
	}
	pressed := func(keys [2]shortcut) bool {
		hit := false
		for _, k := range keys {
			if box.Shortcut(k.mods, k.key) || first && c.Shortcut(k.mods, k.key) {
				hit = true
			}
		}
		return hit
	}
	if pressed(back) {
		r.Back()
	}
	if pressed(forward) {
		r.Forward()
	}
}

// show starts showing entry cur in place of the entry shown: the
// transition, and where the keyboard focus goes.
func (r *Router) show(c *Context, box *Element, cur *routeEntry) {
	rt := c.rt
	old := r.shown
	r.shown = cur
	oldPage := keyedID(box.id, old.page)
	if rt.within(rt.focused, oldPage) {
		old.focus = rt.focused
	}
	// The focus in the page going away, or in a popup that may have
	// chosen the page, goes into the new page; outside, as in a sidebar
	// choosing the pages, it stays.
	if rt.states[rt.focused] == nil || rt.within(rt.focused, box.id) || rt.within(rt.focused, overlayID) {
		r.focusing, r.announcing = true, false
		rt.focused = 0
	} else {
		r.focusing, r.announcing = false, true
	}
	r.leaving, r.dir, r.start = nil, 0, c.now
	if r.Transition == TransitionNone {
		return
	}
	r.leaving = old
	if r.Transition == TransitionAuto && !rt.preferences().ReduceMotion {
		from, to := pathParts(pathOf(old.loc)), pathParts(pathOf(cur.loc))
		switch {
		case len(to) > len(from) && slices.Equal(to[:len(from)], from):
			r.dir = 1
		case len(from) > len(to) && slices.Equal(from[:len(to)], to):
			r.dir = -1
		}
	}
}

// page builds the page of entry e with fn: inert while it goes away.
func (r *Router) page(c *Context, box *Element, e *routeEntry, fn func(*Route), leaving bool) *Element {
	pg := Column(c).Key(e.page).Grow(1).MinWidth(0).MinHeight(0)
	pg.flags |= flagPage
	if leaving {
		pg.flags |= flagInert
	} else {
		// The focus goes to it, but Tab goes into it, past it.
		pg.flags |= flagFocusable | flagFocusTarget | flagOwnRing
		pg.role = RoleGroup
	}
	route := &Route{entry: e, page: pg}
	for _, s := range pathParts(pathOf(e.loc)) {
		if u, err := url.PathUnescape(s); err == nil {
			s = u
		}
		route.segments = append(route.segments, s)
	}
	saved, inert := c.router, c.inert
	if leaving {
		c.inert = true
	} else {
		c.router = r
	}
	pg.Children(func() { fn(route) })
	c.router, c.inert = saved, inert
	pg.Label(e.title)
	return pg
}

// keep keeps the state of the pages of the history nearest the page
// shown, but those built.
func (r *Router) keep(c *Context, box *Element, cur *routeEntry) {
	rt := c.rt
	if rt.kept == nil {
		rt.kept = map[uint64]bool{}
	}
	n := 0
	for d := 1; n < keptPages && (r.at-d >= 0 || r.at+d < len(r.entries)); d++ {
		for _, i := range [2]int{r.at - d, r.at + d} {
			if i < 0 || i >= len(r.entries) || n == keptPages {
				continue
			}
			e := r.entries[i]
			id := keyedID(box.id, e.page)
			if e.page == cur.page || r.leaving != nil && r.dir != 0 && e.page == r.leaving.page || rt.kept[id] {
				continue
			}
			rt.kept[id] = true
			n++
		}
	}
}

// focusPage gives the keyboard focus to the element of the page that had
// it as the page went out of sight, else to the page, unless an element
// of the page took it as it was built (AutoFocus).
func (r *Router) focusPage(c *Context, page *Element, cur *routeEntry) {
	rt := c.rt
	if rt.focused != 0 {
		return
	}
	if s := rt.states[cur.focus]; cur.focus != 0 && s != nil && s.seen == rt.frame && s.pass == rt.pass {
		rt.focused = cur.focus
		rt.blinkStart = time.Now()
		return
	}
	page.Focus()
}

// backdrop returns the color behind an element: the background of the
// nearest element around it that has one.
func backdrop(e *Element) Color {
	for p := e.parent; p != nil; p = p.parent {
		if p.bg.A > 0 {
			return p.bg
		}
	}
	return e.c.theme.Background
}

// within reports whether element id is ancestor or inside it, in the last
// frame.
func (rt *engine) within(id, ancestor uint64) bool {
	for s, n := rt.states[id], 0; s != nil && n < 1024; s, n = rt.states[s.parent], n+1 {
		if s.id == ancestor {
			return true
		}
		if s.parent == 0 {
			break
		}
	}
	return false
}

// BackButton creates a button going back in r's history, disabled at its
// start, as in the toolbar of Finder or a browser; a right click lists
// the pages before, to go back several. ForwardButton goes forward again.
func BackButton(c *Context, r *Router) *Element { return historyButton(c, r, -1) }

// ForwardButton creates a button going forward in r's history, as
// BackButton goes back.
func ForwardButton(c *Context, r *Router) *Element { return historyButton(c, r, 1) }

func historyButton(c *Context, r *Router, d int) *Element {
	t := c.theme
	r.current()
	label, can := "Back", r.CanGoBack()
	if d > 0 {
		label, can = "Forward", r.CanGoForward()
	}
	b := button(c, "", false).Label(label).Tooltip(label).Disabled(!can)
	b.Padding(t.Space(1.5), t.Space(2))
	b.Children(func() {
		color := t.Text
		if !can {
			color = t.TextMuted
		}
		Box(c).Size(t.Space(4), t.Space(4)).Shrink(0).Role(RoleNone).Draw(func(p *Painter, rc Rect) {
			cx, cy, s := rc.X+rc.W/2, rc.Y+rc.H/2, rc.W*0.22
			dx := s * float32(d)
			var path Path
			path.MoveTo(cx-dx/2, cy-s*1.6).LineTo(cx+dx, cy).LineTo(cx-dx/2, cy+s*1.6)
			p.StrokePath(&path, 1.6, color)
		})
	})
	if b.Clicked() {
		r.Go(d)
	}
	b.ContextMenu(func(m *Menu) {
		for i, n := r.at+d, 1; i >= 0 && i < len(r.entries) && n <= 15; i, n = i+d, n+1 {
			e := r.entries[i]
			title := e.title
			if title == "" {
				title = e.loc
			}
			if m.Item(title).Chosen() {
				r.Go(d * n)
			}
		}
	})
	return b
}
