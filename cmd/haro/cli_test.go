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
	if got, _ := resolveLang(""); got != core.LangEN {
		t.Errorf("sin entorno debe ser inglés, fue %q", got)
	}
}

func TestCheckVersion(t *testing.T) {
	for _, ok := range []string{"", "2.8", "v3.0", "2.8.3", "2.10"} {
		if err := checkVersion(ok); err != nil {
			t.Errorf("checkVersion(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"2", "latest", "x.y"} {
		if err := checkVersion(bad); err == nil {
			t.Errorf("checkVersion(%q) debe fallar", bad)
		}
	}
}
