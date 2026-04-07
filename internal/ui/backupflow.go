package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/pokys/winmigrathor/internal/jobs"
	"github.com/pokys/winmigrathor/internal/users"
)

// BackupWizardModel is the Bubble Tea model for the full backup wizard.
type BackupWizardModel struct {
	step    BackupStep
	width   int
	height  int

	// Step 1: Users
	userSelector Selector

	// Step 2: Data
	dataSelector  Selector
	advancedMode  bool

	// Step 3: Options
	passwordMode   int // 0=skip, 1=assisted, 2=experimental
	compress       bool
	folderScope    int // 0=standard, 1=custom

	// Step 4: Target
	targetInput  textinput.Model
	targetError  string

	// Step 5: Summary
	summaryContent string

	// Step 6: Running
	jobRows []JobProgressRow
	overallPct float64
	warnings   []string
	cancelConfirm bool

	// Step 7: Done
	results []jobs.Result
	logDir  string

	// Shared
	dryRun      bool
	DoneFunc    func() // called when done, to return to main menu
	scanningUsers bool
}

// UsersScannedMsg is received when user detection completes.
type UsersScannedMsg struct {
	Profiles []users.Profile
	Err      error
}

func NewBackupWizard(dryRun bool) BackupWizardModel {
	ti := textinput.New()
	ti.Placeholder = "D:\\migration-backup"
	ti.Width = 40

	dataItems := []SelectItem{
		{Label: "User folders", Detail: "Desktop, Documents, Downloads, ...", Selected: true},
		{Label: "Browsers", Detail: "Chrome, Edge, Firefox", Selected: true},
		{Label: "Email", Detail: "Outlook PST, Thunderbird", Selected: true},
		{Label: "WiFi profiles", Detail: "Saved networks + passwords", Selected: true},
	}

	return BackupWizardModel{
		step:          BackupStepUsers,
		userSelector:  NewSelector("Select user profiles to back up:", nil),
		dataSelector:  NewSelector("Select data categories to include:", dataItems),
		targetInput:   ti,
		dryRun:        dryRun,
		scanningUsers: true,
	}
}

func (m BackupWizardModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		func() tea.Msg {
			profiles, err := users.Detect()
			return UsersScannedMsg{Profiles: profiles, Err: err}
		},
	)
}

func (m BackupWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case UsersScannedMsg:
		m.scanningUsers = false
		if msg.Err != nil {
			// Show error but don't block — user can still proceed if they type a path manually
			m.userSelector.Items = []SelectItem{
				{Label: "Error detecting users: " + msg.Err.Error(), Disabled: true},
			}
			return m, nil
		}
		var items []SelectItem
		for _, p := range msg.Profiles {
			items = append(items, SelectItem{
				Label:     p.Username,
				Detail:    p.Path,
				SizeBytes: p.SizeBytes,
				Selected:  p.IsCurrent, // pre-select current user
			})
		}
		// If nothing was pre-selected (e.g. domain user not matched), select first
		anySelected := false
		for _, it := range items {
			if it.Selected {
				anySelected = true
				break
			}
		}
		if !anySelected && len(items) > 0 {
			items[0].Selected = true
		}
		m.userSelector.Items = items
		return m, nil

	case tea.KeyMsg:
		// Global quit
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		switch m.step {
		case BackupStepUsers:
			return m.handleUsersStep(msg)
		case BackupStepData:
			return m.handleDataStep(msg)
		case BackupStepOptions:
			return m.handleOptionsStep(msg)
		case BackupStepTarget:
			return m.handleTargetStep(msg)
		case BackupStepSummary:
			return m.handleSummaryStep(msg)
		case BackupStepRunning:
			return m.handleRunningStep(msg)
		case BackupStepDone:
			return m.handleDoneStep(msg)
		}
	}
	return m, nil
}

