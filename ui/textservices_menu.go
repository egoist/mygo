package ui

import "github.com/egoist/mygo/internal/platform"

func (ed *editor) textServiceMenu(pm *platform.Menu, labels, names *[]string) map[int]*textServiceAction {
	s := ed.services
	if s == nil || ed.password || ed.readOnly {
		return nil
	}
	actions := map[int]*textServiceAction{}
	add := func(menu *platform.Menu, label string, on bool, typ platform.MenuItemType, name string, edits []textReplacement, word string) *platform.MenuItem {
		id := len(*labels) + 1
		item := &platform.MenuItem{ID: id, Label: label, Enabled: on && ed.compose == "", Visible: true, Type: typ}
		menu.Items = append(menu.Items, item)
		*labels = append(*labels, label)
		*names = append(*names, "")
		if (name != "" || len(edits) > 0) && item.Enabled {
			actions[id] = &textServiceAction{state: s, generation: s.generation, serial: s.serial, version: ed.buf.version, name: name, edits: edits, word: word}
		}
		return item
	}
	add(pm, "", false, platform.MenuItemSeparator, "", nil, "")
	if issue := ed.textIssueAtSelection(); issue != nil {
		item := add(pm, "Spelling Suggestions", len(issue.Replacements) > 0, platform.MenuItemSubmenu, "", nil, "")
		item.Submenu = &platform.Menu{}
		for _, replacement := range issue.Replacements {
			label := replacement
			if label == "" {
				label = "Delete word"
			}
			add(item.Submenu, label, true, platform.MenuItemNormal, "", []textReplacement{{*issue, replacement}}, "")
		}
		if issue.Kind == TextSpelling || issue.Kind == TextCorrection {
			add(pm, "Ignore Spelling", true, platform.MenuItemNormal, "ignore", nil, issue.Original)
			add(pm, "Learn Spelling", s.info.LearnWord, platform.MenuItemNormal, "learn", nil, issue.Original)
		}
		add(pm, "", false, platform.MenuItemSeparator, "", nil, "")
	}
	label := "Check Spelling"
	if s.infoError != nil {
		label = "Spelling Unavailable"
	}
	add(pm, label, s.info.Spelling && s.infoError == nil, platform.MenuItemNormal, "check", nil, "")
	settings := add(pm, "Spelling and Substitutions", true, platform.MenuItemSubmenu, "", nil, "")
	settings.Submenu = &platform.Menu{}
	for _, option := range []struct {
		label, name        string
		available, checked bool
	}{
		{"Check Spelling While Typing", "spelling", s.info.Spelling, s.options.SpellChecking},
		{"Correct Spelling Automatically", "correction", s.info.Correction, s.options.AutomaticCorrection},
		{"Smart Quotes", "quotes", s.info.SmartQuotes, s.options.SmartQuotes},
		{"Smart Dashes", "dashes", s.info.SmartDashes, s.options.SmartDashes},
		{"Text Replacement", "replacement", s.info.TextReplacement, s.options.TextReplacement},
	} {
		it := add(settings.Submenu, option.label, option.available, platform.MenuItemCheckbox, option.name, nil, "")
		it.Checked = option.checked && option.available
	}
	return actions
}

func (ed *editor) textIssueAtSelection() *TextIssue {
	s := ed.services
	if s == nil || s.version != ed.buf.version || ed.compose != "" {
		return nil
	}
	a, b := ed.selection()
	for i := range s.issues {
		issue := &s.issues[i]
		if a != b && a == issue.Start && b == issue.End || a == b && a >= issue.Start && a <= issue.End {
			return issue
		}
	}
	return nil
}
