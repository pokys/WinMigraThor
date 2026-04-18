package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Version info set by main at startup.
var (
	AppVersion   = "dev"
	AppBuildDate = "unknown"
)

// Screen represents which main screen is active.
type Screen int

const (
	ScreenMainMenu Screen = iota
	ScreenBackupWizard
	ScreenRestoreWizard
	ScreenCleanup
	ScreenHelp
	ScreenUpdate
)

// MenuItem is a main menu entry.
type MenuItem struct {
	Label       string
	Description string
	Action      Screen
}

// MainMenuModel is the Bubble Tea model for the main menu.
type MainMenuModel struct {
	items   []MenuItem
	cursor  int
	width   int
	height  int
	quitting bool
}

// NavigateMsg is sent to switch screens.
type NavigateMsg struct {
	Screen Screen
}

// QuitMsg signals the app should quit.
type QuitMsg struct{}

func NewMainMenu() MainMenuModel {
	return MainMenuModel{
		items: []MenuItem{
			{Label: "Backup", Description: "Back up data from this machine", Action: ScreenBackupWizard},
			{Label: "Restore", Description: "Restore data from a backup", Action: ScreenRestoreWizard},
			{Label: "Update", Description: "Download latest version from GitHub", Action: ScreenUpdate},
			{Label: "Help", Description: "Show usage and keybindings", Action: ScreenHelp},
			{Label: "Quit", Description: "Exit MigraThor", Action: -1},
		},
	}
}

func (m MainMenuModel) Init() tea.Cmd {
	return nil
}

func (m MainMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			item := m.items[m.cursor]
			if item.Label == "Quit" {
				return m, tea.Quit
			}
			return m, func() tea.Msg { return NavigateMsg{Screen: item.Action} }
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			return m, func() tea.Msg { return NavigateMsg{Screen: ScreenHelp} }
		}
	}
	return m, nil
}

func (m MainMenuModel) View() string {
	if m.quitting {
		return ""
	}

	width := m.width
	if width < 65 {
		width = 65
	}

	hammerRaw := "" +
		"___________\n" +
		" || __   __ ||\n" +
		" ||_| |_| |_||\n" +
		"      | |\n" +
		"      | |\n" +
		"     _| |_\n" +
		"    |_____|"

	logo := "" +
		" ╔╦╗╦╔═╗╦═╗╔═╗╔╦╗╦ ╦╔═╗╦═╗\n" +
		" ║║║║║ ╦╠╦╝╠═╣ ║ ╠═╣║ ║╠╦╝\n" +
		" ╩ ╩╩╚═╝╩╚═╩ ╩ ╩ ╩ ╩╚═╝╩╚═\n"

	hammerStyled := lipgloss.NewStyle().Foreground(colorMuted).Width(15).Render(hammerRaw)
	logoStyled := StyleAccent().Render(logo)

	versionLine := fmt.Sprintf(" v%s (%s)", AppVersion, AppBuildDate)

	logoBlock := logoStyled + StyleMuted.Render(versionLine)
	combined := lipgloss.JoinHorizontal(lipgloss.Center, logoBlock, "  "+hammerStyled)
	subtitle := "\n  Windows Migration Tool\n"

	var menuLines strings.Builder
	for i, item := range m.items {
		marker := "  "
		labelStyle := StyleBase
		if i == m.cursor {
			marker = StyleFocused.Render(MarkerFocused + " ")
			labelStyle = StyleFocused
		}
		desc := StyleMuted.Render("  " + item.Description)
		line := fmt.Sprintf("   %s%-16s%s", marker, labelStyle.Render(item.Label), desc)
		menuLines.WriteString(line + "\n")
	}

	footer := StyleFooter.Width(width - 4).Render("↑/↓ navigate    Enter select    q quit    ? help")

	body := combined + subtitle + "\n" + menuLines.String() + "\n"

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Width(width - 4).
		Render(body)

	return panel + "\n" + footer
}

// StyleAccent returns the accent style for the logo.
func StyleAccent() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorAccent)
}

// HelpOverlay renders the help screen.
func HelpOverlay() string {
	content := `
  Navigation
    ↑ / ↓  or  j / k     Move up / down
    Enter                  Confirm / proceed
    Esc                    Go back one step
    q                      Quit (with confirmation)

  Selection
    Space                  Toggle item on/off
    a                      Select all
    n                      Select none
    Tab                    Switch simple/advanced mode

  Other
    ?                      Show/hide this help
    L                      View logs (on result screen)

                              Press any key to close`

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Width(56).
		Render(StyleTitle.Render(" Keybindings ") + content)
}
