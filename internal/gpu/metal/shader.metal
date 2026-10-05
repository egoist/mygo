// The one shader of the Metal renderer, as shader.hlsl is the Direct3D
// one: every scene op is an instanced quad, and the fragment shader
// computes the coverage of rounded rectangles, borders, gradients, stripes
// and shadows from signed distances, as the CPU renderer (internal/raster)
// does. Colors are straight (not premultiplied) and blending happens in
// sRGB space, as in browsers, with a second color for the source's alpha
// of each channel, which subpixel glyphs need. go generate compiles it
// into shaderlib.go.

#include <metal_stdlib>
using namespace metal;

// One instance per op, as internal/gpu builds them.
struct Inst {
	float4 rect;      // x, y, width, height in pixels
	float4 radii;     // top-left, top-right, bottom-right, bottom-left
	float4 inner;     // radii of the border's inner edge, or of the box casting a shadow
	float4 color;
	float4 color2;    // gradient end
	float4 border;    // border color
	float4 grad;      // gradient start and end points, or stripes
	float4 uv;        // texture rectangle, normalized, border widths, or the box casting a shadow
	float4 clip;      // the innermost clip rectangle
	float4 clipRadii;
	float4 params;    // kind, dashed or grayscale, sigma or paint, opacity
};

// The color and, for dual-source blending, the source's alpha of each
// channel.
struct PSOut {
	float4 color [[color(0), index(0)]];
	float4 alpha [[color(0), index(1)]];
};

struct VSOut {
	float4 pos [[position]];
	float2 p;
	float2 tex;
	uint inst [[flat]];
};

vertex VSOut vs(uint vid [[vertex_id]], uint iid [[instance_id]],
                const device Inst *insts [[buffer(0)]],
                constant float4 &globals [[buffer(1)]]) {
	Inst i = insts[iid];
	float2 corner = float2(float(vid & 1u), float(vid >> 1u));
	float4 r = i.rect;
	float kind = i.params.x;
	if (kind < 0.5f) {
		r = float4(r.xy - 1.0f, r.zw + 2.0f);
	} else if (kind < 1.5f) {
		float e = 3.0f * i.params.z + 1.0f;
		r = float4(r.xy - e, r.zw + 2.0f * e);
	}
	float2 p = r.xy + corner * r.zw;
	VSOut o;
	o.pos = float4(p / globals.xy * float2(2.0f, -2.0f) + float2(-1.0f, 1.0f), 0.0f, 1.0f);
	o.p = p;
	o.tex = mix(i.uv.xy, i.uv.zw, corner);
	o.inst = iid;
	return o;
}

float sdRoundRect(float2 p, float4 rect, float4 radii) {
	float2 h = rect.zw * 0.5f;
	float2 q = p - rect.xy - h;
	float r = q.x < 0.0f ? (q.y < 0.0f ? radii.x : radii.w) : (q.y < 0.0f ? radii.y : radii.z);
	float2 a = abs(q) - h + r;
	return length(max(a, float2(0.0f))) + min(max(a.x, a.y), 0.0f) - r;
}

float coverage(float d) { return saturate(0.5f - d); }

// Continuous corners, Apple's, as internal/raster/corner.go explains:
// three Béziers a corner, which leave the edge 1.528665 radii from the
// corner. On half a corner, s is the distance from the edge it ends on, t
// along that edge from the corner, k how many radii from the corner it
// ends; the half of the middle Bézier from the diagonal, and the last one.
constant float contExtent = 1.528665f;
constant float contTight = 1.001f;
constant float contNear = 0.03f;
constant float contDiag = 0.29150712f;
constant float contJoinS = 0.074911f;
constant float contJoinT = 0.631494f;
constant float4 contMidS = float4(0.29150712f, 0.19646375f, 0.1219855f, 0.074911f);
constant float4 contMidT = float4(0.29150712f, 0.3865505f, 0.502159f, 0.631494f);

float bezier(float4 p, float x) {
	float y = 1.0f - x;
	return y * y * y * p.x + 3.0f * y * y * x * p.y + 3.0f * y * x * x * p.z + x * x * x * p.w;
}

float bezierSlope(float4 p, float x) {
	float y = 1.0f - x;
	return 3.0f * (y * y * (p.y - p.x) + 2.0f * y * x * (p.z - p.y) + x * x * (p.w - p.z));
}

