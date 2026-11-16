package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// editorCommand builds the command to open file at the given line with the
// given editor (normally $VISUAL or $EDITOR; it may include arguments).
// Each editor takes the line its own way:
//
//	vim, nvim, vi, nano, emacs, micro, kak…  +N file
//	code, codium, cursor                      --wait -g file:N
//	hx                                        file:N
//	subl                                      --wait file:N
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

// defaultEditor picks the editor from the environment.
func defaultEditor() string {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := strings.TrimSpace(os.Getenv(v)); e != "" {
			return e
		}
	}
	return "vi"
}
