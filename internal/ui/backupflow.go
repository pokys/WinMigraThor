package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/pokys/winmigrathor/cmd"
	"github.com/pokys/winmigrathor/internal/jobs"
	"github.com/pokys/winmigrathor/internal/users"
)

// ── Messages ──────────────────────────────────────────────────────────────────

// UsersScannedMsg is received when user detection completes.
type UsersScannedMsg struct {
	Profiles []users.Profile
	Err      error
}

type backupProgressMsg jobs.Progress
type backupDoneMsg struct {
	result *cmd.BackupResult
}

// ── Job label → internal name mapping ─────────────────────────────────────────

var jobLabelToName = map[string]string{
	"User folders":        "userdata",
	"Browsers":            "browsers",
	"Bookmarks only":      "bookmarks",
	"Email":               "email",
	"WiFi profiles":       "wifi",
	"VPN connections":     "vpn",
	"Credentials":         "credentials",
	"Certificates":        "certificates",
	"Dev environment":     "devenv",
	"App configs":         "appconfig",
	"Installed apps":      "apps",
	"Printers":            "printers",
}

// ── Options step: flat cursor over all radio options ─────────────────────────
// Layout:
//   [0] Compression: No
//   [1] Compression: Yes
//   [2] Delete unzipped folder (only shown when compress=Yes)
const (
	optCompNo           = 0
	optCompYes          = 1
	optDeleteAfterZip   = 2
	optionsCountBase    = 2
	optionsCountWithDel = 3
)

// ── Model ─────────────────────────────────────────────────────────────────────

// BackupWizardModel is the Bubble Tea model for the full backup wizard.
type BackupWizardModel struct {
	step   BackupStep
	width  int
	height int

	// Step 1: Users
	userSelector  Selector
	scanningUsers bool

	// Step 2: Data
	dataSelector Selector
	advancedMode bool

	// Step 3: Options
	optionsCursor  int
	compress       bool
	deleteAfterZip bool

	// Step 4: Target
	targetInput textinput.Model
	targetError string

	// Step 5: Summary
	summaryContent string

	// Step 6: Running
	jobRows         []JobProgressRow
	overallPct      float64
	warnings        []string
	cancelConfirm   bool
	progressCh      chan jobs.Progress
	backupResultPtr *cmd.BackupResult
	startTime       time.Time

	// Step 7: Done
	results       []jobs.Result
	logDir        string
	dryRun        bool
	finalDuration time.Duration
}

