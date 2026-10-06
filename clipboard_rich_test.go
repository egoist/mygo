package mygo

import "testing"

func TestClipboardRichRepresentations(t *testing.T) {
	Clipboard.WriteRichText("Hello 😀", "<p><b>Hello 😀</b></p>", `{\rtf1\b Hello}`)
	if Clipboard.ReadText() != "Hello 😀" || Clipboard.ReadHTML() != "<p><b>Hello 😀</b></p>" || Clipboard.ReadRTF() != `{\rtf1\b Hello}` {
		t.Fatal("rich clipboard did not retain alternative formats")
	}
	Clipboard.WriteText("plain")
	if Clipboard.ReadHTML() != "" || Clipboard.ReadRTF() != "" {
		t.Fatal("plain write retained stale rich content")
	}
	Clipboard.WriteRichText("clear", "<p>clear</p>", `{\rtf1 clear}`)
	Clipboard.Clear()
	if Clipboard.ReadText() != "" || Clipboard.ReadHTML() != "" || Clipboard.ReadRTF() != "" {
		t.Fatal("clear left rich formats behind")
	}
}
