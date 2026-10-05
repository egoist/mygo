package raster

import "math"

// Continuous corners are Apple's (Core Animation's continuous corner
// curve, SwiftUI's rounded rectangles), from the paths SwiftUI makes: each
// is three cubic Béziers in place of a quarter circle. The curve leaves
// the edge ContinuousExtent radii from the corner instead of one, bends
// more and more, crosses the corner's diagonal where a quarter circle
// would, within half a percent of the radius, and bends less and less to
// the other edge. Where the side is too short for both of its corners'
// curves, as a pill's, each ends halfway (in proportion to the radii),
// and the control points of the curve's end move between those of a
// curve ending one radius from the corner and those of the whole curve,
// as SwiftUI's do; corners that end one radius from the corner both ways,
// a circle's, stay quarter circles.
//
// The functions work in radii on half of a corner: s is the distance from
// the edge that half ends on, t the distance along it from the corner, k
// how many radii from the corner it ends (1 to ContinuousExtent). The
// other half is the same with s and t swapped and its own k. The shaders
// compute the same.
const (
	// ContinuousExtent is how many radii from the corner the curve of a
	// continuous corner leaves an edge.
	ContinuousExtent = 1.528665
	// contDiag is where the curve crosses the corner's diagonal.
	contDiag = 0.29150712
	// contJoin is where the middle Bézier ends: (contJoinS, contJoinT).
	contJoinS = 0.074911
	contJoinT = 0.631494
	// contTight is the most a corner's k may be for the corner to be a
	// quarter circle.
	contTight = 1.001
	// contNear is, in radii, more than the distance to a continuous
	// corner's edge ever differs from the distance to a quarter circle's
	// of the same radius, within half a radius of it (0.023).
	contNear = 0.03
)

// The half of the middle Bézier from the diagonal to its end, and the
// control points of the last one, from contJoin to (0, k), at k = 1 and
// for every radius more.
var (
	contMidS = [4]float32{contDiag, 0.19646375, 0.1219855, contJoinS}
	contMidT = [4]float32{contDiag, 0.3865505, 0.502159, contJoinT}
	contEndS = [4]float32{contJoinS, 0, 0, 0}
)

const (
	contEnd1, contEnd1Slope = 0.82, (0.868407 - 0.82) / (ContinuousExtent - 1)
	contEnd2, contEnd2Slope = 0.96, (1.088490 - 0.96) / (ContinuousExtent - 1)
)

func bezier(p [4]float32, x float32) float32 {
	y := 1 - x
	return y*y*y*p[0] + 3*y*y*x*p[1] + 3*y*x*x*p[2] + x*x*x*p[3]
}

func bezierSlope(p [4]float32, x float32) float32 {
	y := 1 - x
	return 3 * (y*y*(p[1]-p[0]) + 2*y*x*(p[2]-p[1]) + x*x*(p[3]-p[2]))
}

// solveBezier returns the parameter where the monotonic Bézier p is v,
// with two steps of Newton's method from where a straight line would
// be: the curves are close to straight.
func solveBezier(p [4]float32, v float32) float32 {
	x := clamp01((v - p[0]) / (p[3] - p[0]))
	for range 2 {
		x = clamp01(x - (bezier(p, x)-v)/bezierSlope(p, x))
	}
	return x
}

// contEnd returns the t of the last Bézier of a half ending k radii from
// the corner.
func contEnd(k float32) [4]float32 {
	return [4]float32{contJoinT, contEnd1 + (k-1)*contEnd1Slope, contEnd2 + (k-1)*contEnd2Slope, k}
}

// contProfile returns the curve's s at t on a half ending at k, and its
// slope ds/dt; before the diagonal, the tangent there.
func contProfile(t, k float32) (s, slope float32) {
	if t <= contDiag {
		return 2*contDiag - t, -1
	}
	cs, ct := contMidS, contMidT
	if t > contJoinT {
		cs, ct = contEndS, contEnd(k)
	}
	x := solveBezier(ct, t)
	return bezier(cs, x), bezierSlope(cs, x) / bezierSlope(ct, x)
}

// contDistance returns the signed distance, in radii, from a continuous
// corner's edge to the point u radii inside its vertical edge and v inside
// its horizontal one, positive outside the shape; the corner's curve ends
// kx radii along the horizontal edge and ky along the vertical one. Near
// the curve, it is the distance along the normal of the curve at the
// point's t, which antialiasing needs, within a few thousandths of a
// pixel.
func contDistance(u, v, kx, ky float32) float32 {
	if u >= kx || v >= ky {
		return max(-u, -v)
	}
	s, t, k := u, v, ky
	if v < u {
		s, t, k = v, u, kx
	}
	// The tangent at t meets the normal through the point near the curve's
	// point nearest to it, t1: the distance to the tangent there is within
	// a thousandth of a pixel of the distance to the curve.
	cs, slope := contProfile(t, k)
	t1 := t + (s-cs)*slope/(1+slope*slope)
	cs, slope = contProfile(t1, k)
	return (cs - s + (t-t1)*slope) / float32(math.Sqrt(float64(1+slope*slope)))
}

// contInset returns how far from the vertical edge, in radii, the curve
// of a continuous corner is v radii from the horizontal edge.
func contInset(v, kx, ky float32) float32 {
	switch {
	case v >= ky:
		return 0
	case v >= contDiag:
		s, _ := contProfile(v, ky)
		return s
	case v <= 0:
		return kx
	case v >= contJoinS:
		// The other half, where its s is v.
		return bezier(contMidT, solveBezier(contMidS, v))
	}
	return bezier(contEnd(kx), 1-float32(math.Cbrt(float64(v/contJoinS))))
}

// contExtents returns how many radii from the corner the curve of a
// corner of radius r ends along the horizontal and vertical edges, of
// lengths w and h, which share it with corners of radii rh and rv.
func contExtents(r, rh, rv, w, h float32) (kx, ky float32) {
	return min(ContinuousExtent, w/(r+rh)), min(ContinuousExtent, h/(r+rv))
}
