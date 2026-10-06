//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/textcheck"
)

var enchant struct {
	once      sync.Once
	available bool
	init      func() ptr
	free      func(ptr)
	request   func(ptr, *byte) ptr
	freeDict  func(ptr, ptr)
	list      func(ptr, ptr, ptr)
	check     func(ptr, *byte, int) int32
	suggest   func(ptr, *byte, int, *uintptr) ptr
	freeList  func(ptr, ptr)
	add       func(ptr, *byte, int)
	error     func(ptr) ptr
}

var (
	enchantListID ptr
	enchantLists  = map[ptr]*[]string{}
	// One callback, with user data routing synchronous enumerations.
	enchantListCallback = purego.NewCallback(func(tag, name, desc, file, data ptr) {
		if list := enchantLists[data]; list != nil {
			*list = append(*list, textcheck.Language(goStr(tag)))
		}
	})
)

func loadEnchant() bool {
	enchant.once.Do(func() {
		lib, err := open("libenchant-2.so.2", "libenchant-2.so")
		if err != nil {
			return
		}
		enchant.available = bind(lib, &enchant.init, "enchant_broker_init") &&
			bind(lib, &enchant.free, "enchant_broker_free") && bind(lib, &enchant.request, "enchant_broker_request_dict") &&
			bind(lib, &enchant.freeDict, "enchant_broker_free_dict") && bind(lib, &enchant.list, "enchant_broker_list_dicts") &&
			bind(lib, &enchant.check, "enchant_dict_check") && bind(lib, &enchant.suggest, "enchant_dict_suggest") &&
			bind(lib, &enchant.freeList, "enchant_dict_free_string_list") && bind(lib, &enchant.error, "enchant_dict_get_error")
		bind(lib, &enchant.add, "enchant_dict_add")
	})
	return enchant.available
}

type enchantChecker struct {
	info         platform.TextServiceInfo
	broker, dict ptr
}

func (*Backend) NewTextChecker(language string) (platform.TextChecker, error) {
	if !loadEnchant() {
		return nil, platform.ErrUnsupported
	}
	c := &enchantChecker{broker: enchant.init(), info: platform.TextServiceInfo{Provider: "Enchant 2"}}
	if c.broker == 0 {
		return c, platform.ErrUnsupported
	}
	enchantListID++
	enchantLists[enchantListID] = &c.info.Languages
	enchant.list(c.broker, enchantListCallback, enchantListID)
	delete(enchantLists, enchantListID)
	c.info.Language = textcheck.MatchLanguage(language, c.info.Languages)
	if c.info.Language == "" {
		return c, fmt.Errorf("%w: %s", platform.ErrTextLanguageUnavailable, language)
	}
	c.dict = enchant.request(c.broker, cs(strings.ReplaceAll(c.info.Language, "-", "_")))
	if c.dict == 0 {
		return c, fmt.Errorf("%w: %s", platform.ErrTextLanguageUnavailable, language)
	}
	c.info.Spelling, c.info.Suggestions = true, true
	c.info.LearnWord = enchant.add != nil
	return c, nil
}

func (c *enchantChecker) Info() platform.TextServiceInfo { return c.info }
func (c *enchantChecker) Close() {
	if c.dict != 0 {
		enchant.freeDict(c.broker, c.dict)
		c.dict = 0
	}
	if c.broker != 0 {
		enchant.free(c.broker)
		c.broker = 0
	}
}

func (c *enchantChecker) Check(text string, opts platform.TextCheckOptions, done func([]platform.TextIssue, error)) {
	if !opts.Spelling {
		done(nil, nil)
		return
	}
	var issues []platform.TextIssue
	for _, rangeOfWord := range textcheck.Words(text) {
		word := textcheck.Slice(text, rangeOfWord[0], rangeOfWord[1])
		switch result := enchant.check(c.dict, cs(word), len(word)); {
		case result < 0:
			done(nil, errors.New(goStr(enchant.error(c.dict))))
			return
		case result == 0:
			continue
		}
		issue := platform.TextIssue{Start: rangeOfWord[0], End: rangeOfWord[1], Kind: platform.TextSpelling}
		var count uintptr
		list := enchant.suggest(c.dict, cs(word), len(word), &count)
		if list != 0 {
			base := *(*unsafe.Pointer)(unsafe.Pointer(&list))
			for _, s := range unsafe.Slice((*ptr)(base), min(count, 8)) {
				issue.Replacements = append(issue.Replacements, goStr(s))
			}
			enchant.freeList(c.dict, list)
		}
		issues = append(issues, issue)
	}
	done(issues, nil)
}

func (c *enchantChecker) LearnWord(word string) error {
	if enchant.add == nil {
		return platform.ErrUnsupported
	}
	enchant.add(c.dict, cs(word), len(word))
	if err := goStr(enchant.error(c.dict)); err != "" {
		return errors.New(err)
	}
	return nil
}
