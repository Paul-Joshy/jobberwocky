package ui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// mdStyle is picked once, before the UI starts: asking the terminal for its
// background colour from inside the running program would block.
var mdStyle = func() string {
	if lipgloss.HasDarkBackground() {
		return "dark"
	}
	return "light"
}()

// renderMarkdown renders fit checks and draft summaries; on any error it falls
// back to plain wrapped text so a document is never lost.
func renderMarkdown(md string, width int) string {
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(mdStyle), glamour.WithWordWrap(max(20, width-2)))
	if err != nil {
		return wrap(md, width)
	}
	out, err := r.Render(md)
	if err != nil {
		return wrap(md, width)
	}
	return out
}