// solveBezier returns where the monotonic Bézier p is v: Newton's method
// from where a straight line would be.
float solveBezier(float4 p, float v) {
	float x = saturate((v - p.x) / (p.w - p.x));
	for (int n = 0; n < 2; n++) {
		x = saturate(x - (bezier(p, x) - v) / bezierSlope(p, x));
	}
	return x;
}

float4 contEnd(float k) {
	return float4(contJoinT, 0.82f + (k - 1.0f) * 0.0915646031f, 0.96f + (k - 1.0f) * 0.243046165f, k);
}

// contProfile returns the curve's s at t on a half ending at k, and its
// slope ds/dt.
float2 contProfile(float t, float k) {
	if (t <= contDiag) {
		return float2(2.0f * contDiag - t, -1.0f);
	}
	bool mid = t <= contJoinT;
	float4 cs = mid ? contMidS : float4(contJoinS, 0.0f, 0.0f, 0.0f);
	float4 ct = mid ? contMidT : contEnd(k);
	float x = solveBezier(ct, t);
	return float2(bezier(cs, x), bezierSlope(cs, x) / bezierSlope(ct, x));
}

// contDistance returns the signed distance, in radii, to a continuous
// corner's edge from the point u radii inside its vertical edge and v
// inside its horizontal one, from the tangent at the curve's point
// nearest to it.
float contDistance(float u, float v, float kx, float ky) {
	if (u >= kx || v >= ky) {
		return max(-u, -v);
	}
	float s = u, t = v, k = ky;
	if (v < u) {
		s = v; t = u; k = kx;
	}
	float2 c = contProfile(t, k);
	float t1 = t + (s - c.x) * c.y / (1.0f + c.y * c.y);
	c = contProfile(t1, k);
	return (c.x - s + (t - t1) * c.y) / sqrt(1.0f + c.y * c.y);
}

// contInset returns how far from the vertical edge, in radii, the curve
// of a continuous corner is v radii from the horizontal edge.
float contInset(float v, float kx, float ky) {
	if (v >= ky) {
		return 0.0f;
	}
	if (v >= contDiag) {
		return contProfile(v, ky).x;
	}
	if (v <= 0.0f) {
		return kx;
	}
	if (v >= contJoinS) {
		return bezier(contMidT, solveBezier(contMidS, v));
	}
	return bezier(contEnd(kx), 1.0f - pow(v / contJoinS, 1.0f / 3.0f));
}

// areaCoverage returns the area of the pixel at p inside the rectangle,
// near square corners: exact for lines thinner than a pixel too.
float areaCoverage(float2 p, float4 rect) {
	float2 c = saturate(min(rect.xy + rect.zw, p + 0.5f) - max(rect.xy, p - 0.5f));
	return c.x * c.y;
}

// cornerCoverage returns how much of the pixel u inside a continuous
// corner's vertical edge and v inside its horizontal one, within the box
// of the corner's curve, the shape covers; the curve ends kx radii along
// the horizontal edge and ky along the vertical one.
float cornerCoverage(float u, float v, float kx, float ky, float r) {
	// Tight corners are quarter circles, and the others never further
	// from one than contNear radii: far from its edge, the pixel is wholly
	// in or out.
	float2 a = r - float2(u, v);
	float d = length(max(a, 0.0f)) + min(max(a.x, a.y), 0.0f) - r;
	if ((kx < contTight && ky < contTight) || abs(d) > 0.5f + contNear * r) {
		return coverage(d);
	}
	return coverage(contDistance(u / r, v / r, kx, ky) * r);
}

// contCoverage is rectCoverage with continuous corners of radii r: the
// pixel belongs to the corner whose curve's box holds it, if any, as a
// curve may reach past the middle of a side whose other corner is smaller.
float contCoverage(float2 p, float4 rect, float4 r) {
	float4 d = float4(p - rect.xy, rect.xy + rect.zw - p); // left, top, right, bottom
	// Where the curves end along the top, right, bottom and left edges, and
	// for each corner its u and v, and where its curve ends along its
	// horizontal and vertical edges.
	float4 k = min(float4(contExtent), rect.zwzw / max(r + r.yzwx, 1e-6f));
	float4 u = d.xzzx, v = d.yyww, kx = k.xxzz, ky = k.wyyw;
	float4 ex = kx * r, ey = ky * r;
	float4 c;
	float cr;
	if (r.x > 0.0f && u.x < ex.x && v.x < ey.x) {
		c = float4(u.x, v.x, kx.x, ky.x); cr = r.x;
	} else if (r.y > 0.0f && u.y < ex.y && v.y < ey.y) {
		c = float4(u.y, v.y, kx.y, ky.y); cr = r.y;
	} else if (r.z > 0.0f && u.z < ex.z && v.z < ey.z) {
		c = float4(u.z, v.z, kx.z, ky.z); cr = r.z;
	} else if (r.w > 0.0f && u.w < ex.w && v.w < ey.w) {
		c = float4(u.w, v.w, kx.w, ky.w); cr = r.w;
	} else {
		return areaCoverage(p, rect);
	}
	return cornerCoverage(c.x, c.y, c.z, c.w, cr);
}

