# P7 Recorder UI

P7 Recorder UI is the Wails desktop shell for the Go recorder. It gives operators a console-style setup surface for the same `p7-recorder-go` core.

Current controls:

- save and load `config.toml`
- list recorder audio devices
- start and stop the recorder process
- view recorder logs
- open P7, recorder downloads, feedback, and donate links

## Development

Install Wails once:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Run the app from this directory:

```powershell
wails dev
```

The development UI falls back to `go run ../p7-recorder-go/cmd/p7-recorder-go` when a `p7-recorder-go.exe` sidecar is not beside the UI executable.

## Build

```powershell
.\build_windows.ps1
```

The Windows build output is:

```text
build\bin\p7-recorder-ui.exe
build\bin\p7-recorder-go.exe
build\bin\config.example.toml
```

The UI expects the recorder executable to sit beside it in packaged builds.

## macOS signing and notarization

Unsigned macOS `.pkg` releases will show Apple's "could not verify this is free of malware" Gatekeeper warning. To publish a trusted package, replace the placeholder GitHub Actions secrets in `projectseven-co-ltd/p7-scanner`, set `APPLE_SIGNING_ENABLED` to `true`, then tag a `ui-v*` release:

- `APPLE_SIGNING_ENABLED`
- `APPLE_DEVELOPER_ID_APPLICATION_CERT_BASE64`
- `APPLE_DEVELOPER_ID_APPLICATION_CERT_PASSWORD`
- `APPLE_DEVELOPER_ID_INSTALLER_CERT_BASE64`
- `APPLE_DEVELOPER_ID_INSTALLER_CERT_PASSWORD`
- `APPLE_ID`
- `APPLE_APP_SPECIFIC_PASSWORD`
- `APPLE_TEAM_ID`

Optional identity override secrets, if the keychain has more than one matching certificate:

- `APPLE_DEVELOPER_ID_APPLICATION_IDENTITY`
- `APPLE_DEVELOPER_ID_INSTALLER_IDENTITY`

Create the certificate secrets from exported `.p12` Developer ID Application and Developer ID Installer certificates:

```sh
base64 -i developer-id-application.p12 | pbcopy
base64 -i developer-id-installer.p12 | pbcopy
```

When those secrets are present, the release workflow signs the app bundle, signs the installer package, submits it to Apple notarization, staples the notarization ticket, and uploads the stapled `.pkg`.

## Windows signing

Unsigned Windows installers may show SmartScreen warnings. To sign Windows releases, replace the placeholder GitHub Actions secrets, set `WINDOWS_CODESIGN_ENABLED` to `true`, then tag a `ui-v*` release:

- `WINDOWS_CODESIGN_ENABLED`
- `WINDOWS_CODESIGN_CERT_BASE64`
- `WINDOWS_CODESIGN_CERT_PASSWORD`

Create the certificate secret from an exported `.pfx` code-signing certificate:

```powershell
[Convert]::ToBase64String([IO.File]::ReadAllBytes("C:\path\to\codesign.pfx")) | Set-Clipboard
```

When enabled, the release workflow signs the recorder sidecar, the Wails UI executable, and the NSIS setup executable.
