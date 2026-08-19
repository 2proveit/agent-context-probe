param(
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\Agent Context Probe"
)

$ErrorActionPreference = "Stop"
$binary = Join-Path $InstallDir "agent-context-probe.exe"
if (Test-Path $binary -PathType Leaf) {
    Remove-Item $binary -Force
    Write-Host "Removed $binary"
}
else {
    Write-Host "Agent Context Probe is not installed at $binary"
}
Write-Host "Configuration, request history, and database backups were preserved."
