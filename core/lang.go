package core

import "os"

// Lang es un idioma de los textos que produce el núcleo (explicaciones y
// mensajes del detector). Los textos de interfaz no dependen de él.
type Lang string

const (
	LangES Lang = "es"
	LangEN Lang = "en"
)

// Langs lista los idiomas disponibles; el primero es el valor por defecto.
var Langs = []Lang{LangES, LangEN}

// ParseLang interpreta «es», «en» o un locale como «en_US.UTF-8» o «es-MX».
// Devuelve ok=false si está vacío o no está soportado.
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

// LangFromEnv elige el idioma según LC_ALL, LC_MESSAGES y LANG (en ese
// orden): inglés si empieza por «en» y español en cualquier otro caso,
// incluido si no hay nada definido, porque los textos de interfaz están en español.
func LangFromEnv() Lang {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(name); v != "" {
			if l, ok := ParseLang(v); ok && l == LangEN {
				return LangEN
			}
			return LangES
		}
	}
	return LangES
}

// t elige el texto según el idioma. Las frases con argumentos se escriben
// completas en cada idioma, porque el orden de las palabras cambia.
func (l Lang) t(es, en string) string {
	if l == LangEN {
		return en
	}
	return es
}
