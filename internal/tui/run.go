package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run opens the interface full screen and blocks until the user quits.
func Run(opts Options) error {
	p := tea.NewProgram(New(opts), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
