$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = $PSScriptRoot
Set-Location $repoRoot

foreach ($command in @("go", "node", "npm.cmd")) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "$command was not found. Go 1.21+ and Node.js 20+ are required to build from source."
    }
}

$nodeMajor = [int](& node -p "process.versions.node.split('.')[0]")
if ($nodeMajor -lt 20) {
    throw "Node.js 20 or newer is required. Found: $(& node --version)"
}

if (-not (Test-Path "web\node_modules")) {
    & npm.cmd --prefix web ci
    if ($LASTEXITCODE -ne 0) {
        throw "npm ci failed with exit code $LASTEXITCODE."
    }
}

& npm.cmd --prefix web run build
if ($LASTEXITCODE -ne 0) {
    throw "web build failed with exit code $LASTEXITCODE."
}

New-Item -ItemType Directory -Force -Path "bin" | Out-Null
Push-Location "proxy"
try {
    & go build -o "..\bin\agent-context-probe.exe" ".\cmd\agent-context-probe"
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}

if (Test-Path "config.yaml") {
    & ".\bin\agent-context-probe.exe" start --config "config.yaml" @args
}
else {
    & ".\bin\agent-context-probe.exe" start @args
}
exit $LASTEXITCODE
