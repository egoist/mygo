//go:build windows && (amd64 || arm64)

package windows

import "testing"

func TestAspectRect(t *testing.T) {
	const frameW, frameH = 10, 30
	for _, c := range []struct {
		name  string
		r     rect
		edge  uint32
		ratio float64
		want  rect
	}{
		{"corner keeps an exact ratio", rect{0, 0, 410, 230}, wmszBottomRight, 2, rect{0, 0, 410, 230}},
		{"corner grows a tall proposal's width", rect{0, 0, 410, 330}, wmszBottomRight, 2, rect{0, 0, 610, 330}},
		{"corner grows a wide proposal's height", rect{0, 0, 610, 230}, wmszBottomRight, 2, rect{0, 0, 610, 330}},
		{"top-left keeps the opposite corner", rect{100, 100, 500, 400}, wmszTopLeft, 2, rect{-50, 100, 500, 400}},
		{"top-right keeps the opposite corner", rect{100, 100, 700, 300}, wmszTopRight, 2, rect{100, -25, 700, 300}},
		{"bottom-left keeps the opposite corner", rect{100, 100, 500, 400}, wmszBottomLeft, 2, rect{-50, 100, 500, 400}},
		{"right edge centers the height", rect{100, 100, 710, 330}, wmszRight, 2, rect{100, 50, 710, 380}},
		{"left edge centers the height", rect{100, 100, 710, 330}, wmszLeft, 2, rect{100, 50, 710, 380}},
		{"bottom edge centers the width", rect{100, 100, 410, 430}, wmszBottom, 2, rect{-50, 100, 560, 430}},
		{"top edge centers the width", rect{100, 100, 410, 430}, wmszTop, 2, rect{-50, 100, 560, 430}},
		{"rounds the derived dimension", rect{0, 0, 1010, 500}, wmszBottomRight, 16.0 / 9.0, rect{0, 0, 1010, 593}},
		{"leaves a degenerate proposal", rect{0, 0, 5, 330}, wmszBottomRight, 2, rect{0, 0, 5, 330}},
	} {
		r := c.r
		aspectRect(&r, c.edge, c.ratio, frameW, frameH)
		if r != c.want {
			t.Errorf("%s: aspectRect(%+v, %d, %v) = %+v, want %+v", c.name, c.r, c.edge, c.ratio, r, c.want)
		}
	}
}