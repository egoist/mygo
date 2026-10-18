package markdown

import (
	"math"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/ui"
)

// frontmatter builds the block of front matter i: a box as a code block's,
// whose header opens and closes the metadata below it.
func (st *viewState) frontmatter(c *ui.Context, i int) ui.Element {
	th := &st.th
	b := st.doc.Block(i)
	cb := &CodeBlockView{Language: b.Info(), Info: b.Info(), Index: i, st: st}
	tx := l10n.In(c.Locale())
	box := ui.Column(c).Background(th.CodeBlockBackground).Border(1, th.CodeBlockBorder).Radius(th.CodeBlockRadius).AlignSelf(ui.Stretch).Clip()
	box.Role(ui.RoleGroup).Label(tx.FrontMatter())
	return box.Children(func() {
		p := ui.CollapsibleBase(c, &st.fmOpen)
		tr := p.Trigger.AlignItems(ui.Center).Gap(th.em(0.4)).Padding(th.em(0.45), th.em(0.6), th.em(0.45), th.em(th.CodeBlockPadding)-th.em(0.2))
		if tr.Hovered() {
			tr.Background(c.Theme().SurfaceHover)
		}
		// The box clips what is around the header: ring inside it, as an
		// accordion's headers do.
		tr.FocusRing(false)
		radius := c.Theme().Radius
		tr.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if tr.FocusVisible() {
				p.FocusRing(ui.Rect{X: r.X + 4, Y: r.Y + 4, W: r.W - 8, H: r.H - 8}, [4]float32{radius, radius, radius, radius})
			}
		})
		tr.Children(func() {
			chevron(c, th, 90*p.Progress())
			ui.Text(c, tx.FrontMatter()).FontSize(th.em(0.8)).SingleLine().Unselectable()
			ui.Text(c, b.Info()).FontSize(th.em(0.8)).TextColor(th.Muted).SingleLine().Unselectable()
		})
		// Nothing while closed.
		p.Panel(func() { cb.Body(c) }).BorderWidth(1, 0, 0, 0).BorderColor(th.CodeBlockBorder)
	})
}

// chevron draws the arrow of a disclosure as ui.Collapsible's: a box of 4
// spaces, the arrow pointing right turned by deg degrees clockwise, or
// left, turned the other way, right to left.
func chevron(c *ui.Context, th *Theme, deg float32) ui.Element {
	size := c.Theme().Space(4)
	e := ui.Box(c).Size(size, size).Shrink(0)
	// The direction of what is built now, which a callback that paints
	// later no longer reads.
	rtl := c.Direction() == ui.RightToLeft
	return e.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy, d := r.X+r.W/2, r.Y+r.H/2, r.W/8
		s := float32(1)
		if rtl {
			s, deg = -1, -deg
		}
		sin, cos := math.Sincos(float64(deg) * math.Pi / 180)
		at := func(x, y float32) (float32, float32) {
			return cx + x*float32(cos) - y*float32(sin), cy + x*float32(sin) + y*float32(cos)
		}
		var path ui.Path
		path.MoveTo(at(-s*d, -2*d)).LineTo(at(s*d, 0)).LineTo(at(-s*d, 2*d))
		p.StrokePath(&path, 1.5, th.Muted)
	})
}
