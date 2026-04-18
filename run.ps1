# WinMigraThor - quick launcher
# Usage: irm https://raw.githubusercontent.com/pokys/WinMigraThor/main/run.ps1 | iex

$url  = "https://github.com/pokys/WinMigraThor/releases/download/latest/migrathor.exe"
$dest = "$env:TEMP\migrathor.exe"

Write-Host "Downloading MigraThor..." -ForegroundColor Cyan
Invoke-RestMethod -Uri $url -OutFile $dest

Write-Host "Starting MigraThor (UAC prompt may appear)..." -ForegroundColor Cyan
Start-Process -FilePath $dest -Verb RunAs
