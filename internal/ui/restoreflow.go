package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/pokys/winmigrathor/cmd"
	"github.com/pokys/winmigrathor/internal/jobs"
	"github.com/pokys/winmigrathor/internal/meta"
)

// ── Messages ─────────────────────────────────────────────────────────────────

type restoreProgressMsg jobs.Progress
type restoreDoneMsg struct{ result *cmd.RestoreResult }
type wingetCheckMsg struct {
	available      bool
	installedApps  map[string]bool // winget ID → installed
}

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

	// Step 2: Data selection (with sub-items for folders/browsers)
	dataSelector Selector

	// Step 3: User mapping
	userMappings    []userMapping
	mappingCursor   int

	// Step 4: Conflict strategy
	conflictCursor int

	// Step 5: Running
	jobRows          []JobProgressRow
	overallPct       float64
	cancelConfirm    bool
	progressCh       chan jobs.Progress
	restoreResultPtr *cmd.RestoreResult
	startTime        time.Time

	// Step 6: App reinstall
	appItems        []appInstallItem
	appCursor       int
	installMode     int // 0=script, 1=execute
	wingetAvailable bool
	wingetChecked   bool

	// Step 7: Done
	results []jobs.Result
	logDir  string
}

type userMapping struct {
	sourceUser string
	targetUser textinput.Model
}

type appInstallItem struct {
	Name           string
	WingetID       string
	MatchQuality   string
	Selected       bool
	AlreadyInstalled bool
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

	case restoreProgressMsg:
		p := jobs.Progress(msg)
		m.updateProgress(p)
		return m, listenRestoreProgress(m.progressCh)

	case restoreDoneMsg:
		if m.restoreResultPtr != nil && len(m.restoreResultPtr.Results) > 0 {
			m.results = m.restoreResultPtr.Results
			m.logDir = m.restoreResultPtr.LogDir
		}
		// Mark all rows done
		for i := range m.jobRows {
			if m.jobRows[i].Bar.Status == "running" || m.jobRows[i].Bar.Status == "waiting" {
				m.jobRows[i].Bar.Status = "done"
				m.jobRows[i].Bar.Percent = 1.0
			}
		}
		// Check if we have app data — if so go to apps step, otherwise done
		if m.hasAppData() {
			m.step = RestoreStepApps
			return m, m.checkWingetAndLoadApps()
		}
		m.step = RestoreStepDone
		return m, nil

	case wingetCheckMsg:
		m.wingetAvailable = msg.available
		m.wingetChecked = true
		// Load app items from backup if not yet loaded
		if len(m.appItems) == 0 {
			m.loadAppItems()
		}
		// Mark already installed apps
		for i := range m.appItems {
			if msg.installedApps[m.appItems[i].WingetID] {
				m.appItems[i].AlreadyInstalled = true
				m.appItems[i].Selected = false
			}
		}
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

func (m *RestoreWizardModel) updateProgress(p jobs.Progress) {
	idx := -1
	for i, r := range m.jobRows {
		if r.Name == p.JobName {
			idx = i
			break
		}
	}
	if idx == -1 {
		m.jobRows = append(m.jobRows, JobProgressRow{
			Name:  p.JobName,
			Index: len(m.jobRows) + 1,
			Total: len(m.jobRows) + 1,
			Bar:   NewProgressBar(p.JobName),
		})
		idx = len(m.jobRows) - 1
	}

	if p.Done {
		m.jobRows[idx].Bar.Status = "done"
		m.jobRows[idx].Bar.Percent = 1.0
	} else if p.Err != nil {
		m.jobRows[idx].Bar.Status = "error"
	} else {
		m.jobRows[idx].Bar.Status = "running"
		if p.Total > 0 {
			m.jobRows[idx].Bar.Percent = float64(p.Current) / float64(p.Total)
		}
		m.jobRows[idx].Bar.CurrentFile = p.CurrentFile
	}

	// Update overall
	var totalPct float64
	for _, r := range m.jobRows {
		totalPct += r.Bar.Percent
	}
	if len(m.jobRows) > 0 {
		m.overallPct = totalPct / float64(len(m.jobRows))
	}
}

// ── Step handlers ────────────────────────────────────────────────────────────

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
			return m, nil
		}
		var cmd tea.Cmd
		m.sourceInput, cmd = m.sourceInput.Update(msg)
		return m, cmd
	}
}

