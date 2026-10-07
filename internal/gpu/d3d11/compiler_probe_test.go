//go:build windows

package d3d11

import (
	"fmt"
	"os"
	"os/exec"
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
	}
	return s
}

func TestMain(m *testing.M) {
	if variant := os.Getenv("MYGO_COMPILER_PROBE"); variant != "" {
		_, err := compiler.Compile(probeSource(variant), "vs", "vs_4_0")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("compiled")
		os.Exit(0)
	}
	for _, variant := range []string{"vertex-only", "typed-texture", "no-texture-parameter", "no-coverage-loop", "loop-braces"} {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "MYGO_COMPILER_PROBE="+variant)
		out, err := cmd.CombinedOutput()
		first, _, _ := strings.Cut(string(out), "\n")
		fmt.Printf("HLSL probe %s: %v %s\n", variant, err, first)
	}
	os.Exit(m.Run())
}
