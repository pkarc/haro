package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run abre la interfaz a pantalla completa y bloquea hasta que el usuario sale.
func Run(opts Options) error {
	p := tea.NewProgram(New(opts), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
