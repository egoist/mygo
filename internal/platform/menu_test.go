package platform

import "testing"

// TestLabelWithBadge checks the fallback of platforms that draw no trailing
// badge of their own.
func TestLabelWithBadge(t *testing.T) {
	if got := (&MenuItem{Label: "Node"}).LabelWithBadge(); got != "Node" {
		t.Errorf("label without badge = %q", got)
	}
	if got := (&MenuItem{Label: "Node", Badge: "120 ms"}).LabelWithBadge(); got != "Node  120 ms" {
		t.Errorf("label with badge = %q", got)
	}
}