func (m *RestoreWizardModel) buildDataSelector() {
	backupPath := strings.TrimSpace(m.sourceInput.Value())
	var items []SelectItem

	// Check for each job type in the backup and build detailed selectors
	for _, j := range m.sourceMeta.Jobs {
		switch j.Name {
		case "userdata":
			// Scan the actual backup to find which folders were backed up
			children := m.scanBackupFolders(backupPath)
			items = append(items, SelectItem{
				Label:     "User folders",
				Detail:    fmt.Sprintf("%d folders", len(children)),
				SizeBytes: j.SizeBytes,
				Selected:  true,
				Children:  children,
			})

		case "browsers":
			// Scan for actual browser dirs in backup
			children := m.scanBackupBrowsers(backupPath)
			items = append(items, SelectItem{
				Label:     "Browsers",
				Detail:    fmt.Sprintf("%d browsers", len(children)),
				SizeBytes: j.SizeBytes,
				Selected:  true,
				Children:  children,
			})

		case "bookmarks":
			items = append(items, SelectItem{
				Label:     "Bookmarks",
				Detail:    "HTML bookmark files",
				SizeBytes: j.SizeBytes,
				Selected:  true,
			})

		default:
			// Generic job types
			label := jobNameToLabel(j.Name)
			items = append(items, SelectItem{
				Label:     label,
				SizeBytes: j.SizeBytes,
				Selected:  true,
			})
		}
	}

	if len(items) == 0 {
		// Fallback: show job types from metadata without detail
		for _, j := range m.sourceMeta.Jobs {
			items = append(items, SelectItem{
				Label:    jobNameToLabel(j.Name),
				Selected: true,
			})
		}
	}

	m.dataSelector = NewSelector("Select what to restore:", items)
}

// scanBackupFolders inspects the backup directory for actual user data folders.
func (m *RestoreWizardModel) scanBackupFolders(backupPath string) []SelectItem {
	var children []SelectItem
	usersDir := filepath.Join(backupPath, "users")
	if _, err := os.Stat(usersDir); os.IsNotExist(err) {
		return nil
	}

	// Look for user subdirectories
	userEntries, err := os.ReadDir(usersDir)
	if err != nil {
		return nil
	}

	standardFolders := []string{"Desktop", "Documents", "Downloads", "Pictures", "Videos", "Music"}

	for _, ue := range userEntries {
		if !ue.IsDir() {
			continue
		}
		userDir := filepath.Join(usersDir, ue.Name())
		for _, folder := range standardFolders {
			folderPath := filepath.Join(userDir, folder)
			if _, err := os.Stat(folderPath); err == nil {
				// Check if already added (deduplicate across users)
				found := false
				for _, c := range children {
					if c.Label == folder {
						found = true
						break
					}
				}
				if !found {
					children = append(children, SelectItem{
						Label:    folder,
						Selected: true,
					})
				}
			}
		}
	}
	return children
}

// scanBackupBrowsers inspects the backup for browser directories.
func (m *RestoreWizardModel) scanBackupBrowsers(backupPath string) []SelectItem {
	var children []SelectItem
	browsersDir := filepath.Join(backupPath, "browsers")
	if _, err := os.Stat(browsersDir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(browsersDir)
	if err != nil {
		return nil
	}

	browserNames := map[string]string{
		"chrome":  "Chrome",
		"edge":    "Edge",
		"firefox": "Firefox",
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name, ok := browserNames[strings.ToLower(e.Name())]
		if !ok {
			name = e.Name()
		}
		children = append(children, SelectItem{
			Label:    name,
			Selected: true,
		})
	}
	return children
}

func jobNameToLabel(name string) string {
	labels := map[string]string{
		"userdata":     "User folders",
		"browsers":     "Browsers",
		"bookmarks":    "Bookmarks",
		"email":        "Email",
		"wifi":         "WiFi profiles",
		"credentials":  "Credentials",
		"certificates": "Certificates",
		"devenv":       "Dev environment",
		"appconfig":    "App configs",
		"apps":         "Installed apps",
	}
	if l, ok := labels[name]; ok {
		return l
	}
	return name
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
	case "tab":
		if len(m.userMappings) > 1 {
			m.userMappings[m.mappingCursor].targetUser.Blur()
			m.mappingCursor = (m.mappingCursor + 1) % len(m.userMappings)
			m.userMappings[m.mappingCursor].targetUser.Focus()
		}
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
	case " ":
		// Space selects radio — tracked by cursor
	case "enter":
		m.step = RestoreStepRunning
		m.startTime = time.Now()
		return m, m.startRestore()
	case "esc":
		m.step = RestoreStepMapping
	}
	return m, nil
}

