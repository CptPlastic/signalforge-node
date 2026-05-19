$ErrorActionPreference = "Stop"

$version = $env:P7_RECORDER_UI_VERSION
if ([string]::IsNullOrWhiteSpace($version)) {
    $version = "dev"
}

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$recorderRoot = Join-Path $repoRoot "tools\p7-recorder-go"

function Find-SignTool {
    $command = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($command) {
        return $command.Source
    }

    $kitsRoot = "${env:ProgramFiles(x86)}\Windows Kits\10\bin"
    if (-not (Test-Path $kitsRoot)) {
        return $null
    }

    return Get-ChildItem -Path $kitsRoot -Filter signtool.exe -Recurse |
        Where-Object { $_.FullName -match "\\x64\\signtool\.exe$" } |
        Sort-Object FullName -Descending |
        Select-Object -ExpandProperty FullName -First 1
}

function Invoke-OptionalCodeSign {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    if ($env:WINDOWS_CODESIGN_ENABLED -ne "true") {
        Write-Host "Windows code signing skipped: WINDOWS_CODESIGN_ENABLED is not true."
        return
    }
    if ([string]::IsNullOrWhiteSpace($env:WINDOWS_CODESIGN_CERT_BASE64)) {
        throw "WINDOWS_CODESIGN_ENABLED is true, but WINDOWS_CODESIGN_CERT_BASE64 is not set."
    }

    $signTool = Find-SignTool
    if (-not $signTool) {
        throw "signtool.exe was not found. Install the Windows SDK or run in a GitHub windows-latest runner."
    }

    $certPath = Join-Path ([System.IO.Path]::GetTempPath()) "p7-recorder-ui-codesign.pfx"
    try {
        [System.IO.File]::WriteAllBytes($certPath, [Convert]::FromBase64String($env:WINDOWS_CODESIGN_CERT_BASE64))
        $arguments = @(
            "sign",
            "/fd", "SHA256",
            "/td", "SHA256",
            "/tr", "http://timestamp.digicert.com",
            "/f", $certPath
        )
        if (-not [string]::IsNullOrWhiteSpace($env:WINDOWS_CODESIGN_CERT_PASSWORD)) {
            $arguments += @("/p", $env:WINDOWS_CODESIGN_CERT_PASSWORD)
        }
        $arguments += $Path
        & $signTool @arguments
        if ($LASTEXITCODE -ne 0) {
            throw "signtool failed for $Path with exit code $LASTEXITCODE"
        }
    }
    finally {
        Remove-Item -Force $certPath -ErrorAction SilentlyContinue
    }
}

Push-Location $PSScriptRoot
try {
    $wails = Get-Command wails -ErrorAction SilentlyContinue
    if ($null -eq $wails) {
        throw "wails CLI not found. Install with: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
    }
    $makensis = Get-Command makensis -ErrorAction SilentlyContinue
    if ($null -eq $makensis) {
        throw "makensis not found. Install NSIS or add makensis to PATH before building the installer."
    }

    Push-Location $recorderRoot
    try {
        & .\build_windows.ps1
        if ($LASTEXITCODE -ne 0) {
            throw "p7-recorder-go build failed with exit code $LASTEXITCODE"
        }
        Invoke-OptionalCodeSign -Path "dist\p7-recorder-go.exe"
    }
    finally {
        Pop-Location
    }

    wails build --nsis -ldflags "-s -w -X main.version=$version"
    if ($LASTEXITCODE -ne 0) {
        throw "wails build failed with exit code $LASTEXITCODE"
    }
    $installer = Get-ChildItem -Path "build\bin" -Filter "*-installer.exe" | Select-Object -First 1
    if ($null -eq $installer) {
        throw "wails build completed, but no NSIS installer was produced"
    }
    Invoke-OptionalCodeSign -Path "build\bin\p7-recorder-ui.exe"
    Invoke-OptionalCodeSign -Path $installer.FullName
    Copy-Item (Join-Path $recorderRoot "dist\p7-recorder-go.exe") "build\bin\p7-recorder-go.exe" -Force
    Copy-Item (Join-Path $recorderRoot "config.example.toml") "build\bin\config.example.toml" -Force
    Copy-Item "README.md" "build\bin\README.md" -Force
    Write-Host "Built build\bin\p7-recorder-ui.exe and NSIS installer with p7-recorder-go.exe sidecar"
}
finally {
    Pop-Location
}