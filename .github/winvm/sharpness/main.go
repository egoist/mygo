// sharpness prints, for a screenshot of textscale, how sharp the left
// (scaled) card's text is relative to the right (laid out) card's: the
// variance of the Laplacian of luminance over each card's box, found as
// the two largest white areas, left over right. 1 is as sharp.
package main

import (
	"fmt"
	"image"
	_ "image/png"
	"log"
	"os"
	"sort"
)

type box struct{ x0, y0, x1, y1, n int }

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	m, _, err := image.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	b := m.Bounds()
	w, h := b.Dx(), b.Dy()
	lum := make([]float64, w*h)
	white := make([]bool, w*h)
	for y := range h {
		for x := range w {
			r, g, bl, _ := m.At(b.Min.X+x, b.Min.Y+y).RGBA()
			lum[y*w+x] = (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257
			white[y*w+x] = r>>8 >= 250 && g>>8 >= 250 && bl>>8 >= 250
		}
	}
	// Connected white areas, by flood fill.
	seen := make([]bool, w*h)
	var boxes []box
	stack := []int{}
	for i := range white {
		if !white[i] || seen[i] {
			continue
		}
		bx := box{w, h, 0, 0, 0}
		stack = append(stack[:0], i)
		seen[i] = true
		for len(stack) > 0 {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := j%w, j/w
			bx.n++
			bx.x0, bx.y0, bx.x1, bx.y1 = min(bx.x0, x), min(bx.y0, y), max(bx.x1, x), max(bx.y1, y)
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := x+d[0], y+d[1]
				if nx >= 0 && ny >= 0 && nx < w && ny < h && white[ny*w+nx] && !seen[ny*w+nx] {
					seen[ny*w+nx] = true
					stack = append(stack, ny*w+nx)
				}
			}
		}
		boxes = append(boxes, bx)
	}
	sort.Slice(boxes, func(i, j int) bool { return boxes[i].n > boxes[j].n })
	if len(boxes) < 2 {
		log.Fatal("cards not found")
	}
	cards := boxes[:2]
	sort.Slice(cards, func(i, j int) bool { return cards[i].x0 < cards[j].x0 })
	v := make([]float64, 2)
	for c, bx := range cards {
		var sum, sq float64
		n := 0
		// Inside the card, away from its border.
		for y := bx.y0 + 4; y <= bx.y1-4; y++ {
			for x := bx.x0 + 4; x <= bx.x1-4; x++ {
				l := 4*lum[y*w+x] - lum[y*w+x-1] - lum[y*w+x+1] - lum[(y-1)*w+x] - lum[(y+1)*w+x]
				sum += l
				sq += l * l
				n++
			}
		}
		mean := sum / float64(n)
		v[c] = sq/float64(n) - mean*mean
		fmt.Printf("card %d: box (%d,%d)-(%d,%d), laplacian variance %.1f\n", c, bx.x0, bx.y0, bx.x1, bx.y1, v[c])
	}
	fmt.Printf("sharpness %.3f (left %dx%d, right %dx%d)\n", v[0]/v[1], cards[0].x1-cards[0].x0, cards[0].y1-cards[0].y0, cards[1].x1-cards[1].x0, cards[1].y1-cards[1].y0)
}
