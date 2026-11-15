// Package tui es la interfaz de terminal de haro (Bubble Tea). Solo presenta:
// toda la lógica de HAProxy viene de core.
package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/pkarc/haro/core"
)

// styles agrupa los estilos de la interfaz. Los colores son adaptativos: cada
// uno tiene variante para terminal clara y oscura.
type styles struct {
	title, dim, bold, code             lipgloss.Style
	err, warn, info, ok                lipgloss.Style
	pane, paneFocus                    lipgloss.Style
	cursor, hl, status, statusKey, tag lipgloss.Style
}

func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

func newStyles() styles {
	border := lipgloss.RoundedBorder()
	return styles{
		title:     lipgloss.NewStyle().Bold(true),
		dim:       lipgloss.NewStyle().Foreground(ac("#6b7280", "#9ca3af")),
		bold:      lipgloss.NewStyle().Bold(true),
		code:      lipgloss.NewStyle().Foreground(ac("#0e7490", "#67e8f9")),
		err:       lipgloss.NewStyle().Foreground(ac("#b91c1c", "#f87171")).Bold(true),
		warn:      lipgloss.NewStyle().Foreground(ac("#a16207", "#fbbf24")).Bold(true),
		info:      lipgloss.NewStyle().Foreground(ac("#1d4ed8", "#60a5fa")),
		ok:        lipgloss.NewStyle().Foreground(ac("#15803d", "#4ade80")),
		pane:      lipgloss.NewStyle().Border(border).BorderForeground(ac("#d1d5db", "#4b5563")),
		paneFocus: lipgloss.NewStyle().Border(border).BorderForeground(ac("#2563eb", "#60a5fa")),
		cursor:    lipgloss.NewStyle().Reverse(true),
		hl:        lipgloss.NewStyle().Background(ac("#fef3c7", "#44403c")),
		status:    lipgloss.NewStyle().Foreground(ac("#374151", "#d1d5db")),
		statusKey: lipgloss.NewStyle().Bold(true),
		tag:       lipgloss.NewStyle().Foreground(ac("#7c3aed", "#c4b5fd")),
	}
}

// sectionColor devuelve el color de un tipo de sección según la base de conocimiento.
func sectionColor(typ string) lipgloss.Style {
	if info, ok := core.KB.Sections[typ]; ok && info.Color != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(info.Color))
	}
	return lipgloss.NewStyle()
}

func (s styles) sev(sev core.Severity) lipgloss.Style {
	switch sev {
	case core.SevError:
		return s.err
	case core.SevWarn:
		return s.warn
	}
	return s.info
}

func sevLabel(sev core.Severity) string {
	switch sev {
	case core.SevError:
		return "error"
	case core.SevWarn:
		return "aviso"
	}
	return "sugerencia"
}

// noColor indica si hay que pintar sin colores: opción --no-color, la
// variable NO_COLOR o un terminal «tonto».
func noColor(flag bool) bool {
	return flag || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
}

func applyColorProfile(disable bool) {
	if noColor(disable) {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}
