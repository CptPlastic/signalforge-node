#pragma once

// Copy to hub_config.h (gitignored). Never commit WiFi or hub secrets.
//   cp include/hub_config.example.h include/hub_config.h

// WiFi
#define WIFI_SSID "YourNetwork"
#define WIFI_PASSWORD "YourWiFiPassword"

// Hub (no trailing slash). Use hostname only for TLS SNI.
#define HUB_HOST "p7hub.projectseven.us"
#define HUB_PORT 443
#define HUB_USE_TLS 1

// Hub console → Radio Sets → Field unit → COPY.
#define HUB_SHARE_TOKEN "paste_public_share_token_here"
#define HUB_RADIO_SET_ID "paste_radio_set_id_here"

// PTT login is NOT stored here. Use USB serial once: login email password
// (session token cached on device). See README.

#define HUB_TLS_INSECURE 1

#define PTT_SAMPLE_RATE 16000
#define PTT_MAX_SECONDS 30
#define PTT_MAX_SECONDS_NO_PSRAM 10

// Decode hub call audio and play on I2S speaker (MAX98357A). Safe to leave on
// without hardware — decode/queue only runs when heap allows.
#define HANDHELD_ENABLE_AUDIO_PLAYBACK 1
// Minimum free heap before decoding a call clip (~22KB decoded + TLS headroom).
#define HANDHELD_AUDIO_MIN_HEAP 70000
