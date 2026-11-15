package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// editorCommand construye la orden para abrir file en la línea dada con el
// editor indicado (normalmente $VISUAL o $EDITOR; puede incluir argumentos).
// Cada editor recibe la línea a su manera:
//
//	vim, nvim, vi, nano, emacs, micro, kak…  +N archivo
//	code, codium, cursor                      --wait -g archivo:N
//	hx                                        archivo:N
//	subl                                      --wait archivo:N
func editorCommand(editor, file string, line int) []string {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"vi"}
	}
	switch strings.TrimSuffix(filepath.Base(parts[0]), ".exe") {
	case "code", "codium", "code-insiders", "cursor":
		return append(parts, "--wait", "-g", fmt.Sprintf("%s:%d", file, line))
	case "hx", "helix":
		return append(parts, fmt.Sprintf("%s:%d", file, line))
	case "subl", "sublime_text":
		return append(parts, "--wait", fmt.Sprintf("%s:%d", file, line))
	}
	return append(parts, fmt.Sprintf("+%d", line), file)
}

// defaultEditor elige el editor del entorno.
func defaultEditor() string {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := strings.TrimSpace(os.Getenv(v)); e != "" {
			return e
		}
	}
	return "vi"
}
