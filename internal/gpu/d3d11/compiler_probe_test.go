//go:build windows

package d3d11

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/gpu/d3d11/internal/compiler"
)

func probeSource(variant string) string {
	s := shaderSource
	switch variant {
	case "vertex-only":
		return s[:strings.Index(s, "// The color and")]
	case "typed-texture":
		return strings.ReplaceAll(s, "sampleLocal(Texture2D t,", "sampleLocal(Texture2D<float4> t,")
	case "no-texture-parameter":
		start, end := strings.Index(s, "float4 sampleLocal("), strings.Index(s, "float4 premul(")
		s = s[:start] + s[end:]
		for _, tex := range []string{"maskTex", "colorTex", "imageTex"} {
			for _, affine := range []string{"i.transform0.w", "1"} {
				s = strings.ReplaceAll(s, "sampleLocal("+tex+", i.tex, i.widths, "+affine+")", tex+".Sample(samp, i.tex)")
			}
		}
		return s
	case "no-coverage-loop":
		start, end := strings.Index(s, "float localCoverage("), strings.Index(s, "float clipCoverage(")
		return s[:start] + "float localCoverage(float2 p, float4 rect, float4 radii, float affine) { return rectCoverage(p, rect, radii); }\n" + s[end:]
	case "loop-braces":
		s = strings.ReplaceAll(s, "[loop] for (int y = 0; y < 4; y++)[loop] for (int x = 0; x < 4; x++) {", "[loop] for (int y = 0; y < 4; y++) { [loop] for (int x = 0; x < 4; x++) {")
		return strings.ReplaceAll(s, "return n / 16;", "} return n / 16;")
	case "flat-loop":
		return strings.ReplaceAll(s, "[loop] for (int y = 0; y < 4; y++)[loop] for (int x = 0; x < 4; x++) {", "[loop] for (int tap = 0; tap < 16; tap++) { int x = tap & 3; int y = tap >> 2;")
	case "unrolled-loop":
		return strings.ReplaceAll(s, "[loop] for (int y = 0; y < 4; y++)[loop] for (int x = 0; x < 4; x++) {", "[unroll] for (int y = 0; y < 4; y++)[unroll] for (int x = 0; x < 4; x++) {")
	case "explicit-samples":
		start := strings.Index(s, "[loop] for (int y = 0; y < 4; y++)")
		end := strings.Index(s[start:], "return n / 16;") + start
		var body strings.Builder
		for y := range 4 {
			for x := range 4 {
				fmt.Fprintf(&body, "n += sdRoundRect(p + dx * ((%d.5) / 4 - 0.5) + dy * ((%d.5) / 4 - 0.5), rect, radii) <= 0 ? 1 : 0;\n", x, y)
			}
		}
		return s[:start] + body.String() + s[end:]
	case "analytic-derivatives":
		s = strings.ReplaceAll(s, "float localCoverage(float2 p, float4 rect, float4 radii, float affine)", "float localCoverage(float2 p, float4 rect, float4 radii, float4 transform0, float4 transform1)")
		s = strings.ReplaceAll(s, "if (affine < 0.5)", "if (transform0.w < 0.5)")
		s = strings.ReplaceAll(s, "float2 dx = ddx(p), dy = ddy(p), a = (dx + dy) * 0.5, b = (dx - dy) * 0.5;", "float det = transform0.x * transform1.y - transform0.y * transform1.x; float2 dx = float2(transform1.y, -transform1.x) / det, dy = float2(-transform0.y, transform0.x) / det; float2 a = (dx + dy) * 0.5, b = (dx - dy) * 0.5;")
		return regexp.MustCompile(`localCoverage\(([^;{}]*?), (?:i\.transform0\.w|1)\)`).ReplaceAllString(s, `localCoverage($1, i.transform0, i.transform1)`)
	case "no-early-return":
		return strings.ReplaceAll(s, "if (d < -reach)\n\t\treturn 1;\n\tif (d > reach)\n\t\treturn 0;", "")
	case "fastopt":
		s = strings.ReplaceAll(s, "[loop] for (int y = 0; y < 4; y++)[loop] for (int x = 0; x < 4; x++) {", "[loop] [fastopt] for (int y = 0; y < 4; y++) { [loop] [fastopt] for (int x = 0; x < 4; x++) {")
		return strings.ReplaceAll(s, "return n / 16;", "} return n / 16;")
	case "fastopt-flat":
		return strings.ReplaceAll(s, "[loop] for (int y = 0; y < 4; y++)[loop] for (int x = 0; x < 4; x++) {", "[loop] [fastopt] for (int tap = 0; tap < 16; tap++) { int x = tap & 3; int y = tap >> 2;")
	case "branchless-radii", "constant-radii", "branchless-samples", "branchless-all":
		if variant == "branchless-radii" || variant == "branchless-all" {
			s = strings.ReplaceAll(s, "float r = q.x < 0 ? (q.y < 0 ? radii.x : radii.w) : (q.y < 0 ? radii.y : radii.z);", "float2 side = step(float2(0, 0), q); float r = dot(radii, float4((1-side.x)*(1-side.y), side.x*(1-side.y), side.x*side.y, (1-side.x)*side.y));")
		}
		if variant == "constant-radii" {
			s = strings.ReplaceAll(s, "float r = q.x < 0 ? (q.y < 0 ? radii.x : radii.w) : (q.y < 0 ? radii.y : radii.z);", "float r = radii.x;")
		}
		if variant == "branchless-samples" || variant == "branchless-all" {
			s = strings.ReplaceAll(s, "n += sdRoundRect(q, rect, radii) <= 0 ? 1 : 0;", "n += step(sdRoundRect(q, rect, radii), 0);")
		}
		return s
	}
	return s
}

func TestMain(m *testing.M) {
	if variant := os.Getenv("MYGO_COMPILER_PROBE"); variant != "" {
		compiler.StackReserve = 2 << 20
		switch variant {
		case "optimization-1":
			compiler.Optimization = 0
		case "optimization-2":
			compiler.Optimization = 3 << 14
		case "optimization-0":
			compiler.Optimization = 1 << 14
		case "skip-optimization":
			compiler.Optimization = 1 << 2
		case "prefer-flow":
			compiler.Optimization = 1<<15 | 1<<10
		case "ieee-strict":
			compiler.Optimization = 1<<15 | 1<<13
		}
		_, err := compiler.Compile(probeSource(variant), "ps", "ps_4_0")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("compiled")
		os.Exit(0)
	}
	for _, variant := range []string{"fastopt", "fastopt-flat", "prefer-flow", "ieee-strict"} {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "MYGO_COMPILER_PROBE="+variant)
		out, err := cmd.CombinedOutput()
		first, _, _ := strings.Cut(string(out), "\n")
		fmt.Printf("HLSL probe %s: %v %s\n", variant, err, first)
	}
	compiler.Optimization = 1 << 2
	compiler.StackReserve = 2 << 20
	os.Exit(m.Run())
}
