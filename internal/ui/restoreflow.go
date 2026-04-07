package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/pokys/winmigrathor/internal/jobs"
	"github.com/pokys/winmigrathor/internal/meta"
	
)

// RestoreWizardModel is the Bubble Tea model for the restore wizard.
type RestoreWizardModel struct {
	step   RestoreStep
	width  int
	height int

	// Step 1: Source path
	sourceInput textinput.Model
	sourceError string
	sourceMeta  meta.Metadata
	validated   bool

	// Step 2: Data selection
	dataSelector Selector

	// Step 3: User mapping
	userMappings    []userMapping
	mappingCursor   int

	// Step 4: Conflict strategy
	conflictCursor int

	// Step 5: Running
	jobRows       []JobProgressRow
	overallPct    float64
	cancelConfirm bool

	// Step 6: App reinstall
	appItems       []appInstallItem
	appCursor      int
	installMode    int // 0=script, 1=execute

	// Step 7: Done
	results []jobs.Result
	logDir  string
}

type userMapping struct {
	sourceUser string
	targetUser textinput.Model
}

type appInstallItem struct {
	Name         string
	WingetID     string
	MatchQuality string
	Selected     bool
}

func NewRestoreWizard() RestoreWizardModel {
	ti := textinput.New()
	ti.Placeholder = "D:\\migration-backup"
	ti.Width = 40
	ti.Focus()

	return RestoreWizardModel{
		sourceInput: ti,
		step:        RestoreStepSource,
	}
}

func (m RestoreWizardModel) Init() tea.Cmd { return textinput.Blink }

func (m RestoreWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.step {
		case RestoreStepSource:
			return m.handleSourceStep(msg)
		case RestoreStepData:
			return m.handleDataStep(msg)
		case RestoreStepMapping:
			return m.handleMappingStep(msg)
		case RestoreStepConflict:
			return m.handleConflictStep(msg)
		case RestoreStepRunning:
			return m.handleRunningStep(msg)
		case RestoreStepApps:
			return m.handleAppsStep(msg)
		case RestoreStepDone:
			return m.handleDoneStep(msg)
		}
	}
	return m, nil
}

func (m RestoreWizardModel) handleSourceStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.validated {
			m.step = RestoreStepData
			m.buildDataSelector()
			return m, nil
		}
		path := strings.TrimSpace(m.sourceInput.Value())
		if path == "" {
			m.sourceError = "Please enter a backup path"
			return m, nil
		}
		// Validate
		bm, err := meta.Load(path)
		if err != nil {
			m.sourceError = "Not a valid backup: " + err.Error()
			return m, nil
		}
		m.sourceMeta = bm
		m.validated = true
		m.sourceError = ""
		return m, nil
	case "esc":
		if m.validated {
			m.validated = false
			return m, nil
		}
		return m, func() tea.Msg { return NavigateMsg{Screen: ScreenMainMenu} }
	default:
		if m.validated {
			// Allow re-entering path
			return m, nil
		}
		var cmd tea.Cmd
		m.sourceInput, cmd = m.sourceInput.Update(msg)
		return m, cmd
	}
}

func (m *RestoreWizardModel) buildDataSelector() {
	var items []SelectItem
	for _, j := range m.sourceMeta.Jobs {
		items = append(items, SelectItem{
			Label:     j.Name,
			SizeBytes: j.SizeBytes,
			Selected:  true,
		})
	}
	if len(items) == 0 {
		// Default all job types from metadata
		defaultJobs := []string{"User folders", "Browsers", "Email", "WiFi profiles"}
		for _, j := range defaultJobs {
			items = append(items, SelectItem{Label: j, Selected: true})
		}
	}
	m.dataSelector = NewSelector("Select what to restore:", items)
}

func (m RestoreWizardModel) handleDataStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.dataSelector.AnySelected() {
			m.step = RestoreStepMapping
			m.buildUserMappings()
		}
	case "esc":
		m.step = RestoreStepSource
		m.validated = false
	default:
		var cmd tea.Cmd
		m.dataSelector, cmd = m.dataSelector.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *RestoreWizardModel) buildUserMappings() {
	for _, u := range m.sourceMeta.Users {
		ti := textinput.New()
		ti.SetValue(u)
		ti.Width = 20
		m.userMappings = append(m.userMappings, userMapping{
			sourceUser: u,
			targetUser: ti,
		})
	}
	if len(m.userMappings) > 0 {
		m.userMappings[0].targetUser.Focus()
	}
}