func (m BackupWizardModel) handleUsersStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.userSelector.AnySelected() {
			m.step = BackupStepData
		}
	case "esc":
		return m, func() tea.Msg { return NavigateMsg{Screen: ScreenMainMenu} }
	default:
		var cmd tea.Cmd
		m.userSelector, cmd = m.userSelector.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m BackupWizardModel) handleDataStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.dataSelector.AnySelected() {
			m.step = BackupStepOptions
		}
	case "esc":
		m.step = BackupStepUsers
	case "tab":
		m.advancedMode = !m.advancedMode
		if m.advancedMode {
			// Add advanced items if not present
			current := len(m.dataSelector.Items)
			if current <= 4 {
				m.dataSelector.Items = append(m.dataSelector.Items,
					SelectItem{Label: "Dev environment", Detail: ".ssh, .gitconfig, .docker"},
					SelectItem{Label: "App configs", Detail: "VS Code settings, AppData"},
					SelectItem{Label: "Installed apps", Detail: "Export list + winget match"},
				)
			}
		} else {
			// Trim to first 4
			if len(m.dataSelector.Items) > 4 {
				m.dataSelector.Items = m.dataSelector.Items[:4]
			}
		}
	default:
		var cmd tea.Cmd
		m.dataSelector, cmd = m.dataSelector.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m BackupWizardModel) handleOptionsStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.step = BackupStepTarget
		m.targetInput.Focus()
	case "esc":
		m.step = BackupStepData
	case "up", "k":
		// Navigate radio buttons
	case "down", "j":
		// Navigate radio buttons
	case " ":
		// Toggle compress
	}
	return m, nil
}

func (m BackupWizardModel) handleTargetStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		path := strings.TrimSpace(m.targetInput.Value())
		if path == "" {
			m.targetError = "Please enter a destination path"
			return m, nil
		}
		if err := ValidatePath(path); err != nil {
			m.targetError = err.Error()
			return m, nil
		}
		m.targetError = ""
		m.step = BackupStepSummary
		m.summaryContent = m.buildSummary(path)
	case "esc":
		m.step = BackupStepOptions
	default:
		var cmd tea.Cmd
		m.targetInput, cmd = m.targetInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m BackupWizardModel) handleSummaryStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.dryRun {
			return m, tea.Quit
		}
		m.step = BackupStepRunning
		return m, m.startBackup()
	case "esc":
		m.step = BackupStepTarget
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m BackupWizardModel) handleRunningStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cancelConfirm = !m.cancelConfirm
	case "y":
		if m.cancelConfirm {
			return m, tea.Quit
		}
	case "n":
		m.cancelConfirm = false
	}
	return m, nil
}

func (m BackupWizardModel) handleDoneStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "q":
		return m, func() tea.Msg { return NavigateMsg{Screen: ScreenMainMenu} }
	}
	return m, nil
}

// startBackup is a stub — real implementation sends to a channel.
func (m BackupWizardModel) startBackup() tea.Cmd {
	return nil
}

func (m BackupWizardModel) buildSummary(target string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  Target:    %s\n", target))
	sb.WriteString(fmt.Sprintf("  Compress:  %v\n\n", m.compress))

	sb.WriteString("  Data:\n")
	for _, item := range m.dataSelector.Items {
		if item.Selected {
			sb.WriteString(fmt.Sprintf("    ✔ %s\n", item.Label))
		}
	}
	return sb.String()
}

