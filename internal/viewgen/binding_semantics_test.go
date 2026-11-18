package viewgen

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestHandlerGoScopesRuntime(t *testing.T) {
	for _, tc := range []struct{ name, handler, want string }{
		{"range_assignment", `for _, N = range []int{4,5} { s.Out=N }`, "field=5 global=50 out=5"},
		{"var_initializer", `var N = N+1; s.Out=N`, "field=3 global=50 out=4"},
		{"type_switch", `switch N := any(4).(type) {case int: s.Out=N}; N++`, "field=4 global=50 out=4"},
		{"case_scope", `switch {case true: N:=7; s.Out=N; default: s.Out=N}; N++`, "field=4 global=50 out=7"},
		{"select_scope", `ch:=make(chan int,1); ch<-7; select{case N:=<-ch:s.Out=N}; N++`, "field=4 global=50 out=7"},
		{"named_result", `s.Out=func()(N int){N=7;return}(); N++`, "field=4 global=50 out=7"},
		{"range_declaration", `for _,N:=range []int{4,5}{s.Out=N}; N++`, "field=4 global=50 out=5"},
		{"map_key", `s.Out=map[int]int{N:7}[N]; N++`, "field=4 global=50 out=7"},
		{"struct_key", `s.Out=struct{N int}{N:N}.N; N++`, "field=4 global=50 out=3"},
		{"type_declaration", `{type N int; var x N=7; s.Out=int(x)}; N++`, "field=4 global=50 out=7"},
		{"switch_init", `switch N := 1; N {case 1: _=N}; N++`, "field=4 global=50 out=0"},
		{"receiver_shadow", `s:=7; Out=s; N++`, "field=4 global=50 out=7"},
		{"context_shadow", `c:=7; Out=c; N++`, "field=4 global=50 out=7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeApp(t, map[string]string{
				"app.go": `package main
import u "unsafe"
var _=u.Sizeof(0)
type app struct{N,Out int}
var N=50
`,
				"app.view": `<view type="app"><button on-clicked={` + tc.handler + `}>Go</button></view>`,
				"main.go": `package main
import("fmt";"github.com/egoist/mygo/ui")
func main(){s:=&app{N:3};tt:=ui.NewTester(s.View,200,100);if err:=tt.Click("Go");err!=nil{panic(err)};fmt.Printf("field=%d global=%d out=%d",s.N,N,s.Out)}
`})
			if _, err := Generate(context.Background(), dir, nil); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("run: %v %s", err, out)
			}
			if string(out) != tc.want {
				t.Fatalf("want %s; actual %s", tc.want, out)
			}
		})
	}
}

// TestOffsetofInvalidInMarkup pins the review's decision: the receiver's
// members stand as plain variables while an expression checks, so a
// builtin that asks for a selector by its form — unsafe.Offsetof of a
// bare member — is rejected at its .view place, with no retry machinery.
func TestOffsetofInvalidInMarkup(t *testing.T) {
	dir := writeApp(t, map[string]string{
		"app.go": `package main
import "unsafe"
var _=unsafe.Sizeof(0)
type app struct{N int}
`,
		"app.view": `<view type="app"><text>{unsafe.Offsetof(N)}</text></view>`,
		"main.go": `package main
import "github.com/egoist/mygo/ui"
func main(){_ = ui.View((&app{}).View)}
`})
	_, err := Generate(context.Background(), dir, nil)
	if err == nil || !strings.Contains(err.Error(), "not a selector expression") || !strings.Contains(err.Error(), "app.view:") {
		t.Fatalf("want a .view diagnostic, got %v", err)
	}
}

func TestPromotedParameterNames(t *testing.T) {
	for _, tc := range []struct{ name, decl, view string }{
		{"context", `type state struct{c1 int};type app struct{state;Choice,Out int}`, `<column><radio selected={Choice} value={1}>One</radio><button on-clicked={c:=7; Out=c}>Go</button></column>`},
		{"receiver", `type state struct{s1 *string};type app struct{state}`, `<column><text-input value={s.s1}/><button on-clicked={s:=7; _=s}>Go</button></column>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeApp(t, map[string]string{
				"app.go":   "package main\n" + tc.decl,
				"app.view": `<view type="app">` + tc.view + `</view>`,
				"main.go": `package main
import "github.com/egoist/mygo/ui"
func main(){_ = ui.View((&app{}).View)}
`,
			})
			if _, err := Generate(context.Background(), dir, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPointerBindingsAddressability(t *testing.T) {
	for _, tc := range []struct{ name, decl, view string }{
		{"radio_existing_pointer", `Choice *int`, `<radio selected={Choice} value={1}>One</radio>`},
		{"profile_pointer", `Profile *struct{Name string}`, `<text-input value={Profile.Name}/>`},
		{"slice_index", `Names []string`, `<text-input value={Names[0]}/>`},
		{"generic_value", `Choice int`, `<radio selected={Choice} value={1}>One</radio>`},
		{"generic_pointer_value", `Choice *int; Value *int`, `<radio selected={Choice} value={Value}>One</radio>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeApp(t, map[string]string{"app.go": "package main\ntype app struct{" + tc.decl + "}\n", "app.view": `<view type="app">` + tc.view + `</view>`, "main.go": `package main
import "github.com/egoist/mygo/ui"
func main(){_ = ui.View((&app{}).View)}
`})
			if _, err := Generate(context.Background(), dir, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestImportAliasesWithGeneratedCalls(t *testing.T) {
	for _, tc := range []struct{ name, src, view string }{
		{"fmt_alias", `package main
import f "fmt"
var _=f.Sprint
type app struct{N int}
`, `<button>Go {f.Sprint(N)}</button>`},
		{"ui_alias", `package main
import u "github.com/egoist/mygo/ui"
var _=u.Center
type app struct{N int}
`, `<text text-align={u.Center}>Hello</text>`},
		{"alias_shadow", `package main
import f "fmt"
var _=f.Sprint
type app struct{N int}
`, `<button on-clicked={f:=7; N=f}>Go {f.Sprint(N)}</button>`},
		{"ui_shadow", `package main
type app struct{N int}
`, `<button on-clicked={ui:=7; N=ui}>Go</button>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeApp(t, map[string]string{"app.go": tc.src, "app.view": `<view type="app">` + tc.view + `</view>`, "main.go": `package main
import "github.com/egoist/mygo/ui"
func main(){_ = ui.View((&app{}).View)}
`})
			if _, err := Generate(context.Background(), dir, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