// rectCoverage returns how much of the pixel at p a rounded rectangle
// covers: by the distance to its edge near rounded corners, and exactly,
// the area of the pixel inside it, near square ones. Negative radii are
// continuous corners.
float rectCoverage(float2 p, float4 rect, float4 radii) {
	if (any(radii < 0.0f)) {
		return contCoverage(p, rect, -radii);
	}
	float2 q = p - rect.xy - rect.zw * 0.5f;
	float r = q.x < 0.0f ? (q.y < 0.0f ? radii.x : radii.w) : (q.y < 0.0f ? radii.y : radii.z);
	if (r > 0.0f) {
		return coverage(sdRoundRect(p, rect, radii));
	}
	return areaCoverage(p, rect);
}

float4 premul(float4 c) { return float4(c.rgb * c.a, c.a); }

float3 unpremul(float4 c) { return c.a > 0.0f ? c.rgb / c.a : float3(0.0f); }

// textCoverage corrects the coverage a of a glyph of straight color c as
// Direct2D blends text (scene.TextCoverage): it enhances the contrast,
// the more the darker c is, and corrects for gamma with the ratios g.
float textCoverage(float a, float3 c, float contrast, float boost, float4 g) {
	float k = contrast * saturate(3.0f - 4.0f * dot(c, float3(0.30f, 0.59f, 0.11f))) + boost;
	a = a * (k + 1.0f) / (a * k + 1.0f);
	float f = dot(c, float3(0.25f, 0.5f, 0.25f));
	return saturate(a + a * (1.0f - a) * ((g.x * f + g.y) * a + (g.z * f + g.w)));
}

// subpixelCoverage does the same for each subpixel of a subpixel glyph.
float3 subpixelCoverage(float3 a, float3 c, float contrast, float boost, float4 g) {
	float k = contrast * saturate(3.0f - 4.0f * dot(c, float3(0.30f, 0.59f, 0.11f))) + boost;
	a = a * (k + 1.0f) / (a * k + 1.0f);
	return saturate(a + a * (1.0f - a) * ((g.x * c + g.y) * a + (g.z * c + g.w)));
}

// erf2 approximates the error function (Abramowitz and Stegun 7.1.27).
float2 erf2(float2 x) {
	float2 s = sign(x);
	float2 a = abs(x);
	x = 1.0f + (0.278393f + (0.230389f + 0.078108f * (a * a)) * a) * a;
	x *= x;
	return s - s / (x * x);
}

float gaussian(float x, float sigma) {
	return exp(-(x * x) / (2.0f * sigma * sigma)) / (2.50662827463f * sigma);
}

// The blurred rounded box of Evan Wallace: exact along x, four samples
// along y. k is where continuous corners end, or 0 for circular ones.
float shadowX(float x, float y, float sigma, float corner, float2 h, float2 k) {
	float curved;
	if (k.x > 0.0f) {
		float v = h.y - abs(y);
		curved = v < k.y * corner ? h.x - corner * contInset(v / corner, k.x, k.y) : h.x;
	} else {
		float delta = min(h.y - corner - abs(y), 0.0f);
		curved = h.x - corner + sqrt(max(0.0f, corner * corner - delta * delta));
	}
	float2 integral = 0.5f + 0.5f * erf2((x + float2(-curved, curved)) * (sqrt(0.5f) / sigma));
	return integral.y - integral.x;
}

float boxShadow(float2 p, float4 rect, float sigma, float4 radii) {
	float4 r = abs(radii);
	float corner = max(max(r.x, r.y), max(r.z, r.w));
	float2 k = float2(0.0f);
	if (any(radii < 0.0f) && corner > 0.0f) {
		k = min(float2(contExtent), rect.zw / (2.0f * corner));
		if (k.x < contTight && k.y < contTight) {
			k = float2(0.0f);
		}
	}
	float2 h = rect.zw * 0.5f;
	p -= rect.xy + h;
	float low = p.y - h.y;
	float high = p.y + h.y;
	float from = clamp(-3.0f * sigma, low, high);
	float to = clamp(3.0f * sigma, low, high);
	float dy = (to - from) / 4.0f;
	float y = from + dy * 0.5f;
	float v = 0.0f;
	for (int n = 0; n < 4; n++) {
		v += shadowX(p.x, p.y - y, sigma, corner, h, k) * gaussian(y, sigma) * dy;
		y += dy;
	}
	return v;
}

float3 toLinear(float3 c) {
	return select(pow((c + 0.055f) / 1.055f, float3(2.4f)), c / 12.92f, c <= 0.04045f);
}

float3 toSRGB(float3 c) {
	return select(1.055f * pow(max(c, float3(0.0f)), float3(1.0f / 2.4f)) - 0.055f, c * 12.92f, c <= 0.0031308f);
}

float3 cbrt3(float3 v) { return sign(v) * pow(abs(v), float3(1.0f / 3.0f)); }

float3 oklab(float3 srgb) {
	float3 c = toLinear(srgb);
	float3 lms = cbrt3(float3(
		0.4122214708f * c.r + 0.5363325363f * c.g + 0.0514459929f * c.b,
		0.2119034982f * c.r + 0.6806995451f * c.g + 0.1073969566f * c.b,
		0.0883024619f * c.r + 0.2817188376f * c.g + 0.6299787005f * c.b));
	return float3(
		0.2104542553f * lms.x + 0.7936177850f * lms.y - 0.0040720468f * lms.z,
		1.9779984951f * lms.x - 2.4285922050f * lms.y + 0.4505937099f * lms.z,
		0.0259040371f * lms.x + 0.7827717662f * lms.y - 0.8086757660f * lms.z);
}

float3 fromOklab(float3 lab) {
	float3 lms = float3(
		lab.x + 0.3963377774f * lab.y + 0.2158037573f * lab.z,
		lab.x - 0.1055613458f * lab.y - 0.0638541728f * lab.z,
		lab.x - 0.0894841775f * lab.y - 1.2914855480f * lab.z);
	lms = lms * lms * lms;
	return toSRGB(float3(
		4.0767416621f * lms.x - 3.3077115913f * lms.y + 0.2309699292f * lms.z,
		-1.2684380046f * lms.x + 2.6097574011f * lms.y - 0.3413193965f * lms.z,
		-0.0041960863f * lms.x - 0.7034186147f * lms.y + 1.7076147010f * lms.z));
}

// paint returns the premultiplied color at p of plain color, a gradient
// mixed in sRGB (1) or Oklab (2), or stripes (3), as scene.Paint says.
float4 paint(float2 p, float mode, float4 rect, float4 c1, float4 c2, float4 g) {
	if (mode < 0.5f) {
		return premul(c1);
	}
	if (mode < 2.5f) {
		float2 d = g.zw - g.xy;
		float t = saturate(dot(p - g.xy, d) / max(dot(d, d), 0.0001f));
		float a = mix(c1.a, c2.a, t);
		if (mode < 1.5f) {
			return float4(mix(c1.rgb * c1.a, c2.rgb * c2.a, t), a);
		}
		float3 lab = mix(oklab(c1.rgb) * c1.a, oklab(c2.rgb) * c2.a, t);
		return a > 0.0f ? float4(saturate(fromOklab(lab / a)) * a, a) : float4(0.0f);
	}
	float s = dot(p - rect.xy, g.xy);
	float phase = s - g.w * floor(s / g.w);
	float cov = coverage(min(max(-phase, phase - g.z), g.w - phase));
	return premul(c1) * cov + premul(c2) * (1.0f - cov);
}

