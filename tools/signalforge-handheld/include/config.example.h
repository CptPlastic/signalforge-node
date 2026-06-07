#pragma once

// Deprecated template — edit include/hub_config.h instead.

// WiFi
#define WIFI_SSID "YourNetwork"
#define WIFI_PASSWORD "YourWiFiPassword"

// Hub (no trailing slash). Use hostname only for TLS SNI.
#define HUB_HOST "p7hub.projectseven.us"
#define HUB_PORT 443
#define HUB_USE_TLS 1

// Public listen stream — Radio Set → Share → copy token from hub admin.
#define HUB_SHARE_TOKEN "paste_public_share_token_here"

// PTT — same radio set UUID from hub admin. User must have TX enabled.
#define HUB_RADIO_SET_ID "paste_radio_set_uuid_here"
#define HUB_LOGIN_EMAIL "handheld@local.signalforge"
#define HUB_LOGIN_PASSWORD "change-me-strong"

// Set to 1 to skip TLS certificate validation (prototype only).
#define HUB_TLS_INSECURE 0

// Audio
#define PTT_SAMPLE_RATE 16000
#define PTT_MAX_SECONDS 30

// Shorter cap on boards without PSRAM (override in code if needed).
#define PTT_MAX_SECONDS_NO_PSRAM 10