func NewBackupWizard(dryRun bool) BackupWizardModel {
	ti := textinput.New()
	ti.Placeholder = `D:\migration-backup`
	ti.Width = 40

	return BackupWizardModel{
		step:          BackupStepUsers,
		userSelector:  NewSelector("Select user profiles to back up:", nil),
		dataSelector:  NewSelector("Select data categories to include:", buildBackupDataItems(nil, false)),
		targetInput:   ti,
		dryRun:        dryRun,
		scanningUsers: true,
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m BackupWizardModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		func() tea.Msg {
			profiles, err := users.Detect()
			return UsersScannedMsg{Profiles: profiles, Err: err}
		},
	)
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m BackupWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case UsersScannedMsg:
		m.scanningUsers = false
		if msg.Err != nil {
			m.userSelector.Items = []SelectItem{
				{Label: "Chyba detekce: " + msg.Err.Error(), Disabled: true},
			}
			m.userSelector.rebuildFlat()
			return m, nil
		}
		var items []SelectItem
		for _, p := range msg.Profiles {
			items = append(items, SelectItem{
				Label:     p.Username,
				Detail:    p.Path,
				SizeBytes: p.SizeBytes,
				Selected:  p.IsCurrent,
			})
		}
		// Pre-select first if nobody matched as current
		if len(items) > 0 {
			anySelected := false
			for _, it := range items {
				if it.Selected {
					anySelected = true
					break
				}
			}
			if !anySelected {
				items[0].Selected = true
			}
		}
		m.userSelector = NewSelector("Select user profiles to back up:", items)
		return m, nil

	case backupProgressMsg:
		p := jobs.Progress(msg)
		m.updateProgress(p)
		// Keep listening
		return m, listenProgress(m.progressCh)

	case backupDoneMsg:
		m.step = BackupStepDone
		if !m.startTime.IsZero() {
			m.finalDuration = time.Since(m.startTime).Round(time.Second)
		}
		// RunBackup closes the channel after returning, so the result
		// pointer has been populated by the goroutine at this point.
		if m.backupResultPtr != nil && len(m.backupResultPtr.Results) > 0 {
			m.results = m.backupResultPtr.Results
			m.logDir = m.backupResultPtr.LogDir
		}
		// Mark all rows done and update overall progress
		for i := range m.jobRows {
			if m.jobRows[i].Bar.Status == "running" || m.jobRows[i].Bar.Status == "waiting" {
				m.jobRows[i].Bar.Status = "done"
				m.jobRows[i].Bar.Percent = 1.0
			}
		}
		m.overallPct = 1.0
		return m, nil

	case tea.KeyMsg:
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

func (m *BackupWizardModel) updateProgress(p jobs.Progress) {
	// Find or create job row
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
			Total: len(m.jobRows) + 1, // updated below
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

	if p.Warning != "" {
		m.warnings = append(m.warnings, p.Warning)
	}

	// Update overall progress
	var totalPct float64
	for _, r := range m.jobRows {
		totalPct += r.Bar.Percent
	}
	if len(m.jobRows) > 0 {
		m.overallPct = totalPct / float64(len(m.jobRows))
	}
}

// ── Step handlers ─────────────────────────────────────────────────────────────

func (m BackupWizardModel) handleUsersStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.scanningUsers {
		return m, nil
	}
	switch msg.String() {
	case "enter":
		if m.userSelector.AnySelected() {
			m.refreshDataSelector()
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
		m.refreshDataSelector()
	default:
		var cmd tea.Cmd
		m.dataSelector, cmd = m.dataSelector.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *BackupWizardModel) refreshDataSelector() {
	browserChildren := detectedBackupBrowserChildren(m.selectedBackupUserPaths())
	m.dataSelector = NewSelector("Select data categories to include:", buildBackupDataItems(browserChildren, m.advancedMode))
}

func (m BackupWizardModel) selectedBackupUserPaths() []string {
	var userPaths []string
	for _, item := range m.userSelector.Items {
		if item.Selected {
			path := item.Detail
			if path == "" {
				path = filepath.Join(`C:\Users`, item.Label)
			}
			userPaths = append(userPaths, path)
		}
	}
	return userPaths
}

func buildBackupDataItems(browserChildren []SelectItem, advanced bool) []SelectItem {
	folderChildren := []SelectItem{
		{Label: "Desktop", Selected: true},
		{Label: "Documents", Selected: true},
		{Label: "Downloads", Selected: true},
		{Label: "Pictures", Selected: true},
		{Label: "Videos", Selected: true},
		{Label: "Music", Selected: true},
	}

	browsersItem := SelectItem{
		Label:    "Browsers",
		Detail:   "No supported browsers detected for selected users",
		Selected: false,
		Disabled: true,
	}
	if len(browserChildren) > 0 {
		browsersItem.Detail = "Full profiles of detected browsers"
		browsersItem.Selected = true
		browsersItem.Disabled = false
		browsersItem.Children = browserChildren
	}

	dataItems := []SelectItem{
		{Label: "User folders", Detail: "Desktop, Documents, Downloads, ...", Selected: true, Children: folderChildren},
		browsersItem,
		{Label: "Bookmarks only", Detail: "Export bookmarks as HTML files", Selected: false},
		{Label: "Credentials", Detail: "Windows Credential Manager vault", Selected: true},
		{Label: "Installed apps", Detail: "Export list + winget match", Selected: true},
	}

	if advanced {
		dataItems = append(dataItems,
			SelectItem{Label: "Email", Detail: "Outlook PST, Thunderbird", Selected: true},
			SelectItem{Label: "WiFi profiles", Detail: "Saved networks + passwords", Selected: true},
			SelectItem{Label: "VPN connections", Detail: "RAS phonebook (.pbk) files", Selected: true},
			SelectItem{Label: "Printers", Detail: "Network + local printers (PrintBRM)", Selected: true},
			SelectItem{Label: "Certificates", Detail: "Personal certificates (valid, with private key)", Selected: true},
			SelectItem{Label: "Dev environment", Detail: ".ssh, .gitconfig, .docker"},
			SelectItem{Label: "App configs", Detail: "VS Code settings, AppData"},
		)
	}

	return dataItems
}

func detectedBackupBrowserChildren(userPaths []string) []SelectItem {
	seen := make(map[string]bool)
	for _, userPath := range userPaths {
		for _, name := range jobs.DetectedBrowserNames(userPath) {
			seen[name] = true
		}
	}

	order := []string{"Chrome", "Edge", "Firefox"}
	children := make([]SelectItem, 0, len(order))
	for _, name := range order {
		if seen[name] {
			children = append(children, SelectItem{Label: name, Selected: true})
			delete(seen, name)
		}
	}

	if len(seen) > 0 {
		var extra []string
		for name := range seen {
			extra = append(extra, name)
		}
		sort.Strings(extra)
		for _, name := range extra {
			children = append(children, SelectItem{Label: name, Selected: true})
		}
	}

	return children
}

func (m BackupWizardModel) handleOptionsStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxCursor := optionsCountBase - 1
	if m.compress {
		maxCursor = optionsCountWithDel - 1
	}
	switch msg.String() {
	case "enter":
		m.step = BackupStepTarget
		m.targetInput.Focus()
	case "esc":
		m.step = BackupStepData
	case "up", "k":
		if m.optionsCursor > 0 {
			m.optionsCursor--
		}
	case "down", "j":
		if m.optionsCursor < maxCursor {
			m.optionsCursor++
		}
	case " ", "l", "right":
		switch m.optionsCursor {
		case optCompNo:
			m.compress = false
			m.deleteAfterZip = false
			if m.optionsCursor > optionsCountBase-1 {
				m.optionsCursor = optionsCountBase - 1
			}
		case optCompYes:
			m.compress = true
		case optDeleteAfterZip:
			m.deleteAfterZip = !m.deleteAfterZip
		}
	}
	return m, nil
}

func (m BackupWizardModel) handleTargetStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		path := strings.TrimSpace(m.targetInput.Value())
		if path == "" {
			m.targetError = "Zadej cestu k cílovému adresáři"
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
		m.startTime = time.Now()
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

// ── Backup execution ──────────────────────────────────────────────────────────

func (m *BackupWizardModel) startBackup() tea.Cmd {
	// Collect selected user paths
	var userPaths []string
	for _, item := range m.userSelector.Items {
		if item.Selected {
			path := item.Detail // Detail holds the profile path
			if path == "" {
				path = filepath.Join(`C:\Users`, item.Label)
			}
			userPaths = append(userPaths, path)
		}
	}

	// Collect selected job names, respecting sub-selection for items with children
	var jobNames []string
	for _, item := range m.dataSelector.Items {
		name, ok := jobLabelToName[item.Label]
		if !ok {
			continue
		}
		if len(item.Children) > 0 {
			// Include if any children selected (or no children defined = selected itself)
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

	// Collect selected folder names for userdata job
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

	// Collect selected browser names
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

	target := strings.TrimSpace(m.targetInput.Value())

	// Setup progress channel
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

	opts := cmd.BackupOptions{
		Target:           target,
		Users:            userPaths,
		JobNames:         jobNames,
		SelectedFolders:  selectedFolders,
		SelectedBrowsers: selectedBrowsers,
		DryRun:           m.dryRun,
		Compress:         m.compress,
		DeleteAfterZip:   m.deleteAfterZip,
		PasswordMode:     "skip",
		ConflictStrategy: "ask",
	}

	// Run backup in background goroutine; RunBackup closes ch when done.
	// We store the result pointer atomically — by the time the progress
	// channel closes (triggering backupDoneMsg), RunBackup has returned
	// and the pointer is set.
	m.backupResultPtr = new(cmd.BackupResult)
	resultPtr := m.backupResultPtr
	go func() {
		result, _ := cmd.RunBackup(opts, allJ, ch)
		if result != nil {
			*resultPtr = *result
		}
	}()

	return listenProgress(ch)
}

func listenProgress(ch chan jobs.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return backupDoneMsg{}
		}
		return backupProgressMsg(p)
	}
}

func filterJobsByName(all []jobs.Job, names []string) []jobs.Job {
	set := make(map[string]bool)
	for _, n := range names {
		set[n] = true
	}
	var result []jobs.Job
	for _, j := range all {
		if set[j.Name()] {
			result = append(result, j)
		}
	}
	return result
}

// ── Summary builder ───────────────────────────────────────────────────────────

func (m BackupWizardModel) buildSummary(target string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  %-14s %s\n", "Target:", target))
	sb.WriteString(fmt.Sprintf("  %-14s %s\n", "Compress:", map[bool]string{true: "Ano", false: "Ne"}[m.compress]))
	if m.compress {
		sb.WriteString(fmt.Sprintf("  %-14s %s\n", "Smazat složku:", map[bool]string{true: "Ano", false: "Ne"}[m.deleteAfterZip]))
	}

	sb.WriteString("\n  Uživatelé:\n")
	for _, item := range m.userSelector.Items {
		if item.Selected {
			sb.WriteString(fmt.Sprintf("    %s %-20s %s\n",
				StyleSuccess.Render(IconSuccess), item.Label, FormatSize(item.SizeBytes)))
		}
	}

	sb.WriteString("\n  Data:\n")
	for _, item := range m.dataSelector.Items {
		anySelected := item.Selected
		for _, c := range item.Children {
			if c.Selected {
				anySelected = true
				break
			}
		}
		if !anySelected {
			continue
		}
		sb.WriteString(fmt.Sprintf("    %s %s\n", StyleSuccess.Render(IconSuccess), item.Label))
		for _, c := range item.Children {
			if c.Selected {
				sb.WriteString(fmt.Sprintf("      • %s\n", StyleMuted.Render(c.Label)))
			}
		}
	}

	if m.dryRun {
		sb.WriteString("\n  " + StyleWarning.Render("⚠ DRY-RUN — žádná data nebudou zkopírována"))
	}
	return sb.String()
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m BackupWizardModel) View() string {
	breadcrumb := BackupBreadcrumb(m.step)
	header := StyleHeader.Render(breadcrumb)

	var body, footer string

	switch m.step {
	case BackupStepUsers:
		if m.scanningUsers {
			body = "\n  " + StyleMuted.Render("Hledám uživatelské profily…") + "\n"
			footer = "Prosím čekej…"
		} else if len(m.userSelector.Items) == 0 {
			body = "\n  " + StyleError.Render("✘ Žádné profily nenalezeny.") +
				"\n\n  Ujisti se, že program běží jako správce.\n"
			footer = "Esc zpět"
		} else {
			body = "\n  Vyber uživatelské profily k zálohování:\n\n" + m.userSelector.View()
			footer = "Space přepnout  a vše  n nic  Enter dále  Esc zpět"
		}

	case BackupStepData:
		modeLabel := StyleMuted.Render("[JEDNODUCHÝ]")
		if m.advancedMode {
			modeLabel = StyleMuted.Render("[POKROČILÝ]")
		}
		body = "\n  Vyber kategorie dat:  " + modeLabel + "\n\n" + m.dataSelector.View()
		footer = "Space přepnout  Tab režim  a vše  Enter dále  Esc zpět"

	case BackupStepOptions:
		body = m.renderOptions()
		footer = "↑/↓ navigace    Space / → vybrat    Enter dále    Esc zpět"

	case BackupStepTarget:
		body = "\n  Zadej cestu k zálohovacímu adresáři:\n\n  " + m.targetInput.View()
		if m.targetError != "" {
			body += "\n\n  " + StyleError.Render("✘ "+m.targetError)
		}
		body += "\n\n  " + StyleMuted.Render("Příklad síťové cesty: \\\\server\\share\\backup")
		footer = "Enter potvrdit    Esc zpět"

	case BackupStepSummary:
		body = "\n" + m.summaryContent
		if m.dryRun {
			footer = "q ukončit (dry-run)"
		} else {
			footer = "Enter SPUSTIT ZÁLOHU    Esc zpět    q zrušit"
		}

	case BackupStepRunning:
		body = m.renderRunning()
		footer = "Esc zrušit"

	case BackupStepDone:
		body = m.renderDone()
		footer = "Enter zpět do menu    q ukončit"
	}

	w := m.width - 4
	if w < 60 {
		w = 60
	}
	panel := StyleBorder.Width(w).Render(header + "\n" + body)
	footerLine := StyleFooter.Width(w).Render(footer)
	return panel + "\n" + footerLine
}

func (m BackupWizardModel) renderOptions() string {
	type optDef struct {
		idx        int
		group      string
		label      string
		isSelected bool
		isCheckbox bool
	}

	options := []optDef{
		{optCompNo, "Komprese:", "Ne — ponechat adresářovou strukturu (rychlejší)", !m.compress, false},
		{optCompYes, "", "Ano — vytvořit .zip po záloze", m.compress, false},
	}
	if m.compress {
		options = append(options, optDef{
			optDeleteAfterZip, "Po záloze:", "Smazat nezabalenou složku (ponechat pouze .zip)", m.deleteAfterZip, true,
		})
	}

	var sb strings.Builder
	sb.WriteString("\n")
	lastGroup := ""
	for _, opt := range options {
		if opt.group != "" && opt.group != lastGroup {
			sb.WriteString(fmt.Sprintf("\n  %s\n", StyleTitle.Render(opt.group)))
			lastGroup = opt.group
		}
		var marker string
		if opt.isCheckbox {
			if opt.isSelected {
				marker = StyleSelected.Render(MarkerSelected)
			} else {
				marker = MarkerEmpty
			}
		} else {
			if opt.isSelected {
				marker = StyleSuccess.Render(RadioSelected)
			} else {
				marker = RadioEmpty
			}
		}
		label := opt.label
		if m.optionsCursor == opt.idx {
			label = StyleFocused.Render("> " + opt.label)
		} else {
			label = "  " + label
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n", marker, label))
	}
	return sb.String()
}

func (m BackupWizardModel) renderRunning() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  Celkem: %s  %.0f%%\n\n",
		renderBar(m.overallPct, 32), m.overallPct*100))

	for _, row := range m.jobRows {
		sb.WriteString(row.View())
	}

	if len(m.warnings) > 0 {
		sb.WriteString(fmt.Sprintf("\n  %s\n",
			StyleWarning.Render(fmt.Sprintf("⚠ %d souborů přeskočeno / varování", len(m.warnings)))))
	}

	elapsed := ""
	if !m.startTime.IsZero() {
		elapsed = "  Běží: " + StyleMuted.Render(time.Since(m.startTime).Round(time.Second).String())
	}
	if elapsed != "" {
		sb.WriteString(elapsed + "\n")
	}

	if m.cancelConfirm {
		sb.WriteString("\n  " + StyleWarning.Render("Zrušit zálohu? [Y] Ano    [N] Ne"))
	}
	return sb.String()
}

