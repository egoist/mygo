package chart

import (
	"fmt"
	"math"
	"slices"
)

// Threshold classifies finite values into buckets separated by fixed
// cutoffs, as d3's scaleThreshold does. Use it for colors or other styles
// that mean the same thing across views of different data. The zero value
// has one bucket for all finite values.
type Threshold struct {
	cutoffs []float64
}

// NewThreshold returns a scale with one more bucket than cutoffs. It copies
// the cutoffs, which must be finite and strictly increasing.
//
//	latency, err := chart.NewThreshold(100, 300) // milliseconds
func NewThreshold(cutoffs ...float64) (Threshold, error) {
	for i, v := range cutoffs {
		if !finiteScale(v) || i > 0 && v <= cutoffs[i-1] {
			return Threshold{}, fmt.Errorf("chart: cutoff %d (%g) must be finite and strictly increasing", i, v)
		}
	}
	return Threshold{cutoffs: slices.Clone(cutoffs)}, nil
}

// Len returns the number of buckets.
func (s Threshold) Len() int { return len(s.cutoffs) + 1 }

// Index returns the bucket of v, counting from zero. A value equal to a
// cutoff belongs to the bucket above it. NaN and infinities have no bucket.
func (s Threshold) Index(v float64) (i int, ok bool) {
	if !finiteScale(v) {
		return 0, false
	}
	i, found := slices.BinarySearch(s.cutoffs, v)
	if found {
		i++
	}
	return i, true
}

// Bounds returns bucket i's inclusive lower bound and exclusive upper
// bound. The first and last buckets are unbounded at their outer ends,
// reported as negative and positive infinity, respectively. ok is false
// when i is outside the scale.
func (s Threshold) Bounds(i int) (lo, hi float64, ok bool) {
	if i < 0 || i >= s.Len() {
		return 0, 0, false
	}
	lo, hi = math.Inf(-1), math.Inf(1)
	if i > 0 {
		lo = s.cutoffs[i-1]
	}
	if i < len(s.cutoffs) {
		hi = s.cutoffs[i]
	}
	return lo, hi, true
}
