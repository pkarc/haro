package tui

import "github.com/pkarc/haro/core"

// Interface texts. English is the source and the fallback; Spanish is the
// alternative. Each visible string is written as a pair (en, es) at the point
// of use, so both versions stay side by side.

// pick returns the Spanish text for core.LangES and English for anything else.
func pick(lang core.Lang, en, es string) string {
	if lang == core.LangES {
		return es
	}
	return en
}

// t picks the text in the model's language.
func (m Model) t(en, es string) string { return pick(m.opts.Lang, en, es) }

// t picks the text in the styles' language (styles carry it so that the
// render helpers can translate without a Model).
func (s styles) t(en, es string) string { return pick(s.lang, en, es) }
