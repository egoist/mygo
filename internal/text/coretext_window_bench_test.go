//go:build darwin && !ios

package text

import (
	"strconv"
	"strings"
	"testing"
)

const (
	benchEnglish = "The quick brown fox jumps over the lazy dog, while 42 typists in 1987 wrote smooth prose about kerning and type. "
	benchChinese = "长途综合对比卡片需要在一个超长段落里排版，中文没有空格，标点“如此”，数字123和English混排。"
)

// BenchmarkCoreTextWindowParagraph wraps one paragraph of n bytes with
// spans, as a long paste into rich text: English and Chinese in runs of
// bold, italic, monospaced and plain text. windowThreshold of 1<<30 keeps
// the whole paragraph's typesetter, as before windows.
func BenchmarkCoreTextWindowParagraph(b *testing.B) {
	looks := []Span{{Weight: 700}, {Italic: true}, {Family: "Menlo"}, {}}
	for _, n := range []int{50 << 10, 200 << 10, 1 << 20} {
		for _, c := range []struct{ name, text string }{{"english", benchEnglish}, {"chinese", benchChinese}} {
			text := strings.ToValidUTF8(strings.Repeat(c.text, n/len(c.text)+1)[:n], "")
			var spans []Span
			for end, i := 23, 0; end < len([]rune(text))+23; end, i = end+23, i+1 {
				sp := looks[i%len(looks)]
				sp.End = end
				spans = append(spans, sp)
			}
			for _, mode := range []struct {
				name      string
				threshold int
			}{{"windows", windowThreshold}, {"whole", 1 << 30}} {
				if mode.name == "whole" && n > 200<<10 {
					continue
				}
				b.Run(c.name+"/"+sizeName(n)+"/"+mode.name, func(b *testing.B) {
					old := windowThreshold
					windowThreshold = mode.threshold
					defer func() { windowThreshold = old }()
					s := newSystem()
					p := Params{Text: text, Width: 480, Style: Style{Size: 14}, Spans: EncodeSpans(spans)}
					b.ReportAllocs()
					for b.Loop() {
						s.ReleaseShaping()
						s.mu.Lock()
						s.layout(p)
						s.mu.Unlock()
					}
				})
			}
		}
	}
}

// BenchmarkCoreTextRewrap wraps a text without spans at a new width each
// time, as a window being resized does, from the typesetter kept for it,
// once the font's first wrappings (wrapCheckedFirst) were checked.
func BenchmarkCoreTextRewrap(b *testing.B) {
	for _, c := range []struct{ name, text string }{{"english", benchEnglish}, {"chinese", benchChinese}, {"mixed", benchEnglish + benchChinese}} {
		for _, n := range []int{5000, 14000, 50000} {
			text := string([]rune(strings.Repeat(c.text, n/len([]rune(c.text))+1))[:n])
			b.Run(c.name+"/"+strconv.Itoa(n), func(b *testing.B) {
				s := newSystem()
				width := float32(400)
				for range wrapCheckedFirst {
					width++
					s.mu.Lock()
					s.layout(Params{Text: text, Width: width, Style: Style{Size: 14}})
					s.mu.Unlock()
				}
				for b.Loop() {
					width++
					s.mu.Lock()
					s.layout(Params{Text: text, Width: width, Style: Style{Size: 14}})
					s.mu.Unlock()
				}
			})
		}
	}
}

func sizeName(n int) string {
	if n >= 1<<20 {
		return "1MiB"
	}
	return strconv.Itoa(n>>10) + "KiB"
}