func (m BackupWizardModel) View() string {
	breadcrumb := BackupBreadcrumb(m.step)
	header := StyleHeader.Render(breadcrumb)

	var body string
	var footer string

	switch m.step {
	case BackupStepUsers:
		if m.scanningUsers {
			body = "\n  " + StyleMuted.Render("Scanning user profiles...") + "\n"
			footer = "Please wait..."
		} else {
			body = "\n  Select user profiles to back up:\n\n" + m.userSelector.View()
			footer = "Space toggle  a all  n none  Enter next  Esc back"
		}

	case BackupStepData:
		modeLabel := "[SIMPLE]"
		if m.advancedMode {
			modeLabel = "[ADVANCED]"
		}
		body = "\n  Select data categories to include:  " + StyleMuted.Render(modeLabel) + "\n\n" + m.dataSelector.View()
		footer = "Space toggle  Tab mode  a all  Enter next  Esc back"

	case BackupStepOptions:
		body = m.renderOptions()
		footer = "↑/↓ navigate    Space select    Enter next"

	case BackupStepTarget:
		body = "\n  Enter backup destination path:\n\n  " + m.targetInput.View()
		if m.targetError != "" {
			body += "\n\n  " + StyleError.Render("✘ "+m.targetError)
		}
		footer = "Enter confirm    Esc back"

	case BackupStepSummary:
		body = "\n" + m.summaryContent
		if m.dryRun {
			footer = "q quit (dry-run)"
		} else {
			footer = "Enter START BACKUP    Esc go back    q cancel"
		}

	case BackupStepRunning:
		body = m.renderRunning()
		footer = "Esc cancel (will clean up partial backup)"

	case BackupStepDone:
		body = m.renderDone()
		footer = "Enter back to menu    q quit"
	}

	w := m.width - 4
	if w < 55 {
		w = 55
	}

	content := header + "\n" + body

	panel := StyleBorder.Width(w).Render(content)
	footerLine := StyleFooter.Width(w).Render(footer)

	return panel + "\n" + footerLine
}

func (m BackupWizardModel) renderOptions() string {
	passwordOpts := []string{"Skip", "Assisted export (recommended)", "Experimental auto-extract (insecure)"}
	var sb strings.Builder
	sb.WriteString("\n  Browser passwords:\n")
	for i, opt := range passwordOpts {
		radio := RadioEmpty
		if i == m.passwordMode {
			radio = StyleFocused.Render(RadioSelected)
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n", radio, opt))
	}

	sb.WriteString("\n  Compression:\n")
	if !m.compress {
		sb.WriteString(fmt.Sprintf("  %s No — keep folder structure (faster)\n", StyleFocused.Render(RadioSelected)))
		sb.WriteString(fmt.Sprintf("  %s Yes — create .zip after backup\n", RadioEmpty))
	} else {
		sb.WriteString(fmt.Sprintf("  %s No — keep folder structure (faster)\n", RadioEmpty))
		sb.WriteString(fmt.Sprintf("  %s Yes — create .zip after backup\n", StyleFocused.Render(RadioSelected)))
	}
	return sb.String()
}

func (m BackupWizardModel) renderRunning() string {
	var sb strings.Builder
	sb.WriteString("\n")

	// Overall progress
	sb.WriteString(fmt.Sprintf("  Overall: %s  %.0f%%\n\n",
		renderBar(m.overallPct, 32), m.overallPct*100))

	for _, row := range m.jobRows {
		sb.WriteString(row.View())
	}

	if len(m.warnings) > 0 {
		sb.WriteString("\n  " + StyleWarning.Render(fmt.Sprintf("⚠ %d files skipped or warned", len(m.warnings))))
	}

	if m.cancelConfirm {
		sb.WriteString("\n\n  " + StyleWarning.Render("Cancel backup? [Y] Yes    [N] No"))
	}

	return sb.String()
}

func (m BackupWizardModel) renderDone() string {
	if len(m.results) == 0 {
		return "\n  Backup complete.\n"
	}

	var sb strings.Builder
	sb.WriteString("\n  " + StyleSuccess.Render("✔ Backup finished successfully") + "\n\n")

	for _, r := range m.results {
		icon := StatusIcon(r.Status)
		sb.WriteString(fmt.Sprintf("    %s %-20s %s\n", icon, r.JobName,
			FormatSize(r.SizeBytes)))
	}

	if m.logDir != "" {
		sb.WriteString(fmt.Sprintf("\n  Logs saved to: %s\n", m.logDir))
	}
	return sb.String()
}

func renderBar(pct float64, width int) string {
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled
	return StyleProgressFull.Render(strings.Repeat("█", filled)) +
		StyleProgressEmpty.Render(strings.Repeat("░", empty))
}
