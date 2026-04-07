package ui

import (
	"fmt"
	"strings"

)

const progressWidth = 32

// ProgressBar renders a text-based progress bar.
type ProgressBar struct {
	Percent     float64
	Label       string
	CurrentFile string
	Status      string // waiting, running, done, warning, error
	Width       int
}

func NewProgressBar(label string) ProgressBar {
	return ProgressBar{
		Label:  label,
		Width:  progressWidth,
		Status: "waiting",
	}
}

func (p ProgressBar) View() string {
	var sb strings.Builder

	// Status label
	statusStr := ""
	switch p.Status {
	case "waiting":
		statusStr = StyleMuted.Render("Waiting...")
	case "running":
		statusStr = StyleFocused.Render(fmt.Sprintf("%.0f%%", p.Percent*100))
	case "done":
		statusStr = StyleSuccess.Render("Done " + IconSuccess)
	case "warning":
		statusStr = StyleWarning.Render("Done " + IconWarning)
	case "error":
		statusStr = StyleError.Render("Failed " + IconError)
	}

	sb.WriteString(fmt.Sprintf("  %s  %s\n", p.renderBar(), statusStr))

	if p.CurrentFile != "" && p.Status == "running" {
		truncated := truncate(p.CurrentFile, 50)
		sb.WriteString("  " + StyleMuted.Render("  "+truncated) + "\n")
	}

	return sb.String()
}

func (p ProgressBar) renderBar() string {
	if p.Width <= 0 {
		return ""
	}
	filled := int(p.Percent * float64(p.Width))
	if filled > p.Width {
		filled = p.Width
	}
	if filled < 0 {
		filled = 0
	}
	empty := p.Width - filled

	bar := StyleProgressFull.Render(strings.Repeat("█", filled)) +
		StyleProgressEmpty.Render(strings.Repeat("░", empty))
	return bar
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max+3:]
}

// JobProgressRow renders a single job's progress in the execution view.
type JobProgressRow struct {
	Name    string
	Index   int
	Total   int
	Bar     ProgressBar
}

func (r JobProgressRow) View() string {
	label := fmt.Sprintf("[%d/%d] %s", r.Index, r.Total, r.Name)
	return fmt.Sprintf("  %s\n%s", StyleTitle.Render(label), r.Bar.View())
}