// ── Restore execution ────────────────────────────────────────────────────────

func (m *RestoreWizardModel) startRestore() tea.Cmd {
	// Collect selected job names
	var jobNames []string
	for _, item := range m.dataSelector.Items {
		name := labelToJobName(item.Label)
		if name == "" {
			continue
		}
		if len(item.Children) > 0 {
			anyChild := false
			for _, c := range item.Children {
				if c.Selected {
					anyChild = true
					break
				}
			}
			if item.Selected || anyChild {
				jobNames = append(jobNames, name)
			}
		} else if item.Selected {
			jobNames = append(jobNames, name)
		}
	}

	// Collect selected folders for userdata
	var selectedFolders []string
	for _, item := range m.dataSelector.Items {
		if item.Label == "User folders" {
			for _, c := range item.Children {
				if c.Selected {
					selectedFolders = append(selectedFolders, c.Label)
				}
			}
		}
	}

	// Collect selected browsers
	var selectedBrowsers []string
	for _, item := range m.dataSelector.Items {
		if item.Label == "Browsers" {
			for _, c := range item.Children {
				if c.Selected {
					selectedBrowsers = append(selectedBrowsers, c.Label)
				}
			}
		}
	}

	// Build user mapping
	userMapping := make(map[string]string)
	for _, um := range m.userMappings {
		target := strings.TrimSpace(um.targetUser.Value())
		if target != "" {
			userMapping[um.sourceUser] = filepath.Join(`C:\Users`, target)
		}
	}

	conflictStrategies := []string{"ask", "overwrite", "skip", "rename"}
	strategy := "ask"
	if m.conflictCursor < len(conflictStrategies) {
		strategy = conflictStrategies[m.conflictCursor]
	}

	source := strings.TrimSpace(m.sourceInput.Value())

	ch := make(chan jobs.Progress, 100)
	m.progressCh = ch

	// Initialize job rows
	allJ := jobs.AllJobs()
	activeJobs := filterJobsByName(allJ, jobNames)
	m.jobRows = make([]JobProgressRow, len(activeJobs))
	for i, j := range activeJobs {
		m.jobRows[i] = JobProgressRow{
			Name:  j.Name(),
			Index: i + 1,
			Total: len(activeJobs),
			Bar:   NewProgressBar(j.Name()),
		}
		m.jobRows[i].Bar.Status = "waiting"
	}

	opts := cmd.RestoreOptions{
		Source:           source,
		UserMapping:      userMapping,
		JobNames:         jobNames,
		ConflictStrategy: strategy,
		SelectedFolders:  selectedFolders,
		SelectedBrowsers: selectedBrowsers,
	}

	m.restoreResultPtr = new(cmd.RestoreResult)
	resultPtr := m.restoreResultPtr
	go func() {
		result, _ := cmd.RunRestore(opts, allJ, ch)
		if result != nil {
			*resultPtr = *result
		}
	}()

	return listenRestoreProgress(ch)
}

func listenRestoreProgress(ch chan jobs.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return restoreDoneMsg{}
		}
		return restoreProgressMsg(p)
	}
}

func labelToJobName(label string) string {
	mapping := map[string]string{
		"User folders":    "userdata",
		"Browsers":        "browsers",
		"Bookmarks":       "bookmarks",
		"Email":           "email",
		"WiFi profiles":   "wifi",
		"Credentials":     "credentials",
		"Certificates":    "certificates",
		"Dev environment": "devenv",
		"App configs":     "appconfig",
		"Installed apps":  "apps",
	}
	return mapping[label]
}

// ── Apps step: check winget + load apps ──────────────────────────────────────

func (m *RestoreWizardModel) hasAppData() bool {
	backupPath := strings.TrimSpace(m.sourceInput.Value())
	appsJSON := filepath.Join(backupPath, "apps_winget.json")
	_, err := os.Stat(appsJSON)
	return err == nil
}

