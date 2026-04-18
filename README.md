# WinMigraThor

Windows TUI tool for migrating user data, apps, and settings between machines — or across a Windows reinstall.

Runs fully in the terminal, requires no installation, and needs only built-in Windows tools (robocopy, netsh, PowerShell). Requires Administrator privileges.

> **Minimum:** Windows 10 version 21H2 (build 19044) or later.

---

## Features

| Category | What gets backed up |
|---|---|
| **User folders** | Desktop, Documents, Downloads, Pictures, Videos, Music (selectable) |
| **Browser profiles** | Chrome, Edge, Firefox — full profiles, cache excluded |
| **Bookmarks** | HTML export of Chrome / Edge / Firefox bookmarks |
| **Email** | Outlook PST/OST files, Thunderbird profiles |
| **WiFi** | Saved networks and passwords (via `netsh`) |
| **VPN** | RAS/VPN phonebook connections |
| **Credentials** | Windows Credential Manager vaults |
| **Certificates** | Personal certificates with private keys |
| **Installed apps** | App list with winget ID matching for reinstall |
| **Dev environment** | `.ssh`, `.gitconfig`, `.docker`, `.aws`, `.kube`, `.npmrc`, `.wslconfig`, … |
| **App configs** | VS Code settings/extensions, Windows Terminal, Git Bash |

The data selection step has **basic** (6 items) and **advanced** (all 11 items) modes toggled with `Tab`.

---

## Download & run

**Quickest way — paste into PowerShell:**

```powershell
irm https://raw.githubusercontent.com/pokys/WinMigraThor/main/run.ps1 | iex
```

This downloads the latest `migrathor.exe` to `%TEMP%` and launches it. A UAC prompt will appear — that's expected.

**Or download manually** from the [Releases](https://github.com/pokys/WinMigraThor/releases) page and run as Administrator.

---

## Usage

```
migrathor.exe              Launch main menu (TUI)
migrathor.exe backup       Start backup wizard
migrathor.exe backup -n    Dry-run — show plan without executing
migrathor.exe restore      Start restore wizard
migrathor.exe update       Update migrathor.exe to the latest release
migrathor.exe cleanup      Remove temporary files created by the tool
migrathor.exe version      Print version info
```

### TUI keybindings

| Key | Action |
|---|---|
| `↑` / `↓` or `j` / `k` | Navigate |
| `Space` | Toggle selection |
| `Enter` | Confirm / next step |
| `Esc` | Go back |
| `Tab` | Toggle basic / advanced mode (data step) |
| `a` / `n` | Select all / select none |
| `?` | Help overlay |
| `q` | Quit |
| `Ctrl+C` | Force quit |

---

## Backup wizard

1. **Users** — select Windows user profiles to back up
2. **Data** — pick what to include; `Tab` switches basic ↔ advanced
3. **Options** — enable ZIP compression; optionally delete unzipped folder after zipping
4. **Target path** — choose destination directory
5. **Summary** — review before running
6. **Progress** — per-job progress bars, live warnings
7. **Done** — results summary and log location

Dry-run mode (`--dry-run` / `-n`) runs steps 1–5 and then shows what would happen without touching any files.

---

## Restore wizard

1. **Source** — path to the backup folder (must contain `metadata.json`)
2. **Data** — select which jobs to restore
3. **User mapping** — map source usernames to target profile paths
4. **Conflict strategy** — ask, overwrite, skip, or rename
5. **Progress** — per-job progress bars
6. **App reinstall** *(conditional)* — if app data exists in the backup:
   - **Script mode** — generates `reinstall.ps1` you can review and run
   - **Execute mode** — runs `winget install` directly for each matched app
7. **Done** — summary

---

## Self-update

```
migrathor.exe update
```

Downloads the latest `migrathor.exe` from GitHub, replaces the running binary in-place, and prompts you to restart. The old binary is deleted automatically on success; if anything goes wrong the original is restored.

---

## Backup directory layout

```
backup-folder/
├── metadata.json          # Required by restore wizard
├── config.json            # Selections (users + jobs)
├── logs/
├── users/{username}/      # Desktop, Documents, …
├── browsers/              # chrome/, edge/, firefox/
├── bookmarks/             # *.html exports
├── email/                 # outlook/, thunderbird/
├── wifi/
├── vpn/
├── credentials/
├── certificates/
├── devenv/
├── appconfig/
├── apps.json              # All detected apps
└── apps_winget.json       # Apps matched to winget IDs
```

`metadata.json` records the source hostname, date, OS version, per-job statistics, and total size. The restore wizard validates it before presenting options.

---

## Build

Targets **Windows amd64** only; cross-compile from any platform:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o migrathor.exe .
```

With version metadata:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-X main.version=1.0.0 -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o migrathor.exe .
```

Or use the PowerShell build script on Windows:

```powershell
.\scripts\build.ps1 [-Version "1.2.3"] [-Output "migrathor.exe"] [-Clean]
```

### Releases

GitHub Actions builds `migrathor.exe` automatically:

- **Push to `main`** → rolling `latest` prerelease (always the newest build)
- **Push tag `v*`** → versioned release (e.g. `v1.2.3`)

---

## Requirements

- Windows 10 21H2 (build 19044) or later
- `robocopy.exe` and `netsh.exe` — Windows built-ins, always present
- `winget.exe` — optional, needed for app reinstall (included in Windows 11; installable on Windows 10)
- Administrator privileges

---

## Architecture

```
main.go → cmd/ → internal/jobs/ → internal/engine/ (robocopy)
              ↘ internal/ui/    (Bubble Tea TUI)
```

- **`cmd/`** — orchestrates backup / restore / update / cleanup
- **`internal/jobs/`** — all data migration logic; each job implements `Name`, `Description`, `Scan`, `Backup`, `Restore`
- **`internal/engine/`** — robocopy wrapper with exit-code parsing
- **`internal/ui/`** — Bubble Tea screens (`backupflow.go`, `restoreflow.go`, `updateflow.go`, …)
- **`internal/meta/`** — reads/writes `metadata.json`
- **`internal/checks/`** — validates OS version, admin elevation, required tools
- **`internal/logging/`** — file-only slog loggers (no stderr — would corrupt TUI alt-screen)

All job files and the robocopy engine carry `//go:build windows` guards. Non-Windows stubs allow `go build` on macOS/Linux for syntax checking.
