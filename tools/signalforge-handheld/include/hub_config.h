#pragma once

// Edit this file with your WiFi, hub URL, share token, and PTT login.
// config.example.h is a backup template only.

// WiFi
#define WIFI_SSID "LODGE"
#define WIFI_PASSWORD "Ilovemywife!"

// Hub (no trailing slash). Use hostname only for TLS SNI.
#define HUB_HOST "p7hub.projectseven.us"
#define HUB_PORT 443
#define HUB_USE_TLS 1

// Hub → Radio Sets → Field unit panel → COPY Listen token / Set ID.
#define HUB_SHARE_TOKEN "share_660c65e7922137ac53ed0167667e104299a25efa139b6d97d6c33bbf08c6f7cb"
#define HUB_RADIO_SET_ID "paste_radio_set_uuid_here"
#define HUB_LOGIN_EMAIL "handheld@local.signalforge"
#define HUB_LOGIN_PASSWORD "change-me-strong"

// 1 = accept any TLS cert (recommended for first bench test). 0 = same path today; pin CA later.
#define HUB_TLS_INSECURE 1

// Audio
#define PTT_SAMPLE_RATE 16000
#define PTT_MAX_SECONDS 30

// Shorter cap on boards without PSRAM.
#define PTT_MAX_SECONDS_NO_PSRAM 10
