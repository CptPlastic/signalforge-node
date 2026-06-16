# SignalForge Trunk Recorder

Headless OKWIN + GMRS trunk recorder for RTL-SDR dongles. Uses [Trunk Recorder](https://github.com/TrunkRecorder/trunk-recorder) for RF decode and uploads calls to SignalForge Hub.

## Easiest setup

```bash
# One command — installs Trunk Recorder, writes config, imports OKWIN starter data
sf trunk setup --source-key sk_live_...

# Fully automatic (uses saved profile from sf onboard if available)
sf trunk setup --yes
```

That saves config to `~/.config/signalforge/trunk.yaml` and bundles OKWIN control channels + sample talkgroups. Replace with your full RadioReference export later:

```bash
sf trunk import-rr --csv ~/Downloads/okwin-export.csv --force
```

### Just install Trunk Recorder

```bash
sf trunk install-deps
```

## Start recording

Plug in RTL-SDR dongles, then:

```bash
sf trunk start
```

Config and flags default to `~/.config/signalforge/trunk.yaml`. Override with `--config` if needed.

## Manual steps (optional)

```bash
sf trunk init
sf trunk import-rr --csv samples/okwin-bundle.csv --force
sf trunk check --source-key sk_live_...
sf trunk start --source-key sk_live_...
```

## What `sf trunk setup` does

1. Installs **trunk-recorder** (Homebrew tap on macOS; build script on Linux)
2. Creates **trunk.yaml** in your SignalForge config folder
3. Imports **built-in OKWIN starter data** (or your `--csv` export)
4. Validates **Hub** credentials
5. Generates **trunk-recorder.json** when dongles are attached

## Decode engine

`decode.engine` defaults to `trunk-recorder`. SignalForge generates `trunk-recorder.json` from `trunk.yaml`, starts the binary, watches `recordings/` for calls, and uploads to Hub.

## SDR pool

| N dongles | Roles |
|-----------|-------|
| 1 | control hunt only |
| 2 | control + voice |
| 3+ | control + voice + GMRS (+ extra voice/hunt backup) |

## Hub upload

Uses `POST /api/call-upload` (Rdio Scanner format). Offline clips queue in `upload.queue_directory`.

## RadioReference

- **Starter bundle:** included in `sf trunk setup` (3 OKWIN sites)
- **Full export:** `sf trunk import-rr --csv <premium-export.csv>`
- **Live sync:** `sf trunk sync-rr` with RR API credentials
