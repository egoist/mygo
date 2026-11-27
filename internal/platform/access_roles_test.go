package platform

import "testing"

// TestAccessRoleQuestions checks the questions an element's role answers:
// whether it holds rows of cells, whether it has a value in a range, and
// how what changes in it is read.
func TestAccessRoleQuestions(t *testing.T) {
	for _, r := range []AccessRole{RoleTable, RoleGrid, RoleTreeGrid} {
		if !r.Tabular() {
			t.Errorf("%d holds rows of cells, but Tabular says no", r)
		}
	}
	for _, r := range []AccessRole{RoleButton, RoleSlider, RoleLog} {
		if r.Tabular() {
			t.Errorf("%d holds no rows of cells, but Tabular says so", r)
		}
	}
	for _, r := range []AccessRole{RoleSlider, RoleProgress, RoleMeter, RoleStepper} {
		if !r.Ranged() {
			t.Errorf("%d has a value in a range, but Ranged says no", r)
		}
	}
	for _, r := range []AccessRole{RoleButton, RoleTable, RoleStatus} {
		if r.Ranged() {
			t.Errorf("%d has no value in a range, but Ranged says so", r)
		}
	}

	// Statuses and logs are read after what is being read, an alert at
	// once; a button is no live region.
	for _, tc := range []struct {
		r      AccessRole
		live   string
		atomic bool
	}{
		{RoleStatus, "polite", true},
		{RoleLog, "polite", false},
		{RoleAlert, "assertive", true},
		{RoleButton, "", false},
	} {
		if live, atomic := tc.r.Live(); live != tc.live || atomic != tc.atomic {
			t.Errorf("Live of %d = (%q, %v), want (%q, %v)", tc.r, live, atomic, tc.live, tc.atomic)
		}
	}
}

// TestAccessPopupString checks the value of aria-haspopup a popup kind is.
func TestAccessPopupString(t *testing.T) {
	for _, tc := range []struct {
		p    AccessPopup
		want string
	}{
		{PopupNone, ""},
		{PopupMenu, "menu"},
		{PopupListBox, "listbox"},
		{PopupTree, "tree"},
		{PopupGrid, "grid"},
		{PopupDialog, "dialog"},
		{AccessPopup(99), ""},
	} {
		if got := tc.p.String(); got != tc.want {
			t.Errorf("String of popup %d = %q, want %q", tc.p, got, tc.want)
		}
	}
}

// TestAccessCurrentString checks the value of aria-current a current item is.
func TestAccessCurrentString(t *testing.T) {
	for _, tc := range []struct {
		c    AccessCurrent
		want string
	}{
		{CurrentNone, ""},
		{CurrentItem, "true"},
		{CurrentPage, "page"},
		{CurrentStep, "step"},
		{CurrentLocation, "location"},
		{CurrentDate, "date"},
		{CurrentTime, "time"},
		{AccessCurrent(99), ""},
	} {
		if got := tc.c.String(); got != tc.want {
			t.Errorf("String of current %d = %q, want %q", tc.c, got, tc.want)
		}
	}
}

// TestAccessAutoCompleteString checks the value of aria-autocomplete the
// suggestions of a text field complete as.
func TestAccessAutoCompleteString(t *testing.T) {
	for _, tc := range []struct {
		a    AccessAutoComplete
		want string
	}{
		{AutoCompleteNone, ""},
		{AutoCompleteInline, "inline"},
		{AutoCompleteList, "list"},
		{AutoCompleteBoth, "both"},
		{AccessAutoComplete(99), ""},
	} {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("String of autocomplete %d = %q, want %q", tc.a, got, tc.want)
		}
	}
}
