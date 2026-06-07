# SignalForge Field Handheld

Low-power WiFi field unit: **monitor** live calls from your hub and **PTT** transmit on a radio set. Firmware for ESP32 / ESP32-S3 + SSD1306 OLED + I2S speaker/mic.

This is a purpose-built mobile unit — not a phone app. It uses the same hub APIs as the web public player (listen) and mobile app (PTT upload).

## Features

- Public share WebSocket: `wss://<hub>/public/ws/<token>`
- Password login + multipart PTT: `POST /api/v1/radio-sets/<id>/ptt`
- SSD1306 status UI (link, talkgroup, TX state)
- Half-duplex: monitor pauses while PTT held
- WAV capture uploaded to hub (server transcodes to M4A)

## Recommended hardware

| Part | Amazon search | Notes |
|------|---------------|-------|
| MCU | `ESP32-S3 DevKitC N8R8 PSRAM` | **Recommended** for 30s PTT |
| MCU (you have) | `ESP32 WROOM 38 pin` | OK for bring-up; shorter PTT cap |
| OLED | `SSD1306 0.96 I2C 128x64` | You have these |
| Speaker amp | `MAX98357A I2S` | 3.3 V class-D |
| Speaker | `8 ohm mini speaker 28mm` | |
| Mic | `INMP441 I2S microphone` | Tie L/R pin to GND |
| PTT button | `arcade button momentary` or `tactile 12mm` | Active-low to GND |
| Power | `TP4056 USB C protection` + `3.7V LiPo 1200mAh` | |
| Wiring | `dupont wires` + `breadboard` | |

## Wiring (default pins)

See `include/pins.h`. Summary:

| Function | GPIO |
|----------|------|
| OLED SDA / SCL | 21 / 22 |
| Speaker BCLK / LRCK / DIN | 26 / 25 / 27 |
| Mic SCK / WS / SD | 14 / 15 / 32 |
| PTT (to GND) | 33 |
| Status LED | 2 |

## Hub setup

1. Create a **radio set** with the talkgroups you want to monitor.
2. On each radio set card, open the **Field unit** panel and **COPY**:
   - **Listen token** → `HUB_SHARE_TOKEN` (click **SHARE** first if empty)
   - **Set ID** → `HUB_RADIO_SET_ID`
4. Create a handheld user with **password login** enabled on the hub.
5. In hub admin, enable **TX** for that user (`tx_enabled`).
6. Edit `include/hub_config.h` — WiFi, hub host, share token, radio set ID, login.

## Test without wiring audio (logs only)

### Option A — Mac listen probe (no ESP32)

Same WebSocket the handheld uses. Watch live calls in your terminal:

```bash
cd tools/signalforge-handheld/scripts
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
# Fill HUB_HOST + HUB_SHARE_TOKEN in include/hub_config.h, or pass flags:
python listen_probe.py --host p7hub.projectseven.us --token YOUR_SHARE_TOKEN
```

You should see lines like `call#123 | tg=... | audio=48210B` when traffic hits that radio set.

### Option B — ESP32 on USB, serial monitor only

A board on USB (e.g. `/dev/cu.usbserial-*`) can run firmware **without** OLED/speaker/mic. Edit `hub_config.h` (WiFi + hub creds), then:

```bash
pio run -e esp32 -t upload
pio device monitor -b 115200
```

Serial logs use tags `[wifi]`, `[auth]`, `[listen]`, `[call]`. Missing I2S hardware is OK for this bench step.

## Build & flash

Requires [PlatformIO](https://platformio.org/).

```bash
cd tools/signalforge-handheld
# Edit include/hub_config.h first

# ESP32-S3 (recommended)
pio run -e esp32s3 -t upload

# Classic ESP32 (your WROOM boards)
pio run -e esp32 -t upload

pio device monitor
```

Prototype TLS: set `HUB_TLS_INSECURE 1` in `hub_config.h` only while testing self-signed hubs.

The `esp32s3` environment enables PSRAM flags for N8R8 modules. Use `esp32` for classic WROOM boards.

## Usage

1. Power on → WiFi → login → listen socket.
2. Incoming calls play on the speaker; OLED shows talkgroup.
3. **Hold PTT** → record → **release** → upload clip.
4. Other listeners (and your unit after upload) hear the PTT call on the set.

## RAM notes

| Board | PTT max (default) |
|-------|-------------------|
| ESP32-S3 N8R8 (PSRAM) | 30 s |
| ESP32 WROOM (no PSRAM) | 10 s |

## Project layout

```
include/          Pins, config template, module headers
src/              Display, hub client, audio in/out, main loop
platformio.ini    esp32s3 + esp32 environments
```

## Related hub docs

- Public player WebSocket: `server/internal/api/stream.go` (`/public/ws/{token}`)
- PTT upload: `server/internal/api/ptt.go` (`POST /api/v1/radio-sets/{id}/ptt`)
- Password login: `POST /api/v1/auth/login` → `sessionToken` in JSON
