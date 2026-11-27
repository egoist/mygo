package platform

import "testing"

// TestTexts checks the words backends say: the defaults until the app sets
// others, then the others.
func TestTexts(t *testing.T) {
	saved := CurrentTexts()
	t.Cleanup(func() { SetTexts(saved) })

	// Until anything is set, the texts are the English defaults.
	if got := CurrentTexts(); got != DefaultTexts {
		t.Errorf("the texts before anything is set = %+v, want the defaults", got)
	}

	// What the app sets, backends read.
	SetTexts(Texts{OK: "确定", Cancel: "取消", About: "关于\x00"})
	if got := CurrentTexts(); got.OK != "确定" || got.Cancel != "取消" || got.About != "关于\x00" {
		t.Errorf("the texts set = %+v", got)
	}
}
