package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pokys/winmigrathor/internal/ui"
)

// Injected at build time via -ldflags.
var (
	version   = "1.0.0"
	buildDate = "unknown"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		runTUI(ui.ScreenMainMenu, false)
		return
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("migrator v%s (built %s)\n", version, buildDate)

	case "help", "--help", "-h":
		printUsage()

	case "backup":
		dryRun := hasFlagDryRun(args[1:])
		runTUI(ui.ScreenBackupWizard, dryRun)

	case "restore":
		runTUI(ui.ScreenRestoreWizard, false)

	case "cleanup":
		runTUI(ui.ScreenCleanup, false)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[0])
		printUsage()
		os.Exit(2)
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
	screen    ui.Screen
	mainMenu  ui.MainMenuModel
	backupWiz ui.BackupWizardModel
	restoreWiz ui.RestoreWizardModel
	cleanupSc  ui.CleanupScreen
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
	if startScreen == ui.ScreenCleanup {
		m.cleanupSc = ui.NewCleanupScreen()
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
	case ui.ScreenCleanup:
		return m.cleanupSc.Init()
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

	case ui.ScreenCleanup:
		updated, cmd := m.cleanupSc.Update(msg)
		if cs, ok := updated.(ui.CleanupScreen); ok {
			m.cleanupSc = cs
		}
		return m, cmd
	}
	return m, nil
}

func (m appModel) navigate(screen ui.Screen) (tea.Model, tea.Cmd) {
	m.screen = screen
	switch screen {
	case ui.ScreenMainMenu:
		m.mainMenu = ui.NewMainMenu()
		return m, m.mainMenu.Init()
	case ui.ScreenBackupWizard:
		m.backupWiz = ui.NewBackupWizard(m.dryRun)
		return m, m.backupWiz.Init()
	case ui.ScreenRestoreWizard:
		m.restoreWiz = ui.NewRestoreWizard()
		return m, m.restoreWiz.Init()
	case ui.ScreenCleanup:
		m.cleanupSc = ui.NewCleanupScreen()
		return m, m.cleanupSc.Init()
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
	case ui.ScreenCleanup:
		return m.cleanupSc.View()
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
}

func printUsage() {
	fmt.Print(`migrator - Windows Migration Tool

Usage:
  migrator.exe                     Launch interactive menu
  migrator.exe backup              Launch backup wizard
  migrator.exe backup --dry-run    Show backup plan without executing
  migrator.exe restore             Launch restore wizard
  migrator.exe cleanup             Remove temporary files
  migrator.exe version             Print version information
  migrator.exe help                Show this help

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
