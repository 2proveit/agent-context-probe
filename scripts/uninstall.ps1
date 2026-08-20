param(
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\Agent Context Probe",
    [switch]$SkipPathUpdate
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
if (-not $SkipPathUpdate -and (Test-Path (Join-Path $InstallDir ".agent-context-probe-path-added") -PathType Leaf)) {
    $normalizedInstallDir = $InstallDir.TrimEnd('\')
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $remainingEntries = @($userPath -split ';' | Where-Object {
        $_ -and $_.TrimEnd('\') -ine $normalizedInstallDir
    })
    [Environment]::SetEnvironmentVariable("Path", ($remainingEntries -join ';'), "User")
    $env:PATH = (($env:PATH -split ';' | Where-Object {
        $_ -and $_.TrimEnd('\') -ine $normalizedInstallDir
    }) -join ';')
    Remove-Item (Join-Path $InstallDir ".agent-context-probe-path-added") -Force
}
Write-Host "Configuration, request history, and database backups were preserved."
