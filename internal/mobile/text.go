package mobile

import "github.com/egoist/mygo/internal/mobile/wire"

// The text of native UI on Android, which the host's Paint measures and
// draws (internal/text's engine): any goroutine but the UI thread.

func textCall(m message, reply interface{ Decode(*wire.Reader) }) bool {
	b := current.Load()
	if b == nil || b.t == nil {
		return false
	}
	b.t.call(m, reply)
	return true
}

// TextMetrics returns the metrics of a font.
func TextMetrics(family string, size float64, weight int, italic bool) (r wire.TextMetricsReply, ok bool) {
	ok = textCall(&wire.TextMetrics{Family: family, Size: size, Weight: weight, Italic: italic}, &r)
	return r, ok
}

// MeasureText returns how wide each text is in a font.
func MeasureText(family string, size float64, weight int, italic, noKern bool, texts []string) []float64 {
	var r wire.MeasureTextReply
	textCall(&wire.MeasureText{Family: family, Size: size, Weight: weight, Italic: italic, NoKern: noKern, Texts: texts}, &r)
	return r.Widths
}

// TextAdvances returns the advance of each UTF-16 unit of text laid out
// as a whole.
func TextAdvances(family string, size float64, weight int, italic, noKern bool, text string, rtl bool) []float64 {
	var r wire.TextAdvancesReply
	textCall(&wire.TextAdvances{Family: family, Size: size, Weight: weight, Italic: italic, NoKern: noKern, Text: text, RTL: rtl}, &r)
	return r.Advances
}

// DrawText draws text in white, as DrawText's reply tells.
func DrawText(family string, size float64, weight int, italic bool, text string, scale, dx float64) wire.DrawTextReply {
	var r wire.DrawTextReply
	textCall(&wire.DrawText{Family: family, Size: size, Weight: weight, Italic: italic, Text: text, Scale: scale, DX: dx}, &r)
	return r
}

// RegisterFont adds a font file under family, or its own, and returns the
// family.
func RegisterFont(data []byte, family string) (string, error) {
	var r wire.RegisterFontReply
	if !textCall(&wire.RegisterFont{Data: data, Family: family}, &r) {
		return "", errUnsupported
	}
	return r.Family, errorOf(r.Error)
}
