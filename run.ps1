$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = $PSScriptRoot
$proxyProcess = $null
$webProcess = $null

function Assert-Command {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$InstallHint
    )

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "$Name was not found. $InstallHint"
    }
}

function Stop-ProcessTree {
    param([System.Diagnostics.Process]$TargetProcess)

    if ($null -eq $TargetProcess -or $TargetProcess.HasExited) {
        return
    }

    & taskkill.exe /PID $($TargetProcess.Id) /T /F 2>$null | Out-Null
}

Set-Location $repoRoot

Write-Host "Agent Context Probe - Starting services"
Write-Host "======================================="

Assert-Command "go" "Install Go 1.20 or higher and reopen PowerShell."
Assert-Command "node" "Install Node.js 20 or higher and reopen PowerShell."
Assert-Command "npm.cmd" "Install npm with Node.js 20 or higher and reopen PowerShell."

$nodeMajor = [int](& node -p "process.versions.node.split('.')[0]")
if ($nodeMajor -lt 20) {
    throw "Node.js 20 or higher is required. Found: $(& node --version)"
}

if (-not (Test-Path ".env")) {
    if (-not (Test-Path ".env.example")) {
        throw ".env.example was not found."
    }

    Copy-Item ".env.example" ".env"
    Write-Host "Created .env from .env.example."
}

New-Item -ItemType Directory -Force -Path "bin" | Out-Null

Write-Host "Building proxy server..."
Push-Location "proxy"
try {
    & go mod download
    if ($LASTEXITCODE -ne 0) {
        throw "go mod download failed with exit code $LASTEXITCODE."
    }

    & go build -o "..\bin\proxy.exe" ".\cmd\proxy"
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}

if (-not (Test-Path "web\node_modules")) {
    Write-Host "Installing web dependencies..."
    Push-Location "web"
    try {
        & npm.cmd ci
        if ($LASTEXITCODE -ne 0) {
            throw "npm ci failed with exit code $LASTEXITCODE."
        }
    }
    finally {
        Pop-Location
    }
}

try {
    Write-Host "Starting proxy server on port 3001..."
    $proxyProcess = Start-Process `
        -FilePath (Join-Path $repoRoot "bin\proxy.exe") `
        -WorkingDirectory $repoRoot `
        -NoNewWindow `
        -PassThru

    Start-Sleep -Seconds 2
    if ($proxyProcess.HasExited) {
        throw "Proxy server exited with code $($proxyProcess.ExitCode)."
    }

    Write-Host "Starting web dashboard on port 5173..."
    $webProcess = Start-Process `
        -FilePath "npm.cmd" `
        -ArgumentList @("run", "dev") `
        -WorkingDirectory (Join-Path $repoRoot "web") `
        -NoNewWindow `
        -PassThru

    Write-Host ""
    Write-Host "Web Dashboard: http://localhost:5173"
    Write-Host "API Proxy:    http://localhost:3001"
    Write-Host "Health Check: http://localhost:3001/health"
    Write-Host "Press Ctrl+C to stop both services."

    while (-not $proxyProcess.HasExited -and -not $webProcess.HasExited) {
        Start-Sleep -Milliseconds 500
    }

    if ($proxyProcess.HasExited) {
        throw "Proxy server exited with code $($proxyProcess.ExitCode)."
    }

    throw "Web dashboard exited with code $($webProcess.ExitCode)."
}
finally {
    Write-Host "Stopping services..."
    Stop-ProcessTree $webProcess
    Stop-ProcessTree $proxyProcess
}
