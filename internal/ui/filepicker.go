package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
)

// PathConfirmedMsg is sent when the user submits a path.
type PathConfirmedMsg struct {
	Path string
}

// PathErrorMsg is sent when a path validation fails.
type PathErrorMsg struct {
	Err string
}

// FilePicker is a simple text input for entering a path.
type FilePicker struct {
	input     textinput.Model
	Error     string
	Validated bool
}

func NewFilePicker(placeholder string) FilePicker {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.Width = 50
	return FilePicker{input: ti}
}

func (f FilePicker) Value() string {
	return f.input.Value()
}

func (f FilePicker) Update(msg tea.Msg) (FilePicker, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			path := strings.TrimSpace(f.input.Value())
			if path == "" {
				f.Error = "Path cannot be empty"
				return f, nil
			}
			return f, func() tea.Msg { return PathConfirmedMsg{Path: path} }
		}
	}
	f.input, cmd = f.input.Update(msg)
	return f, cmd
}

func (f FilePicker) View() string {
	var sb strings.Builder
	sb.WriteString("> " + f.input.View() + "\n")
	if f.Error != "" {
		sb.WriteString(StyleError.Render("  ✘ " + f.Error) + "\n")
	}
	return sb.String()
}

// DriveInfo holds information about a drive.
type DriveInfo struct {
	Letter    string
	Label     string
	FreeBytes int64
	TotalBytes int64
	FSType    string
}

// ListDrivesMsg carries drive information.
type ListDrivesMsg struct {
	Drives []DriveInfo
}

// ScanDrives renders detected drive info as a formatted string.
func RenderDrives(drives []DriveInfo) string {
	if len(drives) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n  Available drives:\n")
	for _, d := range drives {
		label := d.Label
		if label == "" {
			label = "Drive"
		}
		sb.WriteString(fmt.Sprintf("    %s:  %-8s  %s free / %s\n",
			d.Letter,
			label,
			FormatSize(d.FreeBytes),
			FormatSize(d.TotalBytes),
		))
	}
	return sb.String()
}

// ValidatePath checks if a path is writable.
func ValidatePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		// Try to create it
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("cannot create directory: %v", err)
		}
		return nil
	}
	if !info.IsDir() {
		return fmt.Errorf("path exists but is not a directory")
	}
	// Test writability
	testFile := strings.TrimRight(path, `/\`) + "/.migrator_test"
	f, err := os.Create(testFile)
	if err != nil {
		return fmt.Errorf("directory is not writable: %v", err)
	}
	f.Close()
	os.Remove(testFile)
	return nil
}
