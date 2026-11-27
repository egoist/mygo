package scene

import "testing"

// TestOklab checks the color space the gradients mix in, at the colors of
// Ottosson's table.
func TestOklab(t *testing.T) {
	for _, tc := range []struct {
		c       Color
		l, a, b float32
		loose   float32
		what    string
	}{
		{Color{255, 255, 255, 255}, 1, 0, 0, 1e-5, "white"},
		{Color{0, 0, 0, 255}, 0, 0, 0, 1e-5, "black"},
		{Color{255, 0, 0, 255}, 0.62796, 0.22486, 0.12585, 2e-4, "red"},
	} {
		o := Oklab(tc.c)
		if o[0] < tc.l-tc.loose || o[0] > tc.l+tc.loose ||
			o[1] < tc.a-tc.loose || o[1] > tc.a+tc.loose ||
			o[2] < tc.b-tc.loose || o[2] > tc.b+tc.loose {
			t.Errorf("Oklab(%s) = %v, want (%v, %v, %v)", tc.what, o, tc.l, tc.a, tc.b)
		}
	}
	// A gray is on the gray axis: no a or b to speak of.
	o := Oklab(Color{128, 128, 128, 255})
	if o[1] > 0.002 || o[2] > 0.002 {
		t.Errorf("Oklab(gray) = %v, want a and b of nothing", o)
	}
	if o[0] < 0.59 || o[0] > 0.61 {
		t.Errorf("Oklab(gray) lightness = %v, want about 0.6", o[0])
	}
}

// TestToLinear checks the two legs of sRGB's encoding.
func TestToLinear(t *testing.T) {
	if v := toLinear(0); v != 0 {
		t.Errorf("toLinear(0) = %v", v)
	}
	if v := toLinear(1); v != 1 {
		t.Errorf("toLinear(1) = %v", v)
	}
	// Below the knee the encoding is a line, above it a power.
	if v := toLinear(0.04045); v < 0.00313 || v > 0.00314 {
		t.Errorf("toLinear(0.04045) = %v", v)
	}
	if v := toLinear(0.5); v < 0.2138 || v > 0.2143 {
		t.Errorf("toLinear(0.5) = %v", v)
	}
}
