#pragma once

// Local only — gitignored. Copy from hub_config.example.h.
// PTT login is NOT stored here; use USB serial: login email password

// WiFi
#define WIFI_SSID "LODGE"
#define WIFI_PASSWORD "Ilovemywife!"

// Hub (no trailing slash). Use hostname only for TLS SNI.
#define HUB_HOST "p7hub.projectseven.us"
#define HUB_PORT 443
#define HUB_USE_TLS 1

// Hub → Radio Sets → Field unit panel → COPY Listen token / Set ID.
#define HUB_SHARE_TOKEN "share_660c65e7922137ac53ed0167667e104299a25efa139b6d97d6c33bbf08c6f7cb"
#define HUB_RADIO_SET_ID "rs_ec317b6ae4e4b85fe9b00ff33cba1db2f3b1d5c5eea4ecef30f3d4fd6d3dc01f"

// 1 = accept any TLS cert (recommended for first bench test).
#define HUB_TLS_INSECURE 1

// Audio
#define PTT_SAMPLE_RATE 16000
#define PTT_MAX_SECONDS 30

// Shorter cap on boards without PSRAM.
#define PTT_MAX_SECONDS_NO_PSRAM 10