func (m RestoreWizardModel) handleMappingStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.step = RestoreStepConflict
	case "esc":
		m.step = RestoreStepData
	default:
		if len(m.userMappings) > 0 {
			var cmd tea.Cmd
			m.userMappings[m.mappingCursor].targetUser, cmd =
				m.userMappings[m.mappingCursor].targetUser.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m RestoreWizardModel) handleConflictStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.conflictCursor > 0 {
			m.conflictCursor--
		}
	case "down", "j":
		if m.conflictCursor < 3 {
			m.conflictCursor++
		}
	case " ", "enter":
		if msg.String() == "enter" {
			m.step = RestoreStepRunning
			return m, m.startRestore()
		}
		// Space selects radio
		// Already tracked by cursor
	case "esc":
		m.step = RestoreStepMapping
	}
	return m, nil
}

func (m RestoreWizardModel) startRestore() tea.Cmd {
	return nil // stub — real implementation triggers restore
}

func (m RestoreWizardModel) handleRunningStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m RestoreWizardModel) handleAppsStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.appCursor > 0 {
			m.appCursor--
		}
	case "down", "j":
		if m.appCursor < len(m.appItems)-1 {
			m.appCursor++
		}
	case " ":
		if m.appCursor < len(m.appItems) {
			m.appItems[m.appCursor].Selected = !m.appItems[m.appCursor].Selected
		}
	case "enter":
		m.step = RestoreStepDone
	case "s":
		m.step = RestoreStepDone
	}
	return m, nil
}

func (m RestoreWizardModel) handleDoneStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "q":
		return m, func() tea.Msg { return NavigateMsg{Screen: ScreenMainMenu} }
	}
	return m, nil
}

func (m RestoreWizardModel) View() string {
	breadcrumb := RestoreBreadcrumb(m.step)
	header := StyleHeader.Render(breadcrumb)

	var body, footer string

	switch m.step {
	case RestoreStepSource:
		body = m.renderSourceStep()
		footer = "Enter confirm    Esc back to menu"

	case RestoreStepData:
		body = "\n  Select what to restore:\n\n" + m.dataSelector.View()
		footer = "Space toggle  a all  n none  Enter next  Esc back"

	case RestoreStepMapping:
		body = m.renderMappingStep()
		footer = "Tab cycle users    Enter next    Esc back"

	case RestoreStepConflict:
		body = m.renderConflictStep()
		footer = "Space select    Enter next    Esc back"

	case RestoreStepRunning:
		body = m.renderRunning()
		footer = "Esc cancel"

	case RestoreStepApps:
		body = m.renderAppsStep()
		footer = "Space toggle  Enter confirm  S skip  Esc back"

	case RestoreStepDone:
		body = m.renderDone()
		footer = "Enter back to menu    L view logs    q quit"
	}

	w := m.width - 4
	if w < 55 {
		w = 55
	}

	panel := StyleBorder.Width(w).Render(header + "\n" + body)
	footerLine := StyleFooter.Width(w).Render(footer)
	return panel + "\n" + footerLine
}

func (m RestoreWizardModel) renderSourceStep() string {
	var sb strings.Builder
	sb.WriteString("\n  Enter path to backup folder:\n\n  ")
	sb.WriteString(m.sourceInput.View())

	if m.sourceError != "" {
		sb.WriteString("\n\n  " + StyleError.Render("✘ "+m.sourceError))
	}

	if m.validated {
		sb.WriteString("\n\n  " + StyleSuccess.Render("✔ Valid backup found") + "\n\n")
		sb.WriteString(fmt.Sprintf("  Source:    %s\n", m.sourceMeta.Hostname))
		sb.WriteString(fmt.Sprintf("  Date:      %s\n", m.sourceMeta.Date))
		sb.WriteString(fmt.Sprintf("  Version:   %s\n", m.sourceMeta.Version))
		if len(m.sourceMeta.Users) > 0 {
			sb.WriteString(fmt.Sprintf("  Users:     %s\n", strings.Join(m.sourceMeta.Users, ", ")))
		}
		sb.WriteString("\n  " + StyleMuted.Render("Press Enter to continue, Esc to change path"))
	} else {
		sb.WriteString("\n\n  " + StyleMuted.Render("(The folder must contain metadata.json)"))
	}
	return sb.String()
}