func (m *RestoreWizardModel) loadAppItems() {
	backupPath := strings.TrimSpace(m.sourceInput.Value())
	appsJSON := filepath.Join(backupPath, "apps_winget.json")
	data, err := os.ReadFile(appsJSON)
	if err != nil {
		return
	}

	var wingetApps []struct {
		WingetID     string `json:"winget_id"`
		Name         string `json:"name"`
		MatchQuality string `json:"match_quality"`
	}
	if err := json.Unmarshal(data, &wingetApps); err != nil {
		return
	}

	for _, app := range wingetApps {
		m.appItems = append(m.appItems, appInstallItem{
			Name:         app.Name,
			WingetID:     app.WingetID,
			MatchQuality: app.MatchQuality,
			Selected:     app.MatchQuality == "exact",
		})
	}
}

func (m *RestoreWizardModel) checkWingetAndLoadApps() tea.Cmd {
	return func() tea.Msg {
		// Check winget availability
		available := checkWingetAvailable()

		// Check what's already installed
		installedApps := make(map[string]bool)
		if available {
			installedApps = getInstalledWingetApps()
		}

		return wingetCheckMsg{
			available:     available,
			installedApps: installedApps,
		}
	}
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
	case "a":
		for i := range m.appItems {
			if !m.appItems[i].AlreadyInstalled {
				m.appItems[i].Selected = true
			}
		}
	case "n":
		for i := range m.appItems {
			m.appItems[i].Selected = false
		}
	case "tab":
		if m.wingetAvailable {
			m.installMode = (m.installMode + 1) % 2
		}
	case "enter":
		m.step = RestoreStepDone
	case "s":
		// Skip app install
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

// ── View ─────────────────────────────────────────────────────────────────────

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
		footer = "↑/↓ navigate    Enter start restore    Esc back"

	case RestoreStepRunning:
		body = m.renderRunning()
		footer = "Esc cancel"

	case RestoreStepApps:
		body = m.renderAppsStep()
		footer = "Space toggle  a all  n none  Tab mode  Enter confirm  S skip"

	case RestoreStepDone:
		body = m.renderDone()
		footer = "Enter back to menu    q quit"
	}

	w := m.width - 4
	if w < 60 {
		w = 60
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
		if len(m.sourceMeta.Jobs) > 0 {
			sb.WriteString("\n  Backed up data:\n")
			for _, j := range m.sourceMeta.Jobs {
				icon := StatusIcon(j.Status)
				sb.WriteString(fmt.Sprintf("    %s %-20s %s\n",
					icon, jobNameToLabel(j.Name), FormatSize(j.SizeBytes)))
			}
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

	elapsed := ""
	if !m.startTime.IsZero() {
		elapsed = "  Running: " + StyleMuted.Render(time.Since(m.startTime).Round(time.Second).String())
	}
	if elapsed != "" {
		sb.WriteString(elapsed + "\n")
	}

	if m.cancelConfirm {
		sb.WriteString("\n\n  " + StyleWarning.Render("Cancel restore? [Y] Yes    [N] No"))
	}
	return sb.String()
}

func (m RestoreWizardModel) renderAppsStep() string {
	var sb strings.Builder
	sb.WriteString("\n  The backup contains an installed apps list.\n")

	if !m.wingetChecked {
		sb.WriteString("  " + StyleMuted.Render("Checking winget availability...") + "\n")
		return sb.String()
	}

	if !m.wingetAvailable {
		sb.WriteString("  " + StyleWarning.Render("⚠ winget is not installed or not available.") + "\n")
		sb.WriteString("  " + StyleMuted.Render("Install winget first, or press S to skip.") + "\n")
		return sb.String()
	}

	sb.WriteString("  Select apps to reinstall via winget:\n\n")

	if len(m.appItems) == 0 {
		sb.WriteString("  " + StyleMuted.Render("No winget-compatible apps found in this backup."))
		return sb.String()
	}

	// Count stats
	var selectedCount, installedCount int
	for _, app := range m.appItems {
		if app.Selected {
			selectedCount++
		}
		if app.AlreadyInstalled {
			installedCount++
		}
	}

	sb.WriteString(fmt.Sprintf("  %s %d apps in backup, %s %d already installed, %s %d selected\n\n",
		StyleMuted.Render("Total:"), len(m.appItems),
		StyleSuccess.Render("✔"), installedCount,
		StyleFocused.Render("→"), selectedCount))

	// Scrollable app list — show a window of items around the cursor
	// Reserve lines for: header(~6), footer(~5), border(4) → usable ≈ height-15
	maxVisible := m.height - 15
	if maxVisible < 5 {
		maxVisible = 5
	}
	if maxVisible > len(m.appItems) {
		maxVisible = len(m.appItems)
	}

	// Calculate scroll window
	scrollStart := 0
	if len(m.appItems) > maxVisible {
		scrollStart = m.appCursor - maxVisible/2
		if scrollStart < 0 {
			scrollStart = 0
		}
		if scrollStart+maxVisible > len(m.appItems) {
			scrollStart = len(m.appItems) - maxVisible
		}
	}
	scrollEnd := scrollStart + maxVisible

	// Show scroll indicator at top
	if scrollStart > 0 {
		sb.WriteString(fmt.Sprintf("  %s\n", StyleMuted.Render(fmt.Sprintf("  ↑ %d more above", scrollStart))))
	}

	for i := scrollStart; i < scrollEnd; i++ {
		app := m.appItems[i]
		cursor := "  "
		if i == m.appCursor {
			cursor = StyleFocused.Render("› ")
		}

		check := MarkerEmpty
		if app.AlreadyInstalled {
			check = StyleSuccess.Render("[✔]")
		} else if app.Selected {
			check = StyleSelected.Render(MarkerSelected)
		}

		matchNote := ""
		if app.MatchQuality == "partial" {
			matchNote = StyleWarning.Render(" (?)")
		}

		status := ""
		if app.AlreadyInstalled {
			status = StyleSuccess.Render(" (installed)")
		}

		sb.WriteString(fmt.Sprintf("  %s%s %-30s %s%s%s\n",
			cursor, check, app.Name, StyleMuted.Render(app.WingetID), matchNote, status))
	}

	// Show scroll indicator at bottom
	remaining := len(m.appItems) - scrollEnd
	if remaining > 0 {
		sb.WriteString(fmt.Sprintf("  %s\n", StyleMuted.Render(fmt.Sprintf("  ↓ %d more below", remaining))))
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

	hasError := false
	hasWarning := false
	for _, r := range m.results {
		if r.Status == "error" {
			hasError = true
		}
		if r.Status == "warning" {
			hasWarning = true
		}
	}

	title := StyleSuccess.Render("✔ Restore completed successfully")
	if hasError {
		title = StyleError.Render("✘ Restore completed with errors")
	} else if hasWarning {
		title = StyleWarning.Render("⚠ Restore completed with warnings")
	}

	sb.WriteString("\n  " + title + "\n\n")

	if m.sourceMeta.Hostname != "" {
		sb.WriteString(fmt.Sprintf("  Source:    %s (%s)\n", m.sourceMeta.Hostname, m.sourceMeta.Date))
	}

	if !m.startTime.IsZero() {
		sb.WriteString(fmt.Sprintf("  Duration:  %s\n", time.Since(m.startTime).Round(time.Second)))
	}

	sb.WriteString("\n  Results:\n")
	for _, r := range m.results {
		icon := StatusIcon(r.Status)
		sb.WriteString(fmt.Sprintf("    %s %-20s %s    %d errors\n",
			icon, r.JobName, FormatSize(r.SizeBytes), len(r.Errors)))
		for _, w := range r.Warnings {
			sb.WriteString("      • " + StyleMuted.Render(w) + "\n")
		}
	}

	if m.logDir != "" {
		sb.WriteString(fmt.Sprintf("\n  Logs saved to: %s\n", StyleMuted.Render(m.logDir)))
	}
	return sb.String()
}

// ── Winget helpers ───────────────────────────────────────────────────────────

func checkWingetAvailable() bool {
	_, err := exec.LookPath("winget.exe")
	return err == nil
}

func getInstalledWingetApps() map[string]bool {
	out, err := exec.Command("winget.exe", "list", "--source", "winget").Output()
	if err != nil {
		return nil
	}

	installed := make(map[string]bool)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			// Try to match winget IDs (typically contain dots: Publisher.AppName)
			for _, f := range fields {
				if strings.Contains(f, ".") && !strings.HasPrefix(f, "-") {
					installed[f] = true
				}
			}
		}
	}
	return installed
}
