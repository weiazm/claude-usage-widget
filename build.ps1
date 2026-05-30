# Builds the tray widget as a GUI exe (no console window).
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    # -s -w strips symbols/DWARF (smaller exe); -trimpath removes local paths;
    # -H windowsgui hides the console window.
    go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
    Write-Host "Built claude-usage-widget.exe" -ForegroundColor Green
} finally {
    Pop-Location
}
