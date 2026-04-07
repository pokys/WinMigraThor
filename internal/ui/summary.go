package ui

import (
	"fmt"
	"strings"
	"time"

)

// BackupPlan holds all data for rendering the summary screen.
type BackupPlan struct {
	Hostname   string
	Date       string
	Target     string
	Compress   bool
	Users      []UserPlanEntry
	Jobs       []JobPlanEntry
	TotalBytes int64
	FreeBytes  int64
}

type UserPlanEntry struct {
	Username  string
	SizeBytes int64
}

type JobPlanEntry struct {
	Name      string
	SizeBytes int64
	Details   []string
}

// RenderSummary renders the backup/restore plan as a string.
func RenderSummary(plan BackupPlan) string {
	var sb strings.Builder

	sb.WriteString(StyleTitle.Render("Backup Plan") + "\n")
	sb.WriteString(strings.Repeat("═", 50) + "\n")

	if plan.Hostname != "" {
		sb.WriteString(fmt.Sprintf("  %-12s %s\n", "Host:", plan.Hostname))
	}
	if plan.Date == "" {
		plan.Date = time.Now().Format("2006-01-02")
	}
	sb.WriteString(fmt.Sprintf("  %-12s %s\n", "Date:", plan.Date))
	if plan.Target != "" {
		sb.WriteString(fmt.Sprintf("  %-12s %s\n", "Target:", plan.Target))
	}
	compress := "No"
	if plan.Compress {
		compress = "Yes"
	}
	sb.WriteString(fmt.Sprintf("  %-12s %s\n", "Compress:", compress))

	if len(plan.Users) > 0 {
		sb.WriteString("\n  Users:\n")
		for _, u := range plan.Users {
			sb.WriteString(fmt.Sprintf("    %s %-30s %s\n",
				StyleSuccess.Render(IconSuccess),
				u.Username,
				FormatSize(u.SizeBytes)))
		}
	}

	if len(plan.Jobs) > 0 {
		sb.WriteString("\n  Data:\n")
		for _, j := range plan.Jobs {
			sb.WriteString(fmt.Sprintf("    %s %-30s %s\n",
				StyleSuccess.Render(IconSuccess),
				j.Name,
				FormatSize(j.SizeBytes)))
			for _, d := range j.Details {
				sb.WriteString("      • " + StyleMuted.Render(d) + "\n")
			}
		}
	}

	sb.WriteString("\n  " + strings.Repeat("─", 48) + "\n")
	sb.WriteString(fmt.Sprintf("  %-30s %s\n", "Estimated total:", StyleTitle.Render(FormatSize(plan.TotalBytes))))

	if plan.FreeBytes > 0 {
		freeStr := FormatSize(plan.FreeBytes)
		status := StyleSuccess.Render("✔")
		if plan.FreeBytes < plan.TotalBytes {
			status = StyleError.Render("✘ Not enough space!")
		}
		sb.WriteString(fmt.Sprintf("  %-30s %s %s\n", "Target free space:", freeStr, status))
	}

	return sb.String()
}
