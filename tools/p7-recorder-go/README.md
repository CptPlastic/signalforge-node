# P7 Recorder Go

P7 Recorder Go is the replacement recorder core for the Python/PyInstaller recorder. It records local input audio with VOX detection, writes each call to a local queue, and uploads queued WAV clips to SignalForge Hub using the existing `/api/call-upload` endpoint.

## Build

```powershell
go build -o dist/p7-recorder-go.exe ./cmd/p7-recorder-go
```

```bash
go build -o dist/p7-recorder-go ./cmd/p7-recorder-go
```

## Run

```powershell
.\p7-recorder-go.exe --config config.toml --init-config
.\p7-recorder-go.exe --config config.toml --list-devices
.\p7-recorder-go.exe --config config.toml
```

Use `--init-config --force` only when you want to overwrite an existing config file.

The config format matches the existing Python recorder so users can migrate by copying the same server URL, source key, audio thresholds, and metadata.

## Folder ingest

Set `[folder_ingest].enabled = true` to watch a folder for completed `.wav`, `.mp3`, `.m4a`, or `.flac` files instead of recording microphone audio. The recorder waits until a file is stable, queues it for upload, and moves successfully uploaded files into the processed folder.

Set `reprocess_processed = true` to replay files from the processed folder once per recorder run. That mode leaves the originals in place and is intended for canary checks, alert tests, or demos.
