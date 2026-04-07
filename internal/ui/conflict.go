package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ConflictResolution represents a user's choice for a file conflict.
type ConflictResolution struct {
	Strategy string // overwrite, skip, rename
	ApplyAll bool
}

// ConflictResolvedMsg is sent when the user resolves a conflict.
type ConflictResolvedMsg struct {
	Resolution ConflictResolution
}

// ConflictInfo describes a file conflict.
type ConflictInfo struct {
	Path            string
	ExistingSize    int64
	ExistingModTime time.Time
	BackupSize      int64
	BackupModTime   time.Time
}

// ConflictDialog handles per-file conflict resolution.
type ConflictDialog struct {
	Info     ConflictInfo
	options  []string
	cursor   int
	ApplyAll bool
}

func NewConflictDialog(info ConflictInfo) ConflictDialog {
	return ConflictDialog{
		Info:    info,
		options: []string{"overwrite", "skip", "rename"},
		cursor:  0,
	}
}

func (c ConflictDialog) Update(msg tea.Msg) (ConflictDialog, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if c.cursor > 0 {
				c.cursor--
			}
		case "down", "j":
			if c.cursor < len(c.options)-1 {
				c.cursor++
			}
		case " ":
			if c.cursor == len(c.options) {
				c.ApplyAll = !c.ApplyAll
			}
		case "enter":
			strategy := c.options[c.cursor]
			return c, func() tea.Msg {
				return ConflictResolvedMsg{
					Resolution: ConflictResolution{
						Strategy: strategy,
						ApplyAll: c.ApplyAll,
					},
				}
			}
		}
	}
	return c, nil
}

func (c ConflictDialog) View() string {
	var sb strings.Builder

	sb.WriteString(StyleTitle.Render("File Conflict") + "\n\n")
	sb.WriteString(fmt.Sprintf("  File exists: %s\n\n", StyleMuted.Render(c.Info.Path)))
	sb.WriteString(fmt.Sprintf("  Existing:  %s   %s\n",
		FormatSize(c.Info.ExistingSize),
		c.Info.ExistingModTime.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("  Backup:    %s   %s\n\n",
		FormatSize(c.Info.BackupSize),
		c.Info.BackupModTime.Format("2006-01-02 15:04")))

	for i, opt := range c.options {
		radio := RadioEmpty
		if i == c.cursor {
			radio = StyleFocused.Render(RadioSelected)
		}
		label := strings.Title(opt)
		if opt == "rename" {
			label = "Rename (_restored)"
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n", radio, label))
	}

	sb.WriteString("\n")
	applyAllCheck := MarkerEmpty
	if c.ApplyAll {
		applyAllCheck = StyleSelected.Render(MarkerSelected)
	}
	sb.WriteString(fmt.Sprintf("  %s Apply to all remaining conflicts\n\n", applyAllCheck))
	sb.WriteString(StyleMuted.Render("  [Enter] Confirm"))

	return sb.String()
}
