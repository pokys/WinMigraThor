# WinMigraThor Build Script
# Usage: .\scripts\build.ps1 [-Version "1.2.3"] [-Output "migrator.exe"]

param(
    [string]$Version = "1.0.0",
    [string]$Output = "migrator.exe",
    [switch]$Clean
)

$ErrorActionPreference = "Stop"

$BuildDate = Get-Date -Format "yyyy-MM-dd"
$LdFlags = "-s -w -X main.version=$Version -X main.buildDate=$BuildDate"

Write-Host "=== WinMigraThor Build ===" -ForegroundColor Cyan
Write-Host "Version:    $Version"
Write-Host "Build date: $BuildDate"
Write-Host "Output:     $Output"
Write-Host ""

# Clean
if ($Clean) {
    Write-Host "Cleaning..." -ForegroundColor Yellow
    Remove-Item -ErrorAction SilentlyContinue $Output
}

# Ensure dependencies
Write-Host "Downloading dependencies..." -ForegroundColor Yellow
go mod download
if ($LASTEXITCODE -ne 0) { exit 1 }

# Build for Windows amd64
Write-Host "Building..." -ForegroundColor Yellow
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

go build -ldflags $LdFlags -o $Output .

if ($LASTEXITCODE -ne 0) {
    Write-Host "Build FAILED!" -ForegroundColor Red
    exit 1
}

$size = (Get-Item $Output).Length
$sizeMB = [math]::Round($size / 1MB, 2)
Write-Host ""
Write-Host "Build successful!" -ForegroundColor Green
Write-Host "Output: $Output ($sizeMB MB)"
