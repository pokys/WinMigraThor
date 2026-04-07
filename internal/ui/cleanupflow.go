package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pokys/winmigrathor/internal/cleanup"
	
)

// CleanupScreen handles the cleanup TUI flow.
type CleanupScreen struct {
	items      []cleanup.Item
	scanning   bool
	done       bool
	selectMode bool
	selected   []bool
	cursor     int
	message    string
	width      int
}

func NewCleanupScreen() CleanupScreen {
	return CleanupScreen{scanning: true}
}

// CleanupScannedMsg is received when scanning is complete.
type CleanupScannedMsg struct {
	Items []cleanup.Item
	Err   error
}

// CleanupDoneMsg is received when deletion is complete.
type CleanupDoneMsg struct {
	RemovedCount int
	Err          error
}

func (s CleanupScreen) Init() tea.Cmd {
	return func() tea.Msg {
		items, err := cleanup.Scan()
		return CleanupScannedMsg{Items: items, Err: err}
	}
}

func (s CleanupScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width

	case CleanupScannedMsg:
		s.scanning = false
		if msg.Err != nil {
			s.message = StyleError.Render("Scan error: " + msg.Err.Error())
			return s, nil
		}
		s.items = msg.Items
		s.selected = make([]bool, len(s.items))
		for i := range s.selected {
			s.selected[i] = true
		}

	case CleanupDoneMsg:
		s.done = true
		if msg.Err != nil {
			s.message = StyleError.Render("Cleanup error: " + msg.Err.Error())
		} else {
			s.message = StyleSuccess.Render(fmt.Sprintf("✔ Deleted %d items.", msg.RemovedCount))
		}

	case tea.KeyMsg:
		if s.scanning {
			return s, nil
		}
		if s.done {
			switch msg.String() {
			case "enter", "q", "esc":
				return s, func() tea.Msg { return NavigateMsg{Screen: ScreenMainMenu} }
			}
			return s, nil
		}

		if s.selectMode {
			switch msg.String() {
			case "up", "k":
				if s.cursor > 0 {
					s.cursor--
				}
			case "down", "j":
				if s.cursor < len(s.items)-1 {
					s.cursor++
				}
			case " ":
				if s.cursor < len(s.selected) {
					s.selected[s.cursor] = !s.selected[s.cursor]
				}
			case "enter":
				return s, s.deleteSelected()
			case "esc":
				s.selectMode = false
			}
			return s, nil
		}

		switch msg.String() {
		case "enter":
			return s, s.deleteAll()
		case "s":
			s.selectMode = true
		case "esc", "q":
			return s, func() tea.Msg { return NavigateMsg{Screen: ScreenMainMenu} }
		}
	}
	return s, nil
}

func (s CleanupScreen) deleteAll() tea.Cmd {
	items := s.items
	return func() tea.Msg {
		err := cleanup.Delete(items)
		return CleanupDoneMsg{RemovedCount: len(items), Err: err}
	}
}

func (s CleanupScreen) deleteSelected() tea.Cmd {
	var toDelete []cleanup.Item
	for i, sel := range s.selected {
		if sel && i < len(s.items) {
			toDelete = append(toDelete, s.items[i])
		}
	}
	return func() tea.Msg {
		err := cleanup.Delete(toDelete)
		return CleanupDoneMsg{RemovedCount: len(toDelete), Err: err}
	}
}

func (s CleanupScreen) View() string {
	header := StyleHeader.Render("Cleanup")
	w := s.width - 4
	if w < 55 {
		w = 55
	}

	var body, footer string

	if s.scanning {
		body = "\n  " + StyleMuted.Render("Scanning for temporary files...")
		footer = "Please wait..."
	} else if s.done {
		body = "\n  " + s.message + "\n\n"
		if len(s.items) == 0 {
			body += "  No temporary files remaining.\n"
		}
		footer = "Enter back to menu"
	} else if len(s.items) == 0 {
		body = "\n  " + StyleSuccess.Render("No temporary files found.") + "\n"
		footer = "Enter back to menu    Esc cancel"
	} else {
		body = s.renderItems()
		if s.selectMode {
			footer = "Space toggle  Enter delete selected  Esc back"
		} else {
			footer = "Enter DELETE ALL    S select items    Esc cancel"
		}
	}

	panel := StyleBorder.Width(w).Render(header + "\n" + body)
	footerLine := StyleFooter.Width(w).Render(footer)
	return panel + "\n" + footerLine
}

func (s CleanupScreen) renderItems() string {
	var sb strings.Builder
	sb.WriteString("\n  Found:\n")

	var totalSize int64
	for i, item := range s.items {
		totalSize += item.SizeBytes

		if s.selectMode {
			check := MarkerEmpty
			if i < len(s.selected) && s.selected[i] {
				check = StyleSelected.Render(MarkerSelected)
			}
			cursor := "  "
			if i == s.cursor {
				cursor = StyleFocused.Render("› ")
			}
			sb.WriteString(fmt.Sprintf("  %s%s %-30s %s\n",
				cursor, check, item.Description, FormatSize(item.SizeBytes)))
		} else {
			sb.WriteString(fmt.Sprintf("    • %-30s %s\n", item.Description, FormatSize(item.SizeBytes)))
			sb.WriteString("      " + StyleMuted.Render(item.Path) + "\n")
		}
	}

	sb.WriteString("\n  Total: " + StyleTitle.Render(FormatSize(totalSize)) + "\n")
	sb.WriteString("\n  " + StyleWarning.Render("⚠ This will permanently delete these files.") + "\n")
	return sb.String()
}
