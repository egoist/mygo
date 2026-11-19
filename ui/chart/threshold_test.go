package chart

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestThreshold(t *testing.T) {
	cutoffs := []float64{-10, 0, 100}
	s := NewThreshold(cutoffs...)
	cutoffs[0] = -20 // changing the caller's slice cannot move a boundary
	bounds := [][2]float64{{math.Inf(-1), -10}, {-10, 0}, {0, 100}, {100, math.Inf(1)}}
	if s.Len() != len(bounds) {
		t.Fatalf("Len() = %d, want %d", s.Len(), len(bounds))
	}
	for i, want := range bounds {
		lo, hi, ok := s.Bounds(i)
		if !ok || [2]float64{lo, hi} != want {
			t.Fatalf("Bounds(%d) = (%g, %g, %v), want %v", i, lo, hi, ok, want)
		}
		// Test both sides of every displayed boundary, including the
		// adjacent float below the exclusive upper bound.
		for _, v := range []float64{math.Nextafter(lo, hi), math.Nextafter(hi, lo)} {
			if got, ok := s.Index(v); !ok || got != i {
				t.Errorf("Index(%g) = (%d, %v), want bucket %d", v, got, ok, i)
			}
		}
		if i > 0 {
			if got, ok := s.Index(lo); !ok || got != i {
				t.Errorf("Index(%g) at the cutoff = (%d, %v), want bucket %d", lo, got, ok, i)
			}
		}
	}
	for _, i := range []int{-1, s.Len()} {
		if _, _, ok := s.Bounds(i); ok {
			t.Errorf("Bounds(%d) succeeded outside the scale", i)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(-1), math.Inf(1)} {
		if _, ok := s.Index(v); ok {
			t.Errorf("Index(%g) returned a bucket", v)
		}
	}
}

func TestThresholdEmpty(t *testing.T) {
	var zero Threshold
	if lo, hi, ok := zero.Bounds(0); zero.Len() != 1 || !ok || !math.IsInf(lo, -1) || !math.IsInf(hi, 1) {
		t.Errorf("zero scale has %d buckets, Bounds(0) = (%g, %g, %v)", zero.Len(), lo, hi, ok)
	}
	empty := NewThreshold()
	if i, ok := empty.Index(123); !ok || i != 0 {
		t.Errorf("empty cutoffs: Index(123) = (%d, %v), want (0, true)", i, ok)
	}
}

func TestThresholdHeatmap(t *testing.T) {
	s := Threshold{Cutoffs: []float64{100, 300}}
	palette := []ui.Color{ui.RGB(0, 128, 0), ui.RGB(255, 128, 0), ui.RGB(255, 0, 0)}
	values := []float64{99, 100, 300, math.NaN()}
	columns := NewBand("a", "b", "c", "d")
	tt := ui.NewTester(func(c *ui.Context) {
		Plot(c, Axes{}, func(area ui.Element) {
			area.Background(ui.RGB(255, 255, 255))
			for i, v := range values {
				if bucket, ok := s.Index(v); ok {
					Rect(c, columns.Band(i), Span{0, 1}).Background(palette[bucket])
				}
			}
		}).Fill()
	}, 120, 30)
	want := [][4]uint8{{0, 128, 0, 255}, {255, 128, 0, 255}, {255, 0, 0, 255}, {255, 255, 255, 255}}
	for i, color := range want {
		if got := pixel(tt, 15+i*30, 15); got != color {
			t.Errorf("cell %d has color %v, want %v", i, got, color)
		}
	}
	values[0], values[2] = math.NaN(), math.NaN() // filter the low and high samples
	tt.Frame()
	if got := pixel(tt, 45, 15); got != want[1] {
		t.Errorf("the same value after filtering has color %v, want %v", got, want[1])
	}
}
