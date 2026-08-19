param(
    [Parameter(Mandatory = $true)]
    [string]$ArchivePath
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path $PSScriptRoot -Parent
$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("agent-context-probe-lifecycle-" + [guid]::NewGuid())
$installDir = Join-Path $testRoot "bin"
$env:APPDATA = Join-Path $testRoot "roaming"
$env:LOCALAPPDATA = Join-Path $testRoot "local"
$env:USERPROFILE = Join-Path $testRoot "home"
$env:PATH = "$env:SystemRoot\System32;$env:SystemRoot"
New-Item -ItemType Directory -Force -Path $testRoot | Out-Null

try {
    foreach ($tool in @("go", "node", "npm")) {
        if (Get-Command $tool -ErrorAction SilentlyContinue) {
            throw "Clean lifecycle environment unexpectedly contains $tool"
        }
    }

    & "$PSScriptRoot\install.ps1" -ArchivePath $ArchivePath -InstallDir $installDir
    $binary = Join-Path $installDir "agent-context-probe.exe"
    $versionOutput = & $binary version
    if (-not ($versionOutput | Select-String "agent-context-probe")) {
        throw "Installed binary did not report its version"
    }
    if ($versionOutput -match "agent-context-probe dev") {
        throw "Release binary contains the development version"
    }

    function Start-And-Check([int]$Port) {
        $process = Start-Process -FilePath $binary -ArgumentList @("start", "--port", "$Port") -PassThru -NoNewWindow
        try {
            $ready = $false
            foreach ($attempt in 1..30) {
                try {
                    $response = Invoke-WebRequest "http://127.0.0.1:$Port/health" -UseBasicParsing
                    if ($response.StatusCode -eq 200) {
                        $ready = $true
                        break
                    }
                }
                catch {
                    Start-Sleep -Seconds 1
                }
            }
            if (-not $ready) {
                throw "Server did not become healthy on port $Port"
            }
        }
        finally {
            if (-not $process.HasExited) {
                Stop-Process -Id $process.Id -Force
                $process.WaitForExit()
            }
        }
    }

    Start-And-Check 39111
    $database = Join-Path $env:LOCALAPPDATA "Agent Context Probe\requests.db"
    if (-not (Test-Path $database -PathType Leaf)) {
        throw "Standard Windows database was not created"
    }

    Set-Content -Path $binary -Value "broken executable"
    & "$PSScriptRoot\install.ps1" -ArchivePath $ArchivePath -InstallDir $installDir
    if (-not (& $binary version | Select-String "agent-context-probe")) {
        throw "Upgrade did not replace the binary"
    }
    Start-And-Check 39112

    & "$PSScriptRoot\uninstall.ps1" -InstallDir $installDir
    if (Test-Path $binary) {
        throw "Uninstall did not remove the binary"
    }
    if (-not (Test-Path $database -PathType Leaf)) {
        throw "Uninstall removed user data"
    }
    Write-Host "Release lifecycle test passed without Go, Node.js, or npm."
}
finally {
    Remove-Item $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}
