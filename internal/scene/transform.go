package scene

import "math"

// ClipShape is a clip in the coordinate system in which it was pushed.
type ClipShape struct {
	Rect       Rect
	Radii      [4]float32
	Transform  Affine
	Continuous bool
}

// Affine maps drawing coordinates to frame coordinates. The zero value is
// identity; Set distinguishes a singular matrix (including scale zero).
type Affine struct {
	A, B, C, D, X, Y float32
	Set              bool
}

func Matrix(a, b, c, d, x, y float32) Affine {
	if a == 1 && b == 0 && c == 0 && d == 1 && x == 0 && y == 0 {
		return Affine{}
	}
	return Affine{a, b, c, d, x, y, true}
}

func Translation(x, y float32) Affine { return Matrix(1, 0, 0, 1, x, y) }
func Scaling(x, y float32) Affine     { return Matrix(x, 0, 0, y, 0, 0) }
func Rotation(degrees float32) Affine {
	angle := math.Remainder(float64(degrees), 360)
	switch angle {
	case 0:
		return Affine{}
	case 90:
		return Matrix(0, 1, -1, 0, 0, 0)
	case -90:
		return Matrix(0, -1, 1, 0, 0, 0)
	case 180, -180:
		return Matrix(-1, 0, 0, -1, 0, 0)
	}
	s, c := math.Sincos(angle * math.Pi / 180)
	return Matrix(float32(c), float32(s), -float32(s), float32(c), 0, 0)
}

// Mul returns m*n: n acts first, then m.
func (m Affine) Mul(n Affine) Affine {
	if !m.Set {
		return n
	}
	if !n.Set {
		return m
	}
	return Matrix(m.A*n.A+m.C*n.B, m.B*n.A+m.D*n.B,
		m.A*n.C+m.C*n.D, m.B*n.C+m.D*n.D,
		m.A*n.X+m.C*n.Y+m.X, m.B*n.X+m.D*n.Y+m.Y)
}

func (m Affine) Point(x, y float32) (float32, float32) {
	if !m.Set {
		return x, y
	}
	return m.A*x + m.C*y + m.X, m.B*x + m.D*y + m.Y
}

func (m Affine) Vector(x, y float32) (float32, float32) {
	if !m.Set {
		return x, y
	}
	return m.A*x + m.C*y, m.B*x + m.D*y
}

func (m Affine) Inverse() (Affine, bool) {
	if !m.Set {
		return m, true
	}
	det := float64(m.A)*float64(m.D) - float64(m.B)*float64(m.C)
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return Affine{}, false
	}
	a, b, c, d := float32(float64(m.D)/det), float32(-float64(m.B)/det), float32(-float64(m.C)/det), float32(float64(m.A)/det)
	n := Matrix(a, b, c, d, -a*m.X-c*m.Y, -b*m.X-d*m.Y)
	for _, v := range [6]float32{n.A, n.B, n.C, n.D, n.X, n.Y} {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return Affine{}, false
		}
	}
	return n, true
}

func (m Affine) Bounds(r Rect) Rect {
	if !m.Set {
		return r
	}
	x0, y0 := m.Point(r.X, r.Y)
	x1, y1 := x0, y0
	for _, p := range [3][2]float32{{r.X + r.W, r.Y}, {r.X, r.Y + r.H}, {r.X + r.W, r.Y + r.H}} {
		x, y := m.Point(p[0], p[1])
		x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Pixels expresses a DIP transform in device pixels.
func (m Affine) Pixels(scale float32) Affine {
	if m.Set {
		m.X *= scale
		m.Y *= scale
	}
	return m
}

// HasTransforms reports whether the frame needs affine composition.
func (s *Scene) HasTransforms() bool {
	for i := range s.Ops {
		if s.Ops[i].Transform.Set {
			return true
		}
	}
	for i := range s.Glyphs {
		if s.Glyphs[i].Transform.Set {
			return true
		}
	}
	return false
}
