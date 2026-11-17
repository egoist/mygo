package viewgen

import (
	"testing"
)

// The catalog reads the real ui package the repo builds with (the test
// binary's own dependency), from its export data.
func loadTestAPI(t *testing.T) *API {
	t.Helper()
	api, err := LoadAPI("..")
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func TestAPITags(t *testing.T) {
	api := loadTestAPI(t)
	for tag, want := range map[string]string{
		"column":         "Column",
		"row":            "Row",
		"text":           "Text",
		"text-input":     "TextInput",
		"primary-button": "PrimaryButton",
		"radio":          "Radio",
	} {
		c, ok := api.Constructors[tag]
		if !ok {
			t.Errorf("no constructor for <%s>", tag)
			continue
		}
		if c.GoName != want {
			t.Errorf("<%s> is %s, want %s", tag, c.GoName, want)
		}
		if c.Unsupported != "" {
			t.Errorf("<%s> unsupported: %s", tag, c.Unsupported)
		}
	}
}

func TestAPIConstructorParams(t *testing.T) {
	api := loadTestAPI(t)
	btn := api.Constructors["button"]
	if len(btn.Params) != 1 || btn.Params[0].Name != "label" {
		t.Fatalf("button params: %+v", btn.Params)
	}
	// Radio's selected is a pointer the generator binds with &T.
	radio := api.Constructors["radio"]
	if len(radio.TypeParams) != 1 || radio.TypeParams[0] != "T" {
		t.Fatalf("radio type params: %+v", radio.TypeParams)
	}
	found := false
	for _, p := range radio.Params {
		if p.Name == "selected" {
			found = p.Ptr
		}
	}
	if !found {
		t.Fatalf("radio selected is not a pointer: %+v", radio.Params)
	}
	// A callback-taking constructor is catalogued as unsupported, with
	// its signature, not as an unknown tag.
	for _, tag := range []string{"form", "list", "toolbar"} {
		c, ok := api.Constructors[tag]
		if !ok {
			t.Errorf("no entry for <%s>", tag)
			continue
		}
		if c.Unsupported == "" {
			t.Errorf("<%s> should name what keeps markup from it", tag)
		}
	}
}

func TestAPIMethods(t *testing.T) {
	api := loadTestAPI(t)
	fluent := map[string]string{
		"gap":        "Gap",
		"font-size":  "FontSize",
		"label":      "Label",
		"disabled":   "Disabled",
		"text-color": "TextColor",
	}
	for tag, want := range fluent {
		m, ok := api.Methods[tag]
		if !ok {
			t.Errorf("no method for %s", tag)
			continue
		}
		if m.Kind != MethodFluent || m.GoName != want {
			t.Errorf("%s: %+v", tag, m)
		}
	}
	for _, q := range []string{"clicked", "changed", "submitted", "double-clicked"} {
		m, ok := api.Methods[q]
		if !ok || m.Kind != MethodQuery {
			t.Errorf("query %s: %+v found=%v", q, m, ok)
		}
	}
	if m := api.Methods["bounds"]; m == nil || m.Kind != MethodOther {
		t.Errorf("bounds: %+v", m)
	}
}

func TestKebab(t *testing.T) {
	for goName, tag := range map[string]string{
		"Column":        "column",
		"PrimaryButton": "primary-button",
		"Textf":         "textf",
		"TextArea":      "text-area",
	} {
		if got := Kebab(goName); got != tag {
			t.Errorf("Kebab(%s) = %s, want %s", goName, got, tag)
		}
	}
}
