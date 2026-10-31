package mobile

import (
	"github.com/egoist/mygo/internal/mobile/wire"
	"github.com/egoist/mygo/internal/platform"
)

// The navigation of phones (platform.NavigationBackend): the host's tab
// bar, each tab a window, and stacks of windows pushed over others, under
// the host's navigation bar.

type tabBar struct {
	b  *Backend
	id int
	h  platform.TabsHandler
}

func (b *Backend) NewTabs(tabs []platform.Tab, h platform.TabsHandler) (platform.TabBar, error) {
	b.nextID++
	t := &tabBar{b: b, id: b.nextID, h: h}
	b.call(&wire.CreateTabs{ID: t.id}, nil)
	old := b.front()
	for _, tab := range tabs {
		w := tab.Window.(*window)
		w.visible = true
		b.unshow(w)
		b.shown = append(b.shown, w)
		b.call(&wire.AddTab{ID: t.id, Window: w.id, Title: tab.Title, Symbol: tab.Symbol, Icon: tab.Icon}, nil)
	}
	// The first tab is in front.
	first := tabs[0].Window.(*window)
	b.unshow(first)
	b.shown = append(b.shown, first)
	b.tabs[t.id] = t
	b.call(&wire.ShowTabs{ID: t.id}, nil)
	b.refocus(old)
	return t, nil
}

func (t *tabBar) Select(i int) { t.b.call(&wire.SelectTab{ID: t.id, Index: i}, nil) }

func (t *tabBar) SetBadge(i int, badge string) {
	t.b.call(&wire.SetTabBadge{ID: t.id, Index: i, Badge: badge}, nil)
}

func (t *tabBar) Close() {
	delete(t.b.tabs, t.id)
	t.b.call(&wire.CloseTabs{ID: t.id}, nil)
}

func (b *Backend) PushWindow(parent, child platform.Window, animated bool) {
	p, c := parent.(*window), child.(*window)
	old := b.front()
	c.visible = true
	b.unshow(c)
	b.shown = append(b.shown, c)
	b.call(&wire.PushWindow{Parent: p.id, Child: c.id, Title: c.title, Animated: animated}, nil)
	b.refocus(old)
}

func (b *Backend) PopWindow(w platform.Window, animated bool) {
	b.call(&wire.PopWindow{ID: w.(*window).id, Animated: animated}, nil)
}

// handleNavigation handles the events of navigation, and reports whether
// op was one.
func (b *Backend) handleNavigation(op uint64, r *wire.Reader) bool {
	switch op {
	case wire.EventTabSelected:
		var e wire.TabSelectedEvent
		e.Decode(r)
		if t := b.tabs[e.ID]; t != nil {
			t.h.TabSelected(e.Index)
		}
	case wire.EventPopped:
		var e wire.PoppedEvent
		e.Decode(r)
		if w := b.windows[e.ID]; w != nil {
			if h, ok := w.h.(platform.NavigationHandler); ok {
				h.Popped()
			} else {
				w.Close()
			}
		}
	default:
		return false
	}
	return true
}
