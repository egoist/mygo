//go:build windows

package d3d11

import (
	_ "embed"
	"strings"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/gpu/d3d11/internal/compiler"
)

//go:embed shader.hlsl
var shaderSource string

// effectSource is the head and the tail of an effect's pixel shader,
// around the line "// effect" (see EffectSource).
//
//go:embed effect.hlsl
var effectSource string

// EffectSource returns the source of an effect's pixel shader: shader.hlsl
// with EFFECT defined, the head of effect.hlsl, the effect's shader (see
// scene.Effect), and the tail, which draws its instances with it
// (effectps). Effects compile it ahead of time with CompileEffect.
func EffectSource(src string) string {
	head, tail, _ := strings.Cut(effectSource, "\n// effect\n")
	return "#define EFFECT\n" + shaderSource + "\n" + head + "\n" + src + "\n" + tail
}

// CompileEffect compiles the pixel shader of an effect whose shader is
// src, with the compiler Windows has, as go generate does for code
// compiled ahead of time.
func CompileEffect(src string) ([]byte, error) {
	return compiler.Compile(EffectSource(src), "effectps", "ps_4_0")
}

// compileShaders makes shaderCode compile shader.hlsl even when the
// bytecode of shaders.go comes from it, for tests.
var compileShaders bool

// shaders is the bytecode of the shader's functions: the instances' vertex
// and pixel shaders, and the passes computing backdrops (see package gpu).
type shaders struct {
	vs, ps, passVS, down, blur []byte
}

// shaderCode returns the bytecode of the shaders: that compiled ahead of
// time into shaders.go, or, when shader.hlsl changed since (go generate
// ./internal/gpu/d3d11 was not run), that compiled now from shader.hlsl,
// with the compiler Windows has.
func shaderCode() (code shaders, err error) {
	if gpu.SourceSum(shaderSource) == shaderSum && !compileShaders {
		return shaders{vertexShader, pixelShader, passVertexShader, downShader, blurShader}, nil
	}
	for _, f := range []struct {
		code          *[]byte
		entry, target string
	}{{&code.vs, "vs", "vs_4_0"}, {&code.ps, "ps", "ps_4_0"}, {&code.passVS, "passvs", "vs_4_0"}, {&code.down, "downps", "ps_4_0"}, {&code.blur, "blurps", "ps_4_0"}} {
		if *f.code, err = compileShader(f.entry, f.target); err != nil {
			return shaders{}, err
		}
	}
	return code, nil
}

// compileShader compiles the function entry of shader.hlsl for target, as
// gen.go does.
func compileShader(entry, target string) ([]byte, error) {
	return compiler.Compile(shaderSource, entry, target)
}
