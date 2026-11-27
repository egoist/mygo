package platform

import "testing"

// TestContentRulesIdentifier checks the name of the list compiled from
// rules: the id and a hash of the rules, which is the same while they are.
func TestContentRulesIdentifier(t *testing.T) {
	a := ContentRulesIdentifier("en", "block ads")
	b := ContentRulesIdentifier("en", "block ads")
	if a != b {
		t.Errorf("the same rules named %q and %q", a, b)
	}
	// The id, a dash, and sixteen hexadecimal digits.
	if len(a) != len("en")+1+16 {
		t.Fatalf("identifier = %q, of %d characters", a, len(a))
	}
	for _, c := range a[3:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			t.Fatalf("identifier = %q, hash not hexadecimal", a)
		}
	}
	// Other rules, or another id, name another list.
	if a == ContentRulesIdentifier("en", "block trackers") {
		t.Error("other rules with the same name")
	}
	if a == ContentRulesIdentifier("zh", "block ads") {
		t.Error("another id with the same name")
	}
}

// TestStaleContentRules checks what says a list was compiled for the id
// from other rules than the current ones: a list of another id, or of no
// rules at all, is not stale, it is not one of this id's.
func TestStaleContentRules(t *testing.T) {
	current := ContentRulesIdentifier("en", "block ads")
	stale := ContentRulesIdentifier("en", "block trackers")

	// Other rules than the current ones.
	if !StaleContentRules("en", current, stale) {
		t.Error("a list of other rules is not stale")
	}
	// The current ones are not.
	if StaleContentRules("en", current, current) {
		t.Error("the current list is stale")
	}
	// A list of another id is not one of this id's lists at all.
	if StaleContentRules("zh", current, stale) {
		t.Error("another id's list is stale")
	}
	// Nor is a name that is no list of rules: no hash, a short one, or
	// one of no hexadecimal digits.
	for _, bad := range []string{"en", "en-", "en-abc", "en-gggggggggggggggg"} {
		if StaleContentRules("en", current, bad) {
			t.Errorf("%q counts as a stale list", bad)
		}
	}
}
