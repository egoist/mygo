package shaderir_test

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/shader"
	"github.com/egoist/mygo/internal/shaderir"
)

// TestTypeShapes checks what the types say of themselves: how many words
// they take, the vector of a scalar, how many parts a vector has, the
// size of a matrix, and whether a type is an integer one.
func TestTypeShapes(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  shaderir.Type
		want int
	}{
		{"bool", shaderir.Type{Main: shaderir.Bool}, 1},
		{"float", shaderir.Type{Main: shaderir.Float}, 1},
		{"int", shaderir.Type{Main: shaderir.Int}, 1},
		{"uint", shaderir.Type{Main: shaderir.Uint}, 1},
		{"vec2", shaderir.Type{Main: shaderir.Vec2}, 2},
		{"vec3", shaderir.Type{Main: shaderir.Vec3}, 3},
		{"vec4", shaderir.Type{Main: shaderir.Vec4}, 4},
		{"ivec2", shaderir.Type{Main: shaderir.IVec2}, 2},
		{"ivec3", shaderir.Type{Main: shaderir.IVec3}, 3},
		{"ivec4", shaderir.Type{Main: shaderir.IVec4}, 4},
		{"uvec2", shaderir.Type{Main: shaderir.UVec2}, 2},
		{"uvec3", shaderir.Type{Main: shaderir.UVec3}, 3},
		{"uvec4", shaderir.Type{Main: shaderir.UVec4}, 4},
		{"mat2", shaderir.Type{Main: shaderir.Mat2}, 4},
		{"mat3", shaderir.Type{Main: shaderir.Mat3}, 9},
		{"mat4", shaderir.Type{Main: shaderir.Mat4}, 16},
		{"array", shaderir.Type{Main: shaderir.Array, Length: 3, Sub: []shaderir.Type{{Main: shaderir.Vec2}}}, 6},
		{"struct", shaderir.Type{Main: shaderir.Struct, Sub: []shaderir.Type{{Main: shaderir.Float}}}, -1},
	} {
		if got := c.typ.DwordCount(); got != c.want {
			t.Errorf("%s takes %d words, want %d", c.name, got, c.want)
		}
	}

	for _, c := range []struct {
		scalar shaderir.BasicType
		n      int
		want   shaderir.BasicType
	}{
		{shaderir.Float, 1, shaderir.Float},
		{shaderir.Float, 2, shaderir.Vec2},
		{shaderir.Float, 3, shaderir.Vec3},
		{shaderir.Float, 4, shaderir.Vec4},
		{shaderir.Int, 3, shaderir.IVec3},
		{shaderir.Uint, 4, shaderir.UVec4},
		{shaderir.Bool, 2, shaderir.None},
		{shaderir.Float, 5, shaderir.None},
	} {
		if got := shaderir.VectorOf(c.scalar, c.n); got != c.want {
			t.Errorf("VectorOf(%d, %d) = %d, want %d", c.scalar, c.n, got, c.want)
		}
	}

	for _, c := range []struct {
		name string
		typ  shaderir.Type
		want int
	}{
		{"vec2", shaderir.Type{Main: shaderir.Vec2}, 2},
		{"vec3", shaderir.Type{Main: shaderir.Vec3}, 3},
		{"vec4", shaderir.Type{Main: shaderir.Vec4}, 4},
		{"ivec2", shaderir.Type{Main: shaderir.IVec2}, 2},
		{"ivec3", shaderir.Type{Main: shaderir.IVec3}, 3},
		{"ivec4", shaderir.Type{Main: shaderir.IVec4}, 4},
		{"uvec2", shaderir.Type{Main: shaderir.UVec2}, 2},
		{"uvec3", shaderir.Type{Main: shaderir.UVec3}, 3},
		{"uvec4", shaderir.Type{Main: shaderir.UVec4}, 4},
		{"float", shaderir.Type{Main: shaderir.Float}, -1},
	} {
		if got := c.typ.VectorElementCount(); got != c.want {
			t.Errorf("%s has %d elements, want %d", c.name, got, c.want)
		}
	}

	for _, c := range []struct {
		name string
		typ  shaderir.Type
		want int
	}{
		{"mat2", shaderir.Type{Main: shaderir.Mat2}, 2},
		{"mat3", shaderir.Type{Main: shaderir.Mat3}, 3},
		{"mat4", shaderir.Type{Main: shaderir.Mat4}, 4},
		{"vec4", shaderir.Type{Main: shaderir.Vec4}, -1},
	} {
		if got := c.typ.MatrixSize(); got != c.want {
			t.Errorf("%s is a matrix of %d, want %d", c.name, got, c.want)
		}
	}

	for _, c := range []struct {
		name string
		typ  shaderir.Type
		ints bool
	}{
		{"ivec4", shaderir.Type{Main: shaderir.IVec4}, true},
		{"uvec2", shaderir.Type{Main: shaderir.UVec2}, true},
		{"int", shaderir.Type{Main: shaderir.Int}, true},
		{"uint", shaderir.Type{Main: shaderir.Uint}, true},
		{"vec2", shaderir.Type{Main: shaderir.Vec2}, false},
		{"float", shaderir.Type{Main: shaderir.Float}, false},
		{"bool", shaderir.Type{Main: shaderir.Bool}, false},
	} {
		if got := c.typ.IsIntegerVector(); got != (c.typ.IsIntVector() || c.typ.IsUintVector()) {
			t.Errorf("%s: IsIntegerVector disagrees with IsIntVector and IsUintVector", c.name)
		}
		if got := c.typ.IsInteger(); got != c.ints {
			t.Errorf("%s is an integer type: %v, want %v", c.name, got, c.ints)
		}
	}
}

