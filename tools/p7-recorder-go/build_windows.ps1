$ErrorActionPreference = "Stop"

$version = $env:P7_RECORDER_GO_VERSION
if ([string]::IsNullOrWhiteSpace($version)) {
    $version = "dev"
}

Push-Location $PSScriptRoot
try {
    New-Item -ItemType Directory -Force -Path dist | Out-Null
    $env:CGO_ENABLED = "1"
    go build -trimpath -ldflags "-s -w -X main.version=$version" -o "dist\p7-recorder-go.exe" .\cmd\p7-recorder-go
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
    Copy-Item config.example.toml "dist\config.example.toml" -Force
    Copy-Item README.md "dist\README.md" -Force
    Write-Host "Built dist\p7-recorder-go.exe"
}
finally {
    Pop-Location
}