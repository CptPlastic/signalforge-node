# P7 Recorder Agent

The P7 Recorder Agent records local audio with voice activation and uploads each detected call to SignalForge Hub through the existing `/api/call-upload` endpoint.

The replacement recorder is the Go implementation in `tools/p7-recorder-go`. It uses the same config format and upload contract as the Python recorder, but packages as a smaller native binary for Windows, macOS, and Linux. The Python/PyInstaller recorder remains in `tools/p7-recorder` only while the Go path is being completed.

Use it for sources that are not SDRTrunk, such as:

- GMRS radio speaker/headphone audio
- scanner line-out audio
- a dispatch receiver connected through USB audio
- a permitted Zello or soft-radio client audio output routed into a recorder input

The recorder runs beside the audio source. P7 only receives finished clips and metadata.

## How It Works

1. Create an integration/source in P7.
2. Generate a source API key.
3. Configure the recorder with that key and the server URL.
4. Pick an audio input device.
5. The recorder watches RMS audio level.
6. When audio rises above the configured threshold, it starts a call.
7. When audio stays quiet long enough, it writes a WAV clip.
8. It uploads queued clips to P7.

If P7 is offline, clips stay in the local queue directory and upload later.

## Hardware Examples

### GMRS Radio

A simple setup is:

```text
GMRS radio speaker/headphone out -> USB audio input -> Windows mini PC / Raspberry Pi -> P7 Recorder Agent
```

Start with low radio volume and raise it until calls trigger reliably without clipping.

### Zello

The safest integration is user-controlled audio capture:

```text
Zello client audio output -> virtual audio cable / loopback input -> P7 Recorder Agent
```

Only record channels you are allowed to record. Do not reverse engineer or bypass Zello access controls.

## Install The Go Recorder

Download the current `p7-recorder-go` package for your platform from SignalForge, then extract it into a folder you control.

Create a config with the built-in setup helper:

```powershell
.\p7-recorder-go.exe --config config.toml --init-config
```

On macOS or Linux:

```bash
./p7-recorder-go --config config.toml --init-config
```

The setup helper lists audio devices, asks for the P7 server URL, source API key, audio device, VOX threshold, and basic metadata, then writes `config.toml`.

Use `--init-config --force` only when you want to overwrite an existing config.

## Configure

The source key should come from the P7 Integrations screen. Do not commit a real `config.toml` with a source key.

You can edit the generated `config.toml` directly:

```toml
[p7]
base_url = "https://p7scan.projectseven.us/"
source_key = "sk_live_your_generated_source_key"

[metadata]
system_label = "GMRS"
talkgroup = 18
talkgroup_label = "GMRS Channel 18"
talkgroup_group = "GMRS"
frequency = 462625000
```

## Pick An Audio Device

```powershell
.\p7-recorder-go.exe --config config.toml --list-devices
```

Set `audio.device` in `config.toml` to the numeric device index or device name.

## Run

```powershell
.\p7-recorder-go.exe --config config.toml
```

On macOS or Linux:

```bash
./p7-recorder-go --config config.toml
```

The recorder prints when voice starts, when a clip is queued, and when upload succeeds.

If P7 is offline, clips stay in the local queue directory and upload later.

## Desktop UI

The Wails desktop shell lives in `tools/p7-recorder-ui`. It is the new UI path for the Go recorder and currently provides:

- config editing and saving
- audio device listing
- start/stop controls
- recorder log output
- quick links for P7, downloads, feedback, and donate

For local development:

```powershell
cd tools\p7-recorder-ui
wails dev
```

For a Windows build:

```powershell
cd tools\p7-recorder-ui
.\build_windows.ps1
```

Packaged UI builds expect `p7-recorder-go.exe` to sit beside `p7-recorder-ui.exe`. The build script creates that layout.

## Legacy Python Recorder

The older Python/PyInstaller recorder remains available while the Go recorder gets its final service/GUI layer.

### Install

From the repo root:

```powershell
cd tools\p7-recorder
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
Copy-Item config.example.toml config.toml
```

On Linux/Raspberry Pi:

```bash
cd tools/p7-recorder
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
cp config.example.toml config.toml
```

### Configure

Edit `config.toml`:

```toml
[p7]
base_url = "https://scanner.example.com/"
source_key = "sk_live_your_generated_source_key"

[metadata]
system_label = "GMRS"
talkgroup = 18
talkgroup_label = "GMRS Channel 18"
talkgroup_group = "GMRS"
frequency = 462625000
```

The source key should come from the P7 Integrations screen. Do not commit a real `config.toml` with a source key.

### Pick An Audio Device

```powershell
python p7_recorder.py --list-devices
```

Set `audio.device` in `config.toml` to the numeric device index or device name.

### Run

```powershell
python p7_recorder.py --config config.toml
```

The recorder prints when voice starts, when a clip is queued, and when upload succeeds.

### Desktop App

The recorder also has a small desktop wrapper for easier setup:

```powershell
cd tools\p7-recorder
python -m venv .venv
.\.venv\Scripts\Activate.ps1
pip install -r requirements.txt
python p7_recorder_desktop.py
```