func (m RestoreWizardModel) renderMappingStep() string {
	var sb strings.Builder
	sb.WriteString("\n  Map source users to target users:\n\n")
	sb.WriteString(fmt.Sprintf("  %-24s  %s\n", "Source ("+m.sourceMeta.Hostname+")", "Target"))
	sb.WriteString("  " + strings.Repeat("─", 48) + "\n")
	for i, um := range m.userMappings {
		cursor := "  "
		if i == m.mappingCursor {
			cursor = StyleFocused.Render("› ")
		}
		sb.WriteString(fmt.Sprintf("  %s%-22s → %s\n", cursor, um.sourceUser, um.targetUser.View()))
	}
	return sb.String()
}

func (m RestoreWizardModel) renderConflictStep() string {
	options := []string{
		"Ask me each time",
		"Overwrite all (use backup version)",
		"Skip all (keep existing)",
		"Rename backup files (_restored suffix)",
	}
	var sb strings.Builder
	sb.WriteString("\n  When a file already exists on this machine:\n\n")
	for i, opt := range options {
		radio := RadioEmpty
		if i == m.conflictCursor {
			radio = StyleFocused.Render(RadioSelected)
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n", radio, opt))
	}
	return sb.String()
}

func (m RestoreWizardModel) renderRunning() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  Overall: %s  %.0f%%\n\n",
		renderBar(m.overallPct, 32), m.overallPct*100))

	for _, row := range m.jobRows {
		sb.WriteString(row.View())
	}

	if m.cancelConfirm {
		sb.WriteString("\n\n  " + StyleWarning.Render("Cancel restore? [Y] Yes    [N] No"))
	}
	return sb.String()
}

func (m RestoreWizardModel) renderAppsStep() string {
	var sb strings.Builder
	sb.WriteString("\n  The backup contains an installed apps list.\n")
	sb.WriteString("  Select apps to reinstall via winget:\n\n")

	if len(m.appItems) == 0 {
		sb.WriteString("  " + StyleMuted.Render("No app data found in this backup."))
		return sb.String()
	}

	for i, app := range m.appItems {
		cursor := "  "
		if i == m.appCursor {
			cursor = StyleFocused.Render("› ")
		}
		check := MarkerEmpty
		if app.Selected {
			check = StyleSelected.Render(MarkerSelected)
		}
		matchNote := ""
		if app.MatchQuality == "partial" {
			matchNote = StyleMuted.Render(" (?)")
		}
		sb.WriteString(fmt.Sprintf("  %s%s %-30s %s%s\n",
			cursor, check, app.Name, StyleMuted.Render(app.WingetID), matchNote))
	}

	sb.WriteString("\n  Output mode:\n")
	modes := []string{
		"Generate reinstall.ps1 script (review first)",
		"Execute directly (install now)",
	}
	for i, mode := range modes {
		radio := RadioEmpty
		if i == m.installMode {
			radio = StyleFocused.Render(RadioSelected)
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n", radio, mode))
	}
	return sb.String()
}

func (m RestoreWizardModel) renderDone() string {
	var sb strings.Builder
	sb.WriteString("\n  " + StyleSuccess.Render("✔ Restore finished successfully") + "\n\n")

	if m.sourceMeta.Hostname != "" {
		sb.WriteString(fmt.Sprintf("  Source:    %s (%s)\n", m.sourceMeta.Hostname, m.sourceMeta.Date))
	}

	for _, r := range m.results {
		icon := StatusIcon(r.Status)
		sb.WriteString(fmt.Sprintf("    %s %-20s %s\n", icon, r.JobName, FormatSize(r.SizeBytes)))
	}

	if m.logDir != "" {
		sb.WriteString(fmt.Sprintf("\n  Logs saved to: %s\n", m.logDir))
	}
	return sb.String()
}
