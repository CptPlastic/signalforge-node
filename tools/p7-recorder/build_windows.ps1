$ErrorActionPreference = "Stop"

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
        [string]$ExecutablePath
    )

    if ([string]::IsNullOrWhiteSpace($env:WINDOWS_CODESIGN_CERT_BASE64)) {
        Write-Host "Windows code signing skipped: WINDOWS_CODESIGN_CERT_BASE64 is not set."
        return
    }

    $signTool = Find-SignTool
    if (-not $signTool) {
        throw "signtool.exe was not found. Install the Windows SDK or run in a GitHub windows-latest runner."
    }

    $certPath = Join-Path ([System.IO.Path]::GetTempPath()) "p7-recorder-codesign.pfx"
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

        $arguments += $ExecutablePath
        & $signTool @arguments
    }
    finally {
        Remove-Item -Force $certPath -ErrorAction SilentlyContinue
    }
}

function Write-BuildInfo {
    $version = $env:P7_RECORDER_VERSION
    if ([string]::IsNullOrWhiteSpace($version)) {
        $version = "dev"
    }

    $repository = $env:P7_RECORDER_REPOSITORY
    $defaultServerURL = $env:P7_RECORDER_DEFAULT_SERVER_URL
    if ([string]::IsNullOrWhiteSpace($defaultServerURL)) {
        $defaultServerURL = "https://p7hub.projectseven.us/"
    }
    $apiURL = ""
    $pageURL = ""
    if (-not [string]::IsNullOrWhiteSpace($repository)) {
        $apiURL = "https://api.github.com/repos/$repository/releases/latest"
        $pageURL = "https://github.com/$repository/releases/latest"
    }

    $content = @(
        "APP_VERSION = $($version | ConvertTo-Json -Compress)",
        "RELEASES_API_URL = $($apiURL | ConvertTo-Json -Compress)",
        "RELEASES_PAGE_URL = $($pageURL | ConvertTo-Json -Compress)",
        "DEFAULT_SERVER_URL = $($defaultServerURL | ConvertTo-Json -Compress)"
    ) -join "`n"
    Set-Content -Path "p7_recorder_build_info.py" -Value $content -Encoding UTF8
}

Push-Location $PSScriptRoot
$originalBuildInfo = $null
try {
    $buildInfoPath = Join-Path $PSScriptRoot "p7_recorder_build_info.py"
    if (Test-Path $buildInfoPath) {
        $originalBuildInfo = Get-Content -Path $buildInfoPath -Raw
    }
    Write-BuildInfo

    if (-not (Test-Path .venv)) {
        python -m venv .venv
    }
    .\.venv\Scripts\python.exe -m pip install --upgrade pip
    .\.venv\Scripts\python.exe -m pip install -r requirements.txt
    .\.venv\Scripts\python.exe -m PyInstaller `
        --noconfirm `
        --clean `
        --windowed `
        --name "P7 Recorder" `
        --add-data "p7_recorder.py;." `
        --add-data "signalforge-icon.svg;." `
        p7_recorder_desktop.py

    Invoke-OptionalCodeSign -ExecutablePath "dist\P7 Recorder\P7 Recorder.exe"
    Write-Host "Built dist\P7 Recorder\P7 Recorder.exe"
}
finally {
    if ($null -ne $originalBuildInfo) {
        Set-Content -Path "p7_recorder_build_info.py" -Value $originalBuildInfo -Encoding UTF8
    }
    Pop-Location
}
