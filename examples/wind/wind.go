package main

import "math"

// The wind's settings, the same for every visit.
const (
	// lull is how much calm lies between gusts, and streak how streaked a
	// gust is, both from 0 to 1.
	lull, streak = 0.5, 0.4
	// detail is how broken a gust's outline is, edge how sharp its start,
	// curl how far the air curls, and wisp how many strands trail a gust.
	detail, edge, curl, wisp = 0.6, 0.75, 0.65, 0.5
	// tail is how far behind itself a gust fades out, sweep how far gusts
	// keep to the crests of the wind's long swells, and morph how fast a
	// gust changes shape as it travels, against a slow 1.
	tail, sweep, morph = 0.35, 0.3, 3
	// The content's card lies cardY down the pane, its middle; the
	// texture thins to calmLeft of its light around it, over calmX by
	// calmY DIPs.
	cardY, calmX, calmY, calmLeft = 0.45, 250, 80, 0.3
)

const tau = 2 * math.Pi

// wind is a visit's wind, which its seed picks: its look, the set of
// characters it shows; the heading it blows toward, in radians with y
// down; its speed, in DIPs a second; the length and width of a gust; and
// how far its path bends, and the size of a bend.
type wind struct {
	seed                                                   uint32
	look                                                   string
	heading, speed, gustLength, gustWidth, bend, bendScale float32
}

// newWind returns the wind of seed, with look.
func newWind(seed uint32, look string) wind {
	next := random(seed, "wind")
	between := func(lo, hi float32) float32 { return lo + (hi-lo)*float32(next()) }
	w := wind{seed: seed, look: look, heading: headingOf(next())}
	w.speed = between(42, 64)
	w.gustLength = between(380, 560)
	w.gustWidth = between(120, 190)
	w.bend = between(60, 110)
	w.bendScale = between(300, 440)
	return w
}

// headingOf returns the heading of a fraction e: across the pane and down
// it, from 18° to 42° below the horizontal, rightward for e below 0.5 and
// leftward above.
func headingOf(e float64) float32 {
	lo := math.Pi / 10
	a := lo + (math.Pi/4.3-lo)*math.Mod(e*2, 1)
	if e < 0.5 {
		return float32(a)
	}
	return float32(math.Pi - a)
}

// field is the wind at a moment, in a pane: what the texture's light
// samples, two to a cell (one above the other), each a byte a channel as
// a texture holds it: how lit (R), which way the air blows there (G and
// B, from 0 for -1 to 255 for 1), and how much of the resting light shows
// (A).
type field struct {
	noises
	w      *wind
	time   float32 // the wind's time, in seconds
	reveal float32 // how far the texture has come in, 0 to 1
	hand   *hand
	// The pane's size, and the samples': the first's middle and the
	// distance between two, in DIPs.
	paneW, paneH                   float32
	originX, originY, stepX, stepY float32
	headX, headY                   float32
	carried, span                  float32
	cols, rows                     int // samples
	pix                            []uint8
}

// prepare readies the field for the time, the pane and the canvas, of
// canvasW×canvasH DIPs: how far the wind has carried its gusts by now,
// surging and slackening, and the span of its long swells, about one to
// the canvas.
func (f *field) prepare(canvasW, canvasH float32) {
	f.headX, f.headY = cos(f.w.heading), float32(math.Sin(float64(f.w.heading)))
	f.carried = f.w.speed * (f.time + f.value(f.time*0.09, 3.7, 0, 0x58))
	f.span = 0.58 * (abs(f.headX)*canvasW + abs(f.headY)*canvasH)
	if n := 4 * f.cols * f.rows; cap(f.pix) < n {
		f.pix = make([]uint8, n)
	} else {
		f.pix = f.pix[:n]
	}
}

// gusts returns how gusty the air is at (x, y) in the wind's own frame,
// along it and across it: four sizes of gust travelling nearly together,
// longer along the wind than across, thin strands between them, and the
// long swells they gather on.
func (f *field) gusts(x, y float32) float32 {
	const gain = 0.38 + (0.62-0.38)*detail
	var gust, power float32
	weight := float32(1)
	sx, sy := 0.85*f.w.gustLength, 0.75*f.w.gustWidth
	for i := range 4 {
		k := float32(i)
		gust += weight * f.gradient((x-f.carried*(1+0.1*k))/sx, y/sy, morph*f.time*(0.03+0.018*k)+11*k, 0x60+uint32(i))
		power += weight * weight
		weight *= gain
		sx, sy = sx*0.5, sy*0.58
	}
	strand := 1 - abs(f.gradient((x-1.15*f.carried)/(0.7*f.w.gustLength), y/(0.95*f.w.gustWidth), morph*f.time*0.05+2, 0x66))
	// A crest runs now ahead of its time and now behind it, by up to half
	// the way to the next.
	swell := cos((x - f.carried + 0.5*f.span*f.value(f.time*0.045, y/(2.5*f.w.gustWidth), 7, 0x67)) * (tau / f.span))
	return gust/sqrt(power) + wisp*0.5*(strand*strand*strand-0.35) + sweep*swell
}

// sample computes the sample at (x, y) in the pane, in DIPs, into px.
func (f *field) sample(x, y float32, px []uint8) {
	var push [4]float32 // the hand's push (x, y), the wind it adds, how roused
	var warm float32
	if f.hand.active() {
		// Read a little off true, so that a ripple's outline is never a
		// drawn circle.
		nx := x + 24*f.value(x/95, y/95, f.hand.now*0.12, 0x71)
		ny := y + 24*f.value(x/95+9.1, y/95+9.1, f.hand.now*0.12+4, 0x72)
		push, _ = f.hand.push(nx, ny)
		warm = f.hand.warmth(nx, ny)
		push[2] += 0.25 * warm
		push[3] += 0.05 * warm
	}
	// Air that was pushed shows here what was over there, and the wind's
	// path bends, slowly, carrying it along.
	fromX, fromY := x-push[0], y-push[1]
	turnX := f.value(fromX/f.w.bendScale, fromY/f.w.bendScale, f.time*0.04, 0x51)
	turnY := f.value(fromX/f.w.bendScale+17.3, fromY/f.w.bendScale+17.3, f.time*0.04+5, 0x52)
	bentX, bentY := fromX+f.w.bend*turnX, fromY+f.w.bend*turnY
	// In the wind's frame: along it, and across it.
	hx, hy := f.headX, f.headY
	ax, ay := -hy, hx
	qx, qy := bentX*hx+bentY*hy, bentX*ax+bentY*ay

	// The air curls as it travels: small eddies carried on large ones, and
	// the smallest folds, two or three rows of characters across.
	gw := f.w.gustWidth
	rx, ry := qx-0.85*f.carried, qy
	es := 1.15 * gw
	eddyX := f.gradient(rx/es, ry/es, morph*f.time*0.04, 0x59)
	eddyY := f.gradient(rx/es+31.7, ry/es+31.7, morph*f.time*0.04+3, 0x5a)
	foldX, foldY := rx+curl*0.9*gw*eddyX, ry+curl*0.9*gw*eddyY
	fs := 0.38 * gw
	fineX := f.gradient(foldX/fs, foldY/fs, morph*f.time*0.06, 0x5b)
	fineY := f.gradient(foldX/fs+13.1, foldY/fs+13.1, morph*f.time*0.06+7, 0x5c)
	cx, cy := (foldX+0.3*gw*fineX)/24, (foldY+0.3*gw*fineY)/24
	tinyX := f.gradient(cx, cy, f.time*0.07, 0x5d)
	tinyY := f.gradient(cx+5.3, cy+5.3, f.time*0.07+1, 0x5e)
	// Roused air curls further.
	k := curl * (1 + 0.9*min(push[3], 1.5))
	curledX := qx + k*(gw*(0.9*eddyX+0.3*fineX)+detail*30*tinyX)
	curledY := qy + k*(gw*(0.9*eddyY+0.3*fineY)+detail*30*tinyY)
	along, across := curl*(0.45*eddyX+0.25*fineX), curl*(0.45*eddyY+0.25*fineY)
	bx := hx + 0.6*turnX + push[0]/14 + along*hx + across*ax
	by := hy + 0.6*turnY + push[1]/14 + along*hy + across*ay
	if n := hypot(bx, by); n > 0 {
		bx, by = bx/n, by/n
	}

	still := f.gusts(curledX, curledY)
	field := still + push[2]
	// Light begins at once on the side a gust heads for and thins out
	// behind it, so that a still frame shows which way the wind goes.
	const level = 0.02 + (0.82-0.02)*lull
	const band = 0.26 + (0.03-0.26)*edge
	blowing := field > level-band-0.55*tail
	var strength, streaks float32
	if blowing || warm > 0 {
		// Within a gust, thin quick streaks, as wind draws on water.
		thread := f.value((curledX-f.carried*1.6)/140, curledY/30, f.time*0.16, 0x57)
		streaks = 1 - smoothstep(0, 0.5, abs(thread))
	}
	if blowing {
		ahead := f.gusts(curledX+56, curledY)
		leaving := smoothstep(-0.05, 0.07, ahead-still)
		lit := smoothstep(level-band-0.55*tail*leaving, level+band, field)
		broad := lit * lit * mix(0.45, 1, min(max((field-level)/0.45, 0), 1)) * (0.82 + 0.18*fineY)
		strength = broad * mix(1, 0.25+0.95*streaks, streak)
	}
	if warm > 0 {
		// Air the hand warmed shows, wind or no wind, in its own fine grain.
		grain := 0.55*fineY + 0.45*tinyX + 0.3*(streaks-0.4)
		strength = 1 - (1-strength)*(1-0.85*warm*smoothstep(-0.5, 0.25, grain))
	}

	// Thinner behind the content.
	ex, ey := (x-0.5*f.paneW)/calmX, (y-cardY*f.paneH)/calmY
	calm := 1 - (1-calmLeft)*exp(-0.9*(ex*ex+ey*ey))
	// What the hand does shows at once, however little of the wind came in.
	shown := max(f.reveal, min(1, 2*push[3]+warm))
	px[0] = byteOf(strength * calm * shown)
	px[1] = byteOf(bx*0.5 + 0.5)
	px[2] = byteOf(by*0.5 + 0.5)
	px[3] = byteOf(calm * shown)
}

// byteOf returns v, from 0 to 1, as a byte of a texture.
func byteOf(v float32) uint8 {
	return uint8(min(max(v, 0), 1)*255 + 0.5)
}

// compute computes the samples of rows from to to.
func (f *field) compute(from, to int) {
	for j := from; j < to; j++ {
		y := f.originY + (float32(j)+0.5)*f.stepY
		for i := range f.cols {
			x := f.originX + (float32(i)+0.5)*f.stepX
			f.sample(x, y, f.pix[4*(j*f.cols+i):][:4])
		}
	}
}

// at returns the texel (i, j), clamped to the field's edges.
func (f *field) at(i, j int) []uint8 {
	i = min(max(i, 0), f.cols-1)
	j = min(max(j, 0), f.rows-1)
	return f.pix[4*(j*f.cols+i):][:4]
}
