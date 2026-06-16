# SignalForge Trunk Recorder

Headless OKWIN + GMRS trunk recorder for RTL-SDR dongles. Uses [Trunk Recorder](https://github.com/TrunkRecorder/trunk-recorder) for RF decode and uploads calls to SignalForge Hub.

## Command cheat sheet

```bash
sf trk setup          # one-shot setup (install, config, OKWIN data)
sf trk start          # start recording + Hub upload
sf trk chk            # preflight
sf trk dev            # list RTL-SDR dongles
sf trk st             # readiness status
sf trk deps           # install trunk-recorder only
sf trk imp --csv …    # import RadioReference export
sf trk render         # generate trunk-recorder.json
```

Long names still work (`import-rr`, `check`, `devices`, etc.).

## Easiest setup

```bash
sf trk setup -k sk_live_...
sf trk setup --yes    # automatic if you ran sf onboard
```

Config defaults to `~/.config/signalforge/trunk.yaml`.

## Start recording

```bash
sf trk start
```

## RadioReference

- **Starter bundle:** included in `sf trk setup`
- **Full export:** `sf trk imp --csv ~/Downloads/okwin-export.csv --force`
- **Live sync:** `sf trk sync` with RR API credentials

## SDR pool

| N dongles | Roles |
|-----------|-------|
| 1 | control hunt only |
| 2 | control + voice |
| 3+ | control + voice + GMRS (+ extra voice/hunt backup) |

## Hub upload

Uses `POST /api/call-upload` (Rdio Scanner format). Offline clips queue in `upload.queue_directory`.
