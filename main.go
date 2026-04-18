package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pokys/winmigrathor/internal/checks"
	"github.com/pokys/winmigrathor/internal/ui"
)

// Injected at build time via -ldflags.
var (
	version   = "0.0.10"
	buildDate = "unknown"
)

func main() {
	// Clean up .old file left by a previous self-update
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}

	args := os.Args[1:]

	ui.AppVersion = version
	ui.AppBuildDate = buildDate

	if shouldRequireAdmin(args) {
		relaunched, err := checks.EnsureAdminRelaunch(os.Args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error requesting administrator rights: %v\n", err)
			os.Exit(1)
		}
		if relaunched {
			return
		}
	}

	if len(args) == 0 {
		runTUI(ui.ScreenMainMenu, false)
		return
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("MigraThor v%s (built %s)\n", version, buildDate)

	case "help", "--help", "-h":
		printUsage()

	case "backup":
		dryRun := hasFlagDryRun(args[1:])
		runTUI(ui.ScreenBackupWizard, dryRun)

	case "restore":
		runTUI(ui.ScreenRestoreWizard, false)

default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[0])
		printUsage()
		os.Exit(2)
	}
}

func shouldRequireAdmin(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "version", "--version", "-v", "help", "--help", "-h":
		return false
	default:
		return true
	}
}

func hasFlagDryRun(args []string) bool {
	for _, a := range args {
		if a == "--dry-run" || a == "-n" {
			return true
		}
	}
	return false
}

// appModel is the root Bubble Tea model that manages screen routing.
type appModel struct {
	screen     ui.Screen
	mainMenu   ui.MainMenuModel
	backupWiz  ui.BackupWizardModel
	restoreWiz ui.RestoreWizardModel
	updateSc   ui.UpdateScreen
	helpActive bool
	dryRun     bool
	width      int
	height     int
}

func newAppModel(startScreen ui.Screen, dryRun bool) appModel {
	m := appModel{
		screen:   startScreen,
		mainMenu: ui.NewMainMenu(),
		dryRun:   dryRun,
	}
	if startScreen == ui.ScreenBackupWizard {
		m.backupWiz = ui.NewBackupWizard(dryRun)
	}
	if startScreen == ui.ScreenRestoreWizard {
		m.restoreWiz = ui.NewRestoreWizard()
	}
	if startScreen == ui.ScreenUpdate {
		m.updateSc = ui.NewUpdateScreen()
	}
	return m
}

func (m appModel) Init() tea.Cmd {
	switch m.screen {
	case ui.ScreenMainMenu:
		return m.mainMenu.Init()
	case ui.ScreenBackupWizard:
		return m.backupWiz.Init()
	case ui.ScreenRestoreWizard:
		return m.restoreWiz.Init()
	case ui.ScreenUpdate:
		return m.updateSc.Init()
	}
	return nil
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle navigation messages at root level
	if nav, ok := msg.(ui.NavigateMsg); ok {
		return m.navigate(nav.Screen)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		// Global: ? shows help overlay
		if msg.String() == "?" && !m.helpActive {
			m.helpActive = true
			return m, nil
		}
		if m.helpActive {
			m.helpActive = false
			return m, nil
		}
	}

	// Delegate to active screen
	switch m.screen {
	case ui.ScreenMainMenu:
		updated, cmd := m.mainMenu.Update(msg)
		if mm, ok := updated.(ui.MainMenuModel); ok {
			m.mainMenu = mm
		}
		return m, cmd

	case ui.ScreenBackupWizard:
		updated, cmd := m.backupWiz.Update(msg)
		if bw, ok := updated.(ui.BackupWizardModel); ok {
			m.backupWiz = bw
		}
		return m, cmd

	case ui.ScreenRestoreWizard:
		updated, cmd := m.restoreWiz.Update(msg)
		if rw, ok := updated.(ui.RestoreWizardModel); ok {
			m.restoreWiz = rw
		}
		return m, cmd

	case ui.ScreenUpdate:
		updated, cmd := m.updateSc.Update(msg)
		if us, ok := updated.(ui.UpdateScreen); ok {
			m.updateSc = us
		}
		return m, cmd

	case ui.ScreenHelp:
		if _, ok := msg.(tea.KeyMsg); ok {
			return m.navigate(ui.ScreenMainMenu)
		}
		return m, nil
	}
	return m, nil
}

func (m appModel) navigate(screen ui.Screen) (tea.Model, tea.Cmd) {
	m.screen = screen
	// Re-send the current window size so the new screen renders at full width
	sizeCmd := func() tea.Msg {
		return tea.WindowSizeMsg{Width: m.width, Height: m.height}
	}
	switch screen {
	case ui.ScreenMainMenu:
		m.mainMenu = ui.NewMainMenu()
		return m, tea.Batch(m.mainMenu.Init(), sizeCmd)
	case ui.ScreenBackupWizard:
		m.backupWiz = ui.NewBackupWizard(m.dryRun)
		return m, tea.Batch(m.backupWiz.Init(), sizeCmd)
	case ui.ScreenRestoreWizard:
		m.restoreWiz = ui.NewRestoreWizard()
		return m, tea.Batch(m.restoreWiz.Init(), sizeCmd)
	case ui.ScreenUpdate:
		m.updateSc = ui.NewUpdateScreen()
		return m, tea.Batch(m.updateSc.Init(), sizeCmd)
	}
	return m, nil
}

func (m appModel) View() string {
	if m.helpActive {
		return ui.HelpOverlay()
	}
	switch m.screen {
	case ui.ScreenMainMenu:
		return m.mainMenu.View()
	case ui.ScreenBackupWizard:
		return m.backupWiz.View()
	case ui.ScreenRestoreWizard:
		return m.restoreWiz.View()
	case ui.ScreenUpdate:
		return m.updateSc.View()
	case ui.ScreenHelp:
		return ui.HelpOverlay()
	}
	return ""
}

func runTUI(startScreen ui.Screen, dryRun bool) {
	m := newAppModel(startScreen, dryRun)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Relaunch after self-update (terminal is fully restored at this point)
	if ui.RestartAfterUpdate {
		if exe, err := os.Executable(); err == nil {
			proc := exec.Command(exe)
			proc.Stdin = os.Stdin
			proc.Stdout = os.Stdout
			proc.Stderr = os.Stderr
			_ = proc.Start()
		}
	}
}

func printUsage() {
	fmt.Print(`MigraThor - Windows Migration Tool

Usage:
  migrathor.exe                     Launch interactive menu
  migrathor.exe backup              Launch backup wizard
  migrathor.exe backup --dry-run    Show backup plan without executing
  migrathor.exe restore             Launch restore wizard
  migrathor.exe version             Print version information
  migrathor.exe help                Show this help

Keybindings (in TUI):
  ↑/↓ or j/k   Navigate
  Space         Toggle selection
  Enter         Confirm / next step
  Esc           Go back
  Tab           Switch simple/advanced mode
  a / n         Select all / select none
  ?             Show help overlay
  q             Quit
`)
}