func (m BackupWizardModel) renderDone() string {
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

	title := StyleSuccess.Render("✔ Záloha dokončena úspěšně")
	if hasError {
		title = StyleError.Render("✘ Záloha dokončena s chybami")
	} else if hasWarning {
		title = StyleWarning.Render("⚠ Záloha dokončena s varováními")
	}

	sb.WriteString("\n  " + title + "\n\n")

	if m.finalDuration > 0 {
		sb.WriteString(fmt.Sprintf("  Doba:      %s\n", m.finalDuration))
	}
	sb.WriteString("\n  Výsledky:\n")
	for _, r := range m.results {
		icon := StatusIcon(r.Status)
		sb.WriteString(fmt.Sprintf("    %s %-20s %s    %d chyb\n",
			icon, r.JobName, FormatSize(r.SizeBytes), len(r.Errors)))
		for _, w := range r.Warnings {
			sb.WriteString("      • " + StyleMuted.Render(w) + "\n")
		}
	}

	if m.logDir != "" {
		sb.WriteString(fmt.Sprintf("\n  Logy uloženy v: %s\n", StyleMuted.Render(m.logDir)))
	}
	return sb.String()
}

func renderBar(pct float64, width int) string {
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled
	return StyleProgressFull.Render(strings.Repeat("█", filled)) +
		StyleProgressEmpty.Render(strings.Repeat("░", empty))
}
