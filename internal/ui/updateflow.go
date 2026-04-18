package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pokys/winmigrathor/cmd"
)

// RestartAfterUpdate is set by the update screen when the user confirms
// a restart. Checked by main after the TUI exits to relaunch cleanly.
var RestartAfterUpdate bool

// updateProgressMsg carries a download progress update from the goroutine.
type updateProgressMsg cmd.UpdateProgress

type updateState int

const (
	updateStateIdle    updateState = iota
	updateStateRunning             // downloading in progress
	updateStateDone                // success, ready to restart
	updateStateError               // download or replace failed
)

// UpdateScreen is the Bubble Tea model for the self-update wizard.
type UpdateScreen struct {
	state      updateState
	bar        ProgressBar
	errMsg     string
	progressCh chan cmd.UpdateProgress
	width      int
}

func NewUpdateScreen() UpdateScreen {
	return UpdateScreen{
		state: updateStateIdle,
		bar:   NewProgressBar("migrathor.exe"),
	}
}

func (m UpdateScreen) Init() tea.Cmd { return nil }

func (m UpdateScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			switch m.state {
			case updateStateIdle:
				m.state = updateStateRunning
				m.bar.Status = "running"
				ch := make(chan cmd.UpdateProgress, 32)
				m.progressCh = ch
				go cmd.RunUpdate(ch)
				return m, listenUpdateProgress(ch)
			case updateStateDone:
				RestartAfterUpdate = true
				return m, tea.Quit
			case updateStateError:
				return m, func() tea.Msg { return NavigateMsg{Screen: ScreenMainMenu} }
			}
		case "q":
			if m.state == updateStateDone {
				return m, tea.Quit
			}
		case "esc":
			if m.state != updateStateRunning {
				return m, func() tea.Msg { return NavigateMsg{Screen: ScreenMainMenu} }
			}
		}

	case updateProgressMsg:
		if msg.Err != nil {
			m.state = updateStateError
			m.errMsg = msg.Err.Error()
			m.bar.Status = "error"
			return m, nil
		}
		if msg.Done {
			m.state = updateStateDone
			m.bar.Percent = 1.0
			m.bar.Status = "done"
			return m, nil
		}
		if msg.Total > 0 {
			m.bar.Percent = float64(msg.Downloaded) / float64(msg.Total)
		}
		m.bar.CurrentFile = formatDownloadProgress(msg.Downloaded, msg.Total)
		return m, listenUpdateProgress(m.progressCh)
	}
	return m, nil
}

func listenUpdateProgress(ch chan cmd.UpdateProgress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return updateProgressMsg{Done: true}
		}
		return updateProgressMsg(p)
	}
}

func formatDownloadProgress(downloaded, total int64) string {
	if total <= 0 {
		return FormatSize(downloaded)
	}
	return fmt.Sprintf("%s / %s", FormatSize(downloaded), FormatSize(total))
}

func (m UpdateScreen) View() string {
	w := m.width - 4
	if w < 60 {
		w = 60
	}

	header := StyleHeader.Render("Aktualizace")
	var body, footer string

	switch m.state {
	case updateStateIdle:
		body = "\n  Stahnout nejnovejsi verzi z GitHubu?\n\n"
		body += "  " + StyleMuted.Render("github.com/pokys/WinMigraThor") + "\n\n"
		body += "  Aktualni exe bude nahrazeno novou verzi.\n"
		body += "  Po dokonceni se aplikace automaticky restartuje.\n"
		footer = "Enter spustit    Esc zpet"

	case updateStateRunning:
		body = "\n  Stahuji novou verzi...\n\n"
		body += m.bar.View()
		footer = "Prosim cekej..."

	case updateStateDone:
		body = "\n  " + StyleSuccess.Render(IconSuccess+" Aktualizace dokoncena!") + "\n\n"
		body += "  Novy " + StyleTitle.Render("migrathor.exe") + " je pripraven.\n"
		footer = "Enter restartovat    q ukoncit"

	case updateStateError:
		body = "\n  " + StyleError.Render(IconError+" Aktualizace selhala") + "\n\n"
		body += "  " + StyleMuted.Render(m.errMsg) + "\n"
		footer = "Enter / Esc zpet"
	}

	panel := StyleBorder.Width(w).Render(header + "\n" + body)
	footerLine := StyleFooter.Width(w).Render(footer)
	return panel + "\n" + footerLine
}
