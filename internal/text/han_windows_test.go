//go:build windows && (amd64 || arm64)

package text

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

var iidIDWriteFontFace5 = guid{0x98eff3a5, 0xb667, 0x479a, [8]byte{0xb1, 0x45, 0xe2, 0xfa, 0x5b, 0x9f, 0xdc, 0x29}}

// faceInfo describes a font face: its family, weight, simulations and the
// values of its axes.
func faceInfo(face uintptr) string {
	f3 := queryInterface(face, &iidIDWriteFontFace3)
	if f3 == 0 {
		return "?"
	}
	defer release(f3)
	var strs uintptr
	family := "?"
	if !failed(call(f3, 40, uintptr(unsafe.Pointer(&strs)))) {
		family = englishName(strs)
		release(strs)
	}
	weight := call(f3, 37)
	sims := call(face, 6)
	axes := ""
	if f5 := queryInterface(face, &iidIDWriteFontFace5); f5 != 0 {
		n := uint32(call(f5, 53))
		if n > 0 {
			vals := make([]struct {
				tag   uint32
				value float32
			}, n)
			call(f5, 54, uintptr(unsafe.Pointer(&vals[0])), uintptr(n))
			for _, v := range vals {
				axes += fmt.Sprintf(" %c%c%c%c=%v", byte(v.tag), byte(v.tag>>8), byte(v.tag>>16), byte(v.tag>>24), v.value)
			}
			axes += fmt.Sprintf(" variations=%v", call(f5, 55) != 0)
		}
		release(f5)
	}
	return fmt.Sprintf("%s w=%d sims=%d%s", family, weight, sims, axes)
}

func preferredUILanguages() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserPreferredUILanguages")
	var num, size uint32
	proc.Call(8, uintptr(unsafe.Pointer(&num)), 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return ""
	}
	buf := make([]uint16, size)
	proc.Call(8, uintptr(unsafe.Pointer(&num)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	var parts []string
	start := 0
	for i, c := range buf {
		if c == 0 {
			if i > start {
				parts = append(parts, syscall.UTF16ToString(buf[start:i]))
			}
			start = i + 1
		}
	}
	return strings.Join(parts, ",")
}

// Diagnostic for #1454: which fonts Chinese text falls back to.
func TestDWriteHanDiagnostic(t *testing.T) {
	s := newSystem()
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.engine().(*dwrite)
	var b strings.Builder
	fmt.Fprintf(&b, "locale %s, preferred UI languages %s\n", syscall.UTF16ToString(e.locale), preferredUILanguages())
	var fams []string
	for _, n := range familyNames(e.system) {
		l := strings.ToLower(n)
		if strings.Contains(l, "yahei") || strings.Contains(l, "gothic") || strings.Contains(l, "sim") || strings.Contains(l, "noto") || strings.Contains(l, "dengxian") || strings.Contains(l, "jhenghei") || strings.Contains(l, "meiryo") || strings.Contains(l, "variable") || strings.Contains(l, "bahnschrift") || strings.Contains(l, "malgun") {
			fams = append(fams, n)
		}
	}
	fmt.Fprintf(&b, "families: %s\n", strings.Join(fams, ", "))
	texts := []string{"我们的音乐播放器正在播放周杰伦的歌曲", "这是一个中文标签，看看字重是否一致。", "设置 播放列表 歌词 关于", "Hello 世界 说话 东西"}
	styles := []Style{{Size: 13}, {Size: 13, Weight: 700}, {Size: 13, Weight: 300}, {Size: 13, Lang: "zh-Hans"}, {Size: 13, Lang: "zh-CN"}, {Size: 13, Lang: "ja"}, {Size: 13, Lang: "en"}}
	for _, style := range styles {
		for _, text := range texts {
			r := []rune(text)
			e.resetScratch()
			fmt.Fprintf(&b, "%q weight %d lang %q:\n", text, style.weight(), style.Lang)
			for _, line := range e.shapeText(r, style, nil, 0, false, false) {
				for _, run := range line.runs {
					fmt.Fprintf(&b, "  %q: %s\n", string(r[run.start:run.end]), faceInfo(run.font.native))
				}
			}
		}
	}
	for _, w := range []int{300, 400, 700} {
		for _, fam := range []string{"Bahnschrift", "Segoe UI Variable Text", "Segoe UI Variable"} {
			style := Style{Size: 40, Family: fam, Weight: w}
			e.resetScratch()
			lines := e.shapeText([]rune("H"), style, nil, 0, false, false)
			if len(lines) == 0 || len(lines[0].runs) == 0 {
				continue
			}
			run := lines[0].runs[0]
			g := e.glyph(run.font, run.glyphs[0].ID, 1, 0, 0, false)
			sum := 0
			for _, p := range g.pix {
				sum += int(p)
			}
			fmt.Fprintf(&b, "%s %d: %s coverage %d\n", fam, w, faceInfo(run.font.native), sum)
		}
	}
	t.Error(b.String())
}