// dash returns how much of a dashed border shows at p: each side, which
// the pixel belongs to when it is nearest that side's edge in widths of
// its border, has an odd number of dashes and gaps of equal length, about
// three widths, starting and ending with a dash.
float dash(float2 p, float4 rect, float4 w) {
	float2 q = p - rect.xy;
	float dt = w.x > 0.0f ? q.y / w.x : 1e9f;
	float dr = w.y > 0.0f ? (rect.z - q.x) / w.y : 1e9f;
	float db = w.z > 0.0f ? (rect.w - q.y) / w.z : 1e9f;
	float dl = w.w > 0.0f ? q.x / w.w : 1e9f;
	float s, len, bw;
	if (dt <= dr && dt <= db && dt <= dl) {
		s = q.x; len = rect.z; bw = w.x;
	} else if (dr <= db && dr <= dl) {
		s = q.y; len = rect.w; bw = w.y;
	} else if (db <= dl) {
		s = rect.z - q.x; len = rect.z; bw = w.z;
	} else {
		s = rect.w - q.y; len = rect.w; bw = w.w;
	}
	// n - 1 is how many periods of a dash and a gap, six widths, fit in
	// len: GPUs divide less exactly than CPUs, and may count one too few
	// or too many where len is a whole number of periods, as sides often
	// are, so the count is checked by multiplying back.
	float n = max(1.0f, floor((len / (3.0f * bw) + 1.0f) * 0.5f + 0.5f));
	float period = 6.0f * bw;
	n += n * period <= len ? 1.0f : 0.0f;
	n -= (n - 1.0f) * period > len ? 1.0f : 0.0f;
	float seg = len / (2.0f * n - 1.0f);
	float k = floor(s / seg);
	float f = s - k * seg;
	float edge = min(f, seg - f);
	return coverage(k - 2.0f * floor(k * 0.5f) < 0.5f ? -edge : edge);
}

fragment PSOut ps(VSOut v [[stage_in]],
                   const device Inst *insts [[buffer(0)]],
                   texture2d<float> maskTex [[texture(0)]],
                   texture2d<float> colorTex [[texture(1)]],
                   texture2d<float> imageTex [[texture(2)]],
                   sampler samp [[sampler(0)]]) {
	Inst i = insts[v.inst];
	float kind = i.params.x;
	float4 res;
	if (kind < 0.5f) {
		float outer = rectCoverage(v.p, i.rect, i.radii);
		res = paint(v.p, i.params.z, i.rect, i.color, i.color2, i.grad) * outer;
		float4 bw = i.uv; // top, right, bottom, left
		if (any(bw > 0.0f)) {
			float4 ir = float4(i.rect.xy + bw.wx, i.rect.zw - bw.yz - bw.wx);
			float innerCov = (ir.z > 0.0f && ir.w > 0.0f) ? rectCoverage(v.p, ir, i.inner) : 0.0f;
			float bc = saturate(outer - innerCov);
			if (i.params.y > 0.5f) {
				bc *= dash(v.p, i.rect, bw);
			}
			float4 b = premul(i.border) * bc;
			res = b + res * (1.0f - b.a);
		}
	} else if (kind < 1.5f) {
		float sigma = i.params.z;
		float s = sigma > 0.0f ? boxShadow(v.p, i.rect, sigma, i.radii) : rectCoverage(v.p, i.rect, i.radii);
		if (i.uv.z > 0.0f && i.uv.w > 0.0f) {
			s *= 1.0f - rectCoverage(v.p, i.uv, i.inner); // outside the box casting it
		}
		res = premul(i.color) * s;
	} else if (kind < 2.5f) {
		float4 c = paint(v.p, i.params.z, i.rect, i.color, i.color2, i.grad);
		res = c * textCoverage(maskTex.sample(samp, v.tex).r, unpremul(c), i.inner.x, i.inner.y, i.radii);
	} else if (kind < 3.5f) {
		res = colorTex.sample(samp, v.tex) * i.color.a;
	} else if (kind < 4.5f) {
		res = imageTex.sample(samp, v.tex) * rectCoverage(v.p, i.rect, i.radii);
		if (i.params.y > 0.5f) {
			res.rgb = float3(dot(res.rgb, float3(0.2126f, 0.7152f, 0.0722f)));
		}
	} else {
		float4 c = paint(v.p, i.params.z, i.rect, i.color, i.color2, i.grad);
		float3 straight = unpremul(c);
		float3 a = subpixelCoverage(colorTex.sample(samp, v.tex).rgb, straight, i.inner.x, i.inner.y, i.radii);
		float3 w = a * c.a * rectCoverage(v.p, i.clip, i.clipRadii) * i.params.w;
		float wa = (w.r + w.g + w.b) / 3.0f;
		return PSOut{float4(straight * w, wa), float4(w, wa)};
	}
	float clip = rectCoverage(v.p, i.clip, i.clipRadii);
	res *= clip * i.params.w;
	return PSOut{res, res.aaaa};
}
