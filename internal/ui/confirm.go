package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ConfirmMsg is sent when the user confirms or cancels.
type ConfirmMsg struct {
	Confirmed bool
}

// Confirm is a simple Yes/No confirmation component.
type Confirm struct {
	Title   string
	Body    string
	focused bool // true = Yes focused
}

func NewConfirm(title, body string) Confirm {
	return Confirm{Title: title, Body: body, focused: true}
}

func (c Confirm) Update(msg tea.Msg) (Confirm, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch strings.ToLower(msg.String()) {
		case "left", "right", "tab", "h", "l":
			c.focused = !c.focused
		case "enter":
			return c, func() tea.Msg { return ConfirmMsg{Confirmed: c.focused} }
		case "y":
			return c, func() tea.Msg { return ConfirmMsg{Confirmed: true} }
		case "n", "esc":
			return c, func() tea.Msg { return ConfirmMsg{Confirmed: false} }
		}
	}
	return c, nil
}

func (c Confirm) View() string {
	var sb strings.Builder

	sb.WriteString(StyleTitle.Render(c.Title) + "\n\n")
	if c.Body != "" {
		sb.WriteString(c.Body + "\n\n")
	}

	yesStyle := StyleMuted
	noStyle := StyleMuted
	if c.focused {
		yesStyle = StyleFocused
	} else {
		noStyle = StyleFocused
	}

	sb.WriteString(yesStyle.Render("[Y] Yes") + "    " + noStyle.Render("[N] No"))
	return sb.String()
}
