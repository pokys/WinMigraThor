package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Colors
	colorPrimary  = lipgloss.AdaptiveColor{Light: "#5C5C5C", Dark: "#AAAAAA"}
	colorAccent   = lipgloss.AdaptiveColor{Light: "#0073CF", Dark: "#5AB4FF"}
	colorSuccess  = lipgloss.AdaptiveColor{Light: "#007700", Dark: "#4EC94E"}
	colorWarning  = lipgloss.AdaptiveColor{Light: "#CC7700", Dark: "#FFB800"}
	colorError    = lipgloss.AdaptiveColor{Light: "#CC0000", Dark: "#FF5555"}
	colorMuted    = lipgloss.AdaptiveColor{Light: "#888888", Dark: "#666666"}
	colorBorder   = lipgloss.AdaptiveColor{Light: "#CCCCCC", Dark: "#444444"}
	colorSelected = lipgloss.AdaptiveColor{Light: "#005F87", Dark: "#5AB4FF"}

	// Base styles
	StyleBase = lipgloss.NewStyle()

	StyleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Padding(0, 1)

	StyleBreadcrumb = lipgloss.NewStyle().
			Foreground(colorMuted)

	StyleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	StylePanel = StyleBorder.
			Padding(1, 2)

	StyleFooter = lipgloss.NewStyle().
			Foreground(colorMuted).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	StyleFocused = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	StyleSelected = lipgloss.NewStyle().
			Foreground(colorSelected)

	StyleMuted = lipgloss.NewStyle().
			Foreground(colorMuted)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(colorSuccess)

	StyleWarning = lipgloss.NewStyle().
			Foreground(colorWarning)

	StyleError = lipgloss.NewStyle().
			Foreground(colorError)

	StyleSizeHint = lipgloss.NewStyle().
			Foreground(colorMuted).
			Align(lipgloss.Right)

	// Progress bar colors
	StyleProgressFull  = lipgloss.NewStyle().Foreground(colorAccent)
	StyleProgressEmpty = lipgloss.NewStyle().Foreground(colorMuted)

	// Marker for focused menu item
	MarkerFocused  = "›"
	MarkerSelected = "[✔]"
	MarkerPartial  = "[~]"
	MarkerEmpty    = "[ ]"
	RadioSelected  = "(●)"
	RadioEmpty     = "( )"

	IconSuccess = "✔"
	IconWarning = "⚠"
	IconError   = "✘"
	IconWaiting = "…"
)

// StatusIcon returns the appropriate icon for a status string.
func StatusIcon(status string) string {
	switch status {
	case "success":
		return StyleSuccess.Render(IconSuccess)
	case "warning":
		return StyleWarning.Render(IconWarning)
	case "error":
		return StyleError.Render(IconError)
	default:
		return StyleMuted.Render(IconWaiting)
	}
}

// FormatSize returns a human-readable size string.
func FormatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes < KB:
		return fmt.Sprintf("%d B", bytes)
	case bytes < MB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	case bytes < GB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	default:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	}
}
