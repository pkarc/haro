package main

import (
	"testing"

	"github.com/pkarc/haro/core"
)

func TestResolveLang(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	for in, want := range map[string]core.Lang{"": core.LangEN, "es": core.LangES, "en": core.LangEN, "es_MX.UTF-8": core.LangES} {
		got, err := resolveLang(in)
		if err != nil || got != want {
			t.Errorf("resolveLang(%q) = %q, %v; quería %q", in, got, err, want)
		}
	}
	if _, err := resolveLang("fr"); err == nil {
		t.Error("un idioma desconocido debe ser error")
	}
	t.Setenv("LANG", "")
	if got, _ := resolveLang(""); got != core.LangES {
		t.Errorf("sin entorno debe ser español, fue %q", got)
	}
}
