# SignalForge CLI

SignalForge CLI is the cross-platform operator companion for SignalForge Hub. It is built in Go and targets Windows, macOS, and Linux.

The first slices focus on making recorder setup less mysterious and moving the recorder experience into one signed terminal binary:

- hub health and version checks
- recorder source-key probe checks
- recorder input inspection for files and folders
- single-file audio upload through the same hub ingest API used by recorders
- folder watch mode that uploads stable audio files and moves them to a processed folder
- Bubble Tea terminal dashboard styled as a SignalForge recorder console

The intent is for this CLI/TUI to become the clean operator surface for recorder work: point it at an audio file, folder, stream, or eventually a capture device; validate the config; process audio; and upload to the hub without installing a separate desktop recorder app.

## Build

```powershell
go build -o dist/signalforge.exe ./cmd/signalforge
Copy-Item dist/signalforge.exe dist/sf.exe
```

```bash
go build -o dist/signalforge ./cmd/signalforge
cp dist/signalforge dist/sf
```

## Install

macOS and Linux with Homebrew:

```bash
brew tap CptPlastic/signalforge
brew install signalforge
```

Windows with Scoop:

```powershell
scoop bucket add signalforge https://github.com/CptPlastic/scoop-signalforge
scoop install signalforge
```

Release archives are also attached to the public `signalforge-cli-v*` releases at <https://github.com/CptPlastic/signalforge.org/releases>.

Homebrew, Scoop, and release archives provide both `signalforge` and the short `sf` alias.

## Onboard

Interactive setup for hub URL, source key, monitor folder, processed output, timers, and canary heartbeat:

```bash
sf onboard
```

This saves a profile under your user config directory (`~/.config/signalforge/` on Linux, `~/Library/Application Support/signalforge/` on macOS) plus a `config.toml` compatible with `p7-recorder-go`.

After onboarding, `sf rec w` and `sf tui` pick up saved folder paths automatically. Use `sf onboard --show` to review the saved profile.

Non-interactive example:

```bash
sf onboard --yes --hub-url https://p7hub.projectseven.us -k sk_live_... \
  --folder --input ./ingest --canary --canary-interval 5m --install-service
```

### Background service

After onboarding, install a user-level service so folder watch keeps running across reboots:

```bash
sf onboard --install-service   # during setup
sf service install             # later, from saved profile
sf service status
sf service uninstall
```

- **Linux**: systemd user unit (`~/.config/systemd/user/signalforge-watch.service`)
- **macOS**: launchd agent (`~/Library/LaunchAgents/org.signalforge.watch.plist`)
- Logs: config dir `logs/watch.log`

## Configure

You can pass flags each time:

```powershell
.\signalforge.exe --hub-url https://p7hub.projectseven.us hub check
```

Or use environment variables:

```powershell
$env:SIGNALFORGE_HUB_URL = "https://p7hub.projectseven.us"
$env:SIGNALFORGE_SOURCE_KEY = "sk_live_REPLACE_WITH_SOURCE_KEY"
```

```bash
export SIGNALFORGE_HUB_URL=https://p7hub.projectseven.us
export SIGNALFORGE_SOURCE_KEY=sk_live_REPLACE_WITH_SOURCE_KEY
```

## Commands

```powershell
signalforge hub check
signalforge recorder check --source-key sk_live_REPLACE_WITH_SOURCE_KEY
signalforge recorder inspect --input ./calls
signalforge recorder upload --input ./calls/example.wav --source-key sk_live_REPLACE_WITH_SOURCE_KEY
signalforge recorder watch --input ./calls --source-key sk_live_REPLACE_WITH_SOURCE_KEY
signalforge recorder watch --input ./calls --once --source-key sk_live_REPLACE_WITH_SOURCE_KEY
signalforge recorder tui --input ./calls --source-key sk_live_REPLACE_WITH_SOURCE_KEY
signalforge tui --input ./calls --source-key sk_live_REPLACE_WITH_SOURCE_KEY
signalforge version
signalforge update check
```

Top-level command aliases are available for faster terminal use:

```powershell
sf h chk
sf rec i -i ./calls
sf rec w -i ./calls -k sk_live_REPLACE_WITH_SOURCE_KEY
sf trk setup -k sk_live_REPLACE_WITH_SOURCE_KEY
sf trk chk
sf trk start
sf upd chk
sf ver
signalforge h chk
signalforge rec i -i ./calls
signalforge rec w -i ./calls -k sk_live_REPLACE_WITH_SOURCE_KEY
signalforge trk setup -k sk_live_REPLACE_WITH_SOURCE_KEY
signalforge upd chk
signalforge ver
signalforge -v
signalforge --v
signalforge --version
```

Common shorthand flags include `-k` for `--source-key`, `-i` for `--input`, `-p` for `--processed`, `-s` for `--stable`, and `-o` for `--once`.

`recorder check` probes `POST /api/call-upload` with `test=1`. A valid source key returns the expected SDRTrunk-style incomplete-call response, which the CLI reports as `source key ok`.

`recorder inspect` accepts a file or folder and reports whether SignalForge can ingest the audio. Supported file extensions are `.wav`, `.mp3`, `.m4a`, and `.flac`.

`recorder upload` uploads one supported audio file with recorder metadata. Metadata can be overridden with flags such as `--system`, `--system-label`, `--talkgroup`, `--talkgroup-label`, `--talkgroup-group`, `--talkgroup-tag`, and `--frequency`.

`recorder watch` runs a folder-ingest loop. It waits until audio files are stable, uploads them, then moves them to a processed folder. Useful flags:

- `--processed processed` sets the destination folder. Relative paths are resolved under `--input`.
- `--poll 1s` controls how often the folder is checked.
- `--stable 2500ms` controls how old a file must be before upload.
- `--reprocess` uploads files without moving them.
- `--once` processes the current ready batch and exits.

The TUI uses Bubble Tea for the SignalForge recorder console: hub health, version, source-key status, recorder input readiness, metadata preview, and a one-key upload action for file inputs or the current ready folder batch. Stream input and live capture are the next runtime layers to add under the same interface.

## Updates

Release builds are stamped with version, commit, and build date metadata. Check the installed binary with:

```powershell
signalforge version
signalforge update check
```

Normal commands perform a quiet automatic update check at most once per day. If a newer GitHub release exists, the CLI prints a short notice with the matching platform asset or release URL. To disable automatic checks for scripts or locked-down installs:

```powershell
$env:SIGNALFORGE_NO_UPDATE_CHECK = "1"
```

```bash
export SIGNALFORGE_NO_UPDATE_CHECK=1
```

For staging or test release feeds, set `SIGNALFORGE_UPDATE_URL` to a GitHub-compatible releases-list JSON endpoint. A single latest-release JSON object is also accepted for tests.
