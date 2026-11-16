package core

import "os"

// Lang is a language of the texts the core produces (explanations and
// detector messages). Interface texts do not depend on it.
type Lang string

const (
	LangEN Lang = "en"
	LangES Lang = "es"
)

// LangDefault is the language used when none is requested.
const LangDefault = LangEN

// Langs lists the available languages; the first is the default.
var Langs = []Lang{LangEN, LangES}

// ParseLang understands "es", "en" or a locale such as "en_US.UTF-8" or
// "es-MX". It returns ok=false if the value is empty or unsupported.
func ParseLang(s string) (Lang, bool) {
	if len(s) < 2 {
		return "", false
	}
	if len(s) > 2 && s[2] != '_' && s[2] != '-' && s[2] != '.' && s[2] != '@' {
		return "", false
	}
	for _, l := range Langs {
		if equalFold(s[:2], string(l)) {
			return l, true
		}
	}
	return "", false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// LangFromEnv picks the language from LC_ALL, LC_MESSAGES and LANG (in that
// order): Spanish if the first one set starts with "es", English otherwise,
// including when nothing is set.
func LangFromEnv() Lang {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(name); v != "" {
			if l, ok := ParseLang(v); ok && l == LangES {
				return LangES
			}
			return LangEN
		}
	}
	return LangEN
}

// t picks the text for the language: Spanish for LangES, English otherwise
// (including the empty language). Phrases with arguments are written in full
// in each language because word order changes.
func (l Lang) t(es, en string) string {
	if l == LangES {
		return es
	}
	return en
}