The desktop app provides:

- server URL and source key fields
- audio input device picker
- threshold and silence controls
- GMRS/channel metadata fields
- packaged version and update status
- direct downloads/update release button
- start/stop buttons
- live recorder log

Release builds default the server URL to `https://p7scan.projectseven.us/`. Local development builds can still point at `http://localhost:8080/` by changing the Server URL field before saving.

It stores its config under the current user's local app data folder, for example:

```text
%LOCALAPPDATA%\P7 Recorder\config.toml
```

### Build A Windows EXE

From PowerShell:

```powershell
cd tools\p7-recorder
.\build_windows.ps1
```

The build output is:

```text
tools\p7-recorder\dist\P7 Recorder\P7 Recorder.exe
```

This is a portable app folder. A later installer can wrap this output with Start menu shortcuts and run-at-startup options.

The Windows EXE is Authenticode-signed during release builds when these GitHub Secrets are configured:

```text
WINDOWS_CODESIGN_CERT_BASE64
WINDOWS_CODESIGN_CERT_PASSWORD
```

`WINDOWS_CODESIGN_CERT_BASE64` should contain the base64-encoded contents of a `.pfx` code-signing certificate. If those secrets are not set, the build still succeeds but the EXE is unsigned, and Windows SmartScreen may require choosing More info -> Run anyway on first launch.

To create the base64 secret value from PowerShell:

```powershell
[Convert]::ToBase64String([IO.File]::ReadAllBytes("C:\path\to\codesign.pfx")) | Set-Clipboard
```

For an unsigned local download, Windows may also mark the ZIP or EXE as downloaded from the internet. This removes that local block:

```powershell
Unblock-File ".\P7 Recorder.exe"
```

### Build A macOS App

From a Mac terminal:

```bash
cd tools/p7-recorder
bash ./build_macos.sh
```

The build output is:

```text
tools/p7-recorder/dist/P7 Recorder.app
```

The macOS recorder depends on PortAudio for local audio capture. On a development Mac, install it with Homebrew if needed:

```bash
brew install portaudio
```

The generated app is ad-hoc signed but not Apple-notarized yet, so macOS may require approving it before first launch. If Finder blocks it, use one of these options:

```bash
xattr -dr com.apple.quarantine "/Applications/P7 Recorder.app"
open "/Applications/P7 Recorder.app"
```

Or Control-click the app in Finder, choose Open, then approve it from Privacy & Security if macOS prompts.

### Build A Linux App

From a Linux terminal:

```bash
cd tools/p7-recorder
bash ./build_linux.sh
```

The build output is:

```text
tools/p7-recorder/dist/P7 Recorder/P7 Recorder
```

The Linux recorder depends on PortAudio for local audio capture. On Debian/Ubuntu, install it with:

```bash
sudo apt-get update
sudo apt-get install -y libportaudio2 portaudio19-dev
```

### GitHub Release Build

The `p7-recorder-release` GitHub Actions workflow builds the Windows, macOS, and Linux recorder packages when a version tag is pushed:

```powershell
git tag v1.2.3
git push origin v1.2.3
```

That creates or updates the GitHub Release and attaches ZIP files named like this:

```text
p7-recorder-windows-v1.2.3.zip
p7-recorder-macos-v1.2.3.zip
p7-recorder-linux-v1.2.3.tar.gz
```

The workflow also runs when a GitHub Release is published manually. It can be started manually from the Actions tab too; manual runs upload the ZIP as a workflow artifact, which is useful for testing the package before publishing a release.

Release builds stamp the tag, SignalForge release URLs, and default SignalForge Hub server URL into the desktop app. The app shows its current version, checks the repository's latest GitHub Release, and keeps a separate downloads button available so users can always open the public release page.

Packaged builds can be checked without launching the GUI:

```powershell
".\P7 Recorder.exe" --print-build-info
".\P7 Recorder.exe" --print-build-info build-info.json
```

## Tuning

Use these values in `config.toml`:

- `threshold`: higher means less sensitive; lower means more sensitive.
- `silence_ms`: quiet time before a call ends.
- `pre_roll_ms`: audio kept from just before voice detection.
- `min_duration_ms`: short bursts below this are discarded.
- `max_duration_sec`: force-close very long transmissions.

For noisy radios, raise `threshold` and/or increase squelch on the radio.

## Metadata Mapping

The recorder uploads fields P7 already understands:

```text
system/systemLabel       -> source family, such as GMRS
 talkgroup/talkgroupLabel -> channel or repeater name
 talkgroupGroup           -> GMRS, Zello, Repeater, etc.
 frequency                -> Hz, if known
 dateTime                 -> call start time
 duration                 -> recorded clip duration
 audio                    -> WAV file
```

## Current Limitations

- One recorder process maps to one metadata profile.
- It does not decode CTCSS/DCS, DTMF, caller ID, or radio unit IDs.
- Zello support is audio-capture based, not a Zello API integration.
- Clips are uploaded as WAV, so storage can grow quickly on busy feeds.

Future versions can add a tray app, multi-channel profiles, automatic device setup, compressed audio, and source health details in the P7 UI.
