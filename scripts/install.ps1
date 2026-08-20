param(
    [string]$Version = "latest",
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\Agent Context Probe",
    [string]$ArchivePath = "",
    [string]$ChecksumsPath = "",
    [switch]$SkipPathUpdate
)

$ErrorActionPreference = "Stop"
$Repository = if ($env:ACP_REPOSITORY) { $env:ACP_REPOSITORY } else { "2proveit/agent-context-probe" }
$temporaryDir = Join-Path ([System.IO.Path]::GetTempPath()) ("agent-context-probe-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $temporaryDir | Out-Null

try {
    if (-not $ArchivePath) {
        if ($Version -eq "latest") {
            $release = Invoke-RestMethod "https://api.github.com/repos/$Repository/releases/latest"
            $Version = $release.tag_name
        }
        $tag = if ($Version.StartsWith("v")) { $Version } else { "v$Version" }
        $releaseVersion = $tag.Substring(1)
        $archiveName = "agent-context-probe_${releaseVersion}_windows_x86_64.zip"
        $ArchivePath = Join-Path $temporaryDir $archiveName
        $ChecksumsPath = Join-Path $temporaryDir "agent-context-probe_checksums.txt"
        $releaseUrl = "https://github.com/$Repository/releases/download/$tag"
        Invoke-WebRequest "$releaseUrl/$archiveName" -OutFile $ArchivePath
        Invoke-WebRequest "$releaseUrl/agent-context-probe_checksums.txt" -OutFile $ChecksumsPath
    }

    if (-not (Test-Path $ArchivePath -PathType Leaf)) {
        throw "Release archive does not exist: $ArchivePath"
    }
    if ($ChecksumsPath) {
        $archiveName = Split-Path $ArchivePath -Leaf
        $entry = Get-Content $ChecksumsPath | Where-Object { $_ -match "\s+$([regex]::Escape($archiveName))$" } | Select-Object -First 1
        if (-not $entry) {
            throw "Checksum entry is missing for $archiveName"
        }
        $expected = ($entry -split "\s+")[0].ToLowerInvariant()
        $actual = (Get-FileHash -Algorithm SHA256 $ArchivePath).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            throw "Checksum verification failed for $archiveName"
        }
    }

    $extractDir = Join-Path $temporaryDir "archive"
    Expand-Archive -Path $ArchivePath -DestinationPath $extractDir
    $binary = Get-ChildItem $extractDir -Recurse -Filter "agent-context-probe.exe" | Select-Object -First 1
    if (-not $binary) {
        throw "The archive does not contain agent-context-probe.exe"
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    $staged = Join-Path $InstallDir "agent-context-probe.new.exe"
    Copy-Item $binary.FullName $staged -Force
    Move-Item $staged (Join-Path $InstallDir "agent-context-probe.exe") -Force
    Write-Host "Installed $(Join-Path $InstallDir 'agent-context-probe.exe')"
    Write-Host "Configuration and data will use the operating system standard user directories."
    if (-not $SkipPathUpdate) {
        $normalizedInstallDir = $InstallDir.TrimEnd('\')
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $pathEntries = @($userPath -split ';' | Where-Object { $_ })
        $alreadyPresent = $pathEntries | Where-Object { $_.TrimEnd('\') -ieq $normalizedInstallDir }
        if (-not $alreadyPresent) {
            $newUserPath = (@($pathEntries) + $InstallDir) -join ';'
            [Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
            New-Item -ItemType File -Force -Path (Join-Path $InstallDir ".agent-context-probe-path-added") | Out-Null
            Write-Host "Added $InstallDir to the user PATH. Open a new terminal to use it."
        }
        if (-not (($env:PATH -split ';') | Where-Object { $_.TrimEnd('\') -ieq $normalizedInstallDir })) {
            $env:PATH = "$InstallDir;$env:PATH"
        }
    }
}
finally {
    Remove-Item $temporaryDir -Recurse -Force -ErrorAction SilentlyContinue
}
