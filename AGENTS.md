# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Build

The binary targets **Windows amd64 only**. Build from any platform:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o migrathor.exe .
```

With version metadata:
```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-X main.version=1.0.0 -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o migrathor.exe .
```

The PowerShell script `scripts/build.ps1` wraps this for Windows CI. GitHub Actions builds and releases `migrathor.exe` automatically on push to main (rolling "latest") and on version tags (`v*`).

There are **no tests** in this codebase.

## Architecture

MigraThor is a Windows TUI application for migrating user data between machines. It uses the **Bubble Tea** model-update-view pattern throughout.

### Layer overview

```
main.go → cmd/ → internal/jobs/ → internal/engine/
              ↘ internal/ui/
```

- **`main.go`**: CLI entry point, routes subcommands (`backup`, `restore`, `cleanup`, `version`) to either TUI screens or direct cmd calls.
- **`cmd/`**: `RunBackup` / `RunRestore` orchestrate job execution. They receive an `Options` struct from the TUI, iterate over selected `Job` implementations, and stream progress via a channel. The channel is closed when done — this is the signal the TUI uses to transition to the Done step.
- **`internal/jobs/`**: All data migration logic. Each job implements the `Job` interface (`Name`, `Description`, `Scan`, `Backup`, `Restore`). Jobs are registered in `registry_windows.go` (Windows) / `registry_stub.go` (non-Windows stubs). Most files have `//go:build windows` guards.
- **`internal/engine/`**: Robocopy wrapper (`Copy`) used by jobs for actual file transfer. Parses robocopy exit codes and output.
- **`internal/ui/`**: All Bubble Tea screens. `backupflow.go` and `restoreflow.go` are the main wizards (~800 and ~1100 lines respectively). `selector.go` provides the reusable multi-select component with parent/child hierarchy.
- **`internal/meta/`**: `metadata.json` written to every backup directory. Used by restore wizard to validate and describe a backup.
- **`internal/logging/`**: Two slog loggers writing to files only (no stderr — stderr output corrupts the TUI alt-screen).

### Job interface

```go
type Job interface {
    Name() string
    Description() string
    Scan(userPath string) (ScanResult, error)
    Backup(userPath, target string, opts Options) (Result, error)
    Restore(source, userPath string, opts Options) (Result, error)
}
```

`Options` carries `ProgressCh chan<- Progress`, `SelectedFolders`, `SelectedBrowsers`, `ConflictStrategy`, `DryRun`, etc.

### Progress flow (TUI ↔ goroutine)

The TUI launches backup/restore in a goroutine. Progress is sent via `chan jobs.Progress`. The TUI listens with a recursive `tea.Cmd` (`listenProgress` / `listenRestoreProgress`). When the goroutine finishes, `cmd.RunBackup/RunRestore` closes the channel, which triggers `backupDoneMsg` / `restoreDoneMsg`. Results are passed via a **shared pointer** (`backupResultPtr *cmd.BackupResult`) set before channel close, read after `doneMsg` arrives — avoids race conditions.

### Data selector (basic vs advanced mode)

`backupflow.go` `NewBackupWizard` defines the basic item list (6 items). Tab in the data step toggles advanced mode, appending 4 more items (Email, Certificates, Dev environment, App configs). The toggle uses `len(m.dataSelector.Items) <= 6` as the threshold — keep this in sync if items are added/removed.

### App reinstall step (restore)

After restore jobs complete, if `apps_winget.json` or `apps.json` exists in the backup, the wizard shows Step 6 (App Reinstall). `loadAppItems()` first tries `apps_winget.json` (only winget-matched apps), then falls back to `apps.json` (all registry apps). Two modes: **script** (generates `reinstall.ps1`) and **execute** (runs `winget install` directly). Apps without a `WingetID` are commented out in script mode and skipped in execute mode.

### Windows-only build tags

All files in `internal/jobs/` that use Windows APIs (`registry`, `netsh`, `robocopy`, `winget`, PowerShell) have `//go:build windows`. Non-Windows stubs live in `*_stub.go` files. The `internal/engine/copy.go` is also Windows-only (robocopy). This means the project compiles on macOS/Linux for syntax checking but cannot run job logic.