// TestSourceID checks that a source's ID says it again: the round trip
// through its base32, spelled lower, read back in either case, and a
// string that is not one is refused.
func TestSourceID(t *testing.T) {
	var id shaderir.SourceID
	for i := range id {
		id[i] = byte(i*17 + 3)
	}
	str := id.String()
	if str != strings.ToLower(str) {
		t.Errorf("SourceID.String = %q, want it spelled lower", str)
	}
	got, err := shaderir.ParseSourceID(str)
	if err != nil || got != id {
		t.Errorf("ParseSourceID of %q = %v, %v", str, got, err)
	}
	if got, err := shaderir.ParseSourceID(strings.ToUpper(str)); err != nil || got != id {
		t.Errorf("ParseSourceID of %q = %v, %v", strings.ToUpper(str), got, err)
	}
	// A base32 of another length, and one with digits, which the alphabet
	// has none of, are refused.
	if _, err := shaderir.ParseSourceID("MF"); err == nil {
		t.Error("a source ID of two letters was taken")
	}
	if _, err := shaderir.ParseSourceID("0189"); err == nil {
		t.Error("a source ID with digits was taken")
	}
}

// TestParseBuiltinFunc checks the names of the built-in functions: the
// ones the language knows, and none other.
func TestParseBuiltinFunc(t *testing.T) {
	for name, want := range map[string]shaderir.BuiltinFunc{
		"sin":       shaderir.Sin,
		"vec4":      shaderir.Vec4F,
		"discard":   shaderir.DiscardF,
		"__texelAt": shaderir.TexelAt,
	} {
		got, ok := shaderir.ParseBuiltinFunc(name)
		if !ok || got != want {
			t.Errorf("ParseBuiltinFunc(%q) = %q, %v", name, got, ok)
		}
	}
	if got, ok := shaderir.ParseBuiltinFunc("noSuchFunc"); ok || got != "" {
		t.Errorf("ParseBuiltinFunc of a function that is not built in = %q, %v", got, ok)
	}
}

// TestSourceSwizzling checks the swizzle a source wrote: in the xyzw set
// it stays as written, and in the others it is said in the set's names,
// the letters that are not of a vector staying as they are.
func TestSourceSwizzling(t *testing.T) {
	for _, c := range []struct {
		set           shaderir.SwizzlingSet
		swizzle, want string
	}{
		{shaderir.SwizzlingSetXYZW, "xyzw", "xyzw"},
		{shaderir.SwizzlingSetXYZW, "rgb", "rgb"},
		{shaderir.SwizzlingSetRGBA, "xyzw", "rgba"},
		{shaderir.SwizzlingSetRGBA, "wx", "ar"},
		{shaderir.SwizzlingSetRGBA, "wzq", "abq"},
		{shaderir.SwizzlingSetSTPQ, "xyz", "stp"},
		{shaderir.SwizzlingSetSTPQ, "xy", "st"},
	} {
		e := shaderir.Expr{Type: shaderir.SwizzlingExpr, Swizzling: c.swizzle, SwizzlingSet: c.set}
		if got := e.SourceSwizzling(); got != c.want {
			t.Errorf("the swizzle %q of set %d = %q, want %q", c.swizzle, c.set, got, c.want)
		}
	}
}

// TestFilterUniformVariables checks that the uniforms nothing reaches are
// filtered out, so that more commands can be merged: the ones the entry
// points reach stay, and the ones only a function that is never called
// reaches go.
func TestFilterUniformVariables(t *testing.T) {
	src := `package main

var InVertex float
var InFragment float
var InBoth float
var Unused float
var OnlyInHelper float

func helper() float {
	return OnlyInHelper
}

func Vertex(position vec2) vec4 {
	return vec4(position, 0, InVertex + InBoth)
}

func Fragment() vec4 {
	return vec4(InFragment + InBoth)
}`
	ir, err := shader.Compile([]byte(src), "Vertex", "Fragment", 0)
	if err != nil {
		t.Fatal(err)
	}
	uniforms := make([]uint32, len(ir.Uniforms))
	for i := range uniforms {
		uniforms[i] = 5
	}
	ir.FilterUniformVariables(uniforms)

	want := map[string]uint32{
		"InVertex":     5,
		"InFragment":   5,
		"InBoth":       5,
		"Unused":       0,
		"OnlyInHelper": 0,
	}
	if len(ir.UniformNames) != len(want) {
		t.Fatalf("the program has the uniforms %q", ir.UniformNames)
	}
	for i, name := range ir.UniformNames {
		if uniforms[i] != want[name] {
			t.Errorf("the uniform %s was left %d, want %d", name, uniforms[i], want[name])
		}
	}
}
