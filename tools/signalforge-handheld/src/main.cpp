#include <Arduino.h>
#include <WiFi.h>

#include "hub_config.h"

#ifndef HANDHELD_ENABLE_AUDIO_PLAYBACK
#define HANDHELD_ENABLE_AUDIO_PLAYBACK 1
#endif

#include "audio_in.h"
#include "audio_out.h"
#include "display.h"
#include "hub_client.h"
#include "pins.h"
#include "ptt_button.h"
#include "serial_cli.h"

namespace {
FieldDisplay gDisplay;
HubClient gHub;
AudioOut gAudioOut;
AudioIn gAudioIn;
PttButton gPtt;

String gStatusLine = "starting";
String gLastTalkgroup = "waiting";
String gLastSystem;
String gLastOrigin;
String gLastSender;
float gLastDuration = 0;
int gLastFrequency = 0;
bool gWifiUp = false;
bool gListenUp = false;
uint32_t gLastUiMs = 0;
uint32_t gLastWifiRetryMs = 0;
bool gHadWifiOnce = false;
constexpr uint32_t kWifiRetryMs = 5000;
uint32_t gRecordStartMs = 0;
bool gTransmitting = false;
bool gSpeakerReady = false;
bool gMicReady = false;
int64_t gLastCallId = 0;
int64_t gPendingFetchCallId = 0;
String gPendingFetchAudioType;

enum class ClipFormat : uint8_t { Mp3, Wav, Unsupported };

struct PendingClip {
  uint8_t *data = nullptr;
  size_t len = 0;
  ClipFormat format = ClipFormat::Unsupported;
};

PendingClip gPending;

void freePending() {
  if (gPending.data) {
    free(gPending.data);
    gPending.data = nullptr;
  }
  gPending.len = 0;
}

bool looksLikeMp3(const uint8_t *data, size_t len) {
  if (len < 3) return false;
  if (data[0] == 'I' && data[1] == 'D' && data[2] == '3') return true;
  if (data[0] == 0xff && (data[1] & 0xe0) == 0xe0) return true;
  return false;
}

bool looksLikeWav(const uint8_t *data, size_t len) {
  return len >= 12 && memcmp(data, "RIFF", 4) == 0 && memcmp(data + 8, "WAVE", 4) == 0;
}

bool looksLikeMp4(const uint8_t *data, size_t len) {
  return len >= 12 && memcmp(data + 4, "ftyp", 4) == 0;
}

bool isUnsupportedPlaybackType(const String &audioType, const uint8_t *data, size_t len) {
  String type = audioType;
  type.toLowerCase();
  if (type.indexOf("mp4") >= 0 || type.indexOf("m4a") >= 0 || type.indexOf("opus") >= 0 ||
      type.indexOf("aac") >= 0 || type.indexOf("mpeg4") >= 0) {
    return true;
  }
  if (looksLikeMp4(data, len)) return true;
  if (looksLikeMp3(data, len) || looksLikeWav(data, len)) return false;
  return len > 0;
}

ClipFormat detectClipFormat(const String &audioType, const uint8_t *data, size_t len) {
  if (isUnsupportedPlaybackType(audioType, data, len)) return ClipFormat::Unsupported;
  String type = audioType;
  type.toLowerCase();
  if (type.indexOf("mpeg") >= 0 || type.indexOf("mp3") >= 0 || looksLikeMp3(data, len)) {
    return ClipFormat::Mp3;
  }
  if (type.indexOf("wav") >= 0 || looksLikeWav(data, len)) return ClipFormat::Wav;
  return ClipFormat::Unsupported;
}

void queueClip(uint8_t *data, size_t len, const String &audioType) {
  if (!data || len == 0) return;
  if (gTransmitting) {
    free(data);
    return;
  }
  freePending();
  gPending.data = data;
  gPending.len = len;
  gPending.format = detectClipFormat(audioType, data, len);
}

void onHubCall(const HubCallEvent &event) {
  if (event.id > 0 && event.id <= gLastCallId) {
    return;
  }
  if (event.id > gLastCallId) {
    gLastCallId = event.id;
  }

  gLastTalkgroup = event.talkgroupLabel.length()
                       ? event.talkgroupLabel
                       : (event.talkgroupGroup.length() ? event.talkgroupGroup
                                                        : String("TG #") + event.id);
  gLastSystem = event.systemLabel;
  gLastOrigin = event.origin.length() ? event.origin : String("rf");
  gLastSender = event.senderEmail;
  gLastDuration = event.durationSec;
  gLastFrequency = event.frequencyHz;
  if (event.origin == "ptt" && event.senderEmail.length()) {
    gStatusLine = "PTT " + event.senderEmail;
  } else {
    gStatusLine = event.systemLabel.length() ? event.systemLabel : "call";
  }
  Serial.printf("[call] id=%lld tg=%s sys=%s origin=%s dur=%.1fs type=%s\n",
                static_cast<long long>(event.id),
                gLastTalkgroup.c_str(),
                event.systemLabel.c_str(),
                event.origin.c_str(),
                event.durationSec,
                event.audioType.c_str());

  if (!event.audio || event.audioLen == 0) {
#if HANDHELD_ENABLE_AUDIO_PLAYBACK
    if (gSpeakerReady && event.id > 0 && !gTransmitting) {
      gPendingFetchCallId = event.id;
      gPendingFetchAudioType = event.audioType.length() ? event.audioType : String("audio/mpeg");
    }
#endif
    return;
  }
#if !HANDHELD_ENABLE_AUDIO_PLAYBACK
  return;
#endif
  if (!gSpeakerReady) {
    Serial.printf("[audio] clip %uB — speaker not initialized\n", static_cast<unsigned>(event.audioLen));
    return;
  }
  if (isUnsupportedPlaybackType(event.audioType, event.audio, event.audioLen)) {
    Serial.printf("[audio] skip — %s not supported (need MP3/WAV)\n", event.audioType.c_str());
    return;
  }
  if (gAudioOut.isPlaying()) {
    Serial.println("[audio] skip — already playing");
    return;
  }
  uint8_t *copy = static_cast<uint8_t *>(malloc(event.audioLen));
  if (!copy) {
    Serial.printf("[audio] malloc failed heap=%u\n", ESP.getFreeHeap());
    return;
  }
  memcpy(copy, event.audio, event.audioLen);
  queueClip(copy, event.audioLen, event.audioType);
  Serial.printf("[audio] queued %uB %s\n", static_cast<unsigned>(event.audioLen), event.audioType.c_str());
}

void startListen();

bool connectWifiBlocking(uint32_t timeoutMs) {
  Serial.printf("[wifi] connecting to %s\n", WIFI_SSID);
  WiFi.persistent(false);
  WiFi.mode(WIFI_STA);
  WiFi.setAutoReconnect(true);
  WiFi.setSleep(false);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  gStatusLine = "wifi...";
  const uint32_t start = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - start < timeoutMs) {
    delay(250);
  }
  if (WiFi.status() != WL_CONNECTED) {
    Serial.println("[wifi] connect failed (will retry)");
    return false;
  }
  gWifiUp = true;
  gHadWifiOnce = true;
  Serial.printf("[wifi] connected ip=%s rssi=%d sleep=off\n", WiFi.localIP().toString().c_str(),
                WiFi.RSSI());
  gDisplay.showWifi(WIFI_SSID, WiFi.RSSI());
  return true;
}

void onNetworkBack() {
  gWifiUp = true;
  WiFi.setSleep(false);
  Serial.printf("[wifi] online ip=%s rssi=%d\n", WiFi.localIP().toString().c_str(), WiFi.RSSI());
  startListen();
  gStatusLine = "listen...";
}

void serviceWifi() {
  const bool up = WiFi.status() == WL_CONNECTED;
  if (up) {
    if (!gWifiUp) {
      onNetworkBack();
    }
    gWifiUp = true;
    gHadWifiOnce = true;
    return;
  }

  if (gWifiUp || gHadWifiOnce) {
    if (gWifiUp) {
      Serial.println("[wifi] lost — retrying");
      gHub.disconnectListen();
      gListenUp = false;
      gStatusLine = "wifi down";
    }
    gWifiUp = false;
  }

  if (millis() - gLastWifiRetryMs < kWifiRetryMs) {
    return;
  }
  gLastWifiRetryMs = millis();

  Serial.printf("[wifi] retry %s\n", WIFI_SSID);
  gStatusLine = "wifi retry";
  WiFi.disconnect(true);
  WiFi.mode(WIFI_STA);
  WiFi.setAutoReconnect(true);
  WiFi.setSleep(false);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
}

bool ensurePttSession() {
  if (gHub.ensureSession()) {
    return true;
  }
  Serial.println("[auth] PTT needs login — USB serial: login <email> <password>");
  Serial.println("[auth] use a dedicated handheld hub account with TX enabled");
  gDisplay.showLogin(false, "serial login");
  return false;
}

void serviceCallFetch() {
#if !HANDHELD_ENABLE_AUDIO_PLAYBACK
  return;
#endif
  if (gPendingFetchCallId <= 0 || gTransmitting || gAudioOut.isPlaying() || gPending.data) return;
  const int64_t callId = gPendingFetchCallId;
  const String audioType = gPendingFetchAudioType;
  gPendingFetchCallId = 0;
  gPendingFetchAudioType = "";

  uint8_t *data = nullptr;
  size_t len = 0;
  String fetchedType;
  if (!gHub.fetchPublicCallAudio(HUB_SHARE_TOKEN, callId, &data, &len, &fetchedType)) {
    return;
  }
  const String type = fetchedType.length() ? fetchedType : audioType;
  if (isUnsupportedPlaybackType(type, data, len)) {
    Serial.printf("[audio] skip — %s not supported (need MP3/WAV)\n", type.c_str());
    free(data);
    return;
  }
  queueClip(data, len, type);
  Serial.printf("[audio] queued %uB %s (fetched)\n", static_cast<unsigned>(len), type.c_str());
}

void startListen() {
  if (gHub.listenClientActive()) {
    return;
  }
  Serial.printf("[listen] opening wss://%s/public/ws/%s?seed=0&format=mp3&inline=0\n", HUB_HOST,
                HUB_SHARE_TOKEN);
  gHub.connectListen(HUB_SHARE_TOKEN, onHubCall);
  gStatusLine = "listen...";
}

void servicePlayback() {
#if !HANDHELD_ENABLE_AUDIO_PLAYBACK
  return;
#endif
  if (gTransmitting || gAudioOut.isPlaying() || !gPending.data) return;
  if (gPending.format == ClipFormat::Unsupported) {
    Serial.println("[audio] skip — unsupported clip format");
    freePending();
    return;
  }
  const bool ok = gPending.format == ClipFormat::Mp3
                      ? gAudioOut.playMp3(gPending.data, gPending.len)
                      : gAudioOut.playWav(gPending.data, gPending.len);
  if (ok) {
    gStatusLine = "playing";
    freePending();
  } else {
    gStatusLine = "decode err";
    freePending();
  }
}

void handleSerialInput() {
  if (!Serial.available()) return;
  String line = Serial.readStringUntil('\n');
  FieldDeviceStatus status;
  status.speakerReady = gSpeakerReady;
  status.micReady = gMicReady;
#if HANDHELD_ENABLE_AUDIO_PLAYBACK
  status.audioPlaybackEnabled = true;
#else
  status.audioPlaybackEnabled = false;
#endif
  status.wifiUp = gWifiUp;
  status.listenUp = gListenUp;
  serialCliHandleLine(line.c_str(), gHub, status);
}

void handlePtt() {
  gPtt.loop();
  if (gPtt.justPressed() && !gTransmitting) {
    gTransmitting = true;
    gAudioOut.stop();
    freePending();
    gAudioIn.start();
    gRecordStartMs = millis();
    gStatusLine = "TX key";
    digitalWrite(PIN_STATUS_LED, HIGH);
  }

  if (gTransmitting && gAudioIn.isRecording()) {
    gAudioIn.captureLoop();
    gDisplay.showPttRecording((millis() - gRecordStartMs) / 1000.0f);
    if ((millis() - gRecordStartMs) > (gAudioIn.maxSeconds() * 1000UL)) {
      gAudioIn.stop();
    }
  }

  if (gPtt.justReleased() && gTransmitting) {
    gTransmitting = false;
    digitalWrite(PIN_STATUS_LED, LOW);
    gAudioIn.stop();
    size_t wavLen = 0;
    float duration = 0;
    uint8_t *wav = gAudioIn.buildWav(&wavLen, &duration);
    if (!wav || wavLen == 0) {
      gDisplay.showPttUpload("no audio");
      gStatusLine = "tx empty";
      return;
    }

    gDisplay.showPttUpload("uploading...");
    if (!ensurePttSession()) {
      free(wav);
      gStatusLine = "login fail";
      return;
    }

    const String clientId = String("esp-") + String((uint32_t)ESP.getEfuseMac(), HEX) + "-" + String(millis());
    int64_t callId = 0;
    const bool ok = gHub.uploadPtt(HUB_RADIO_SET_ID, wav, wavLen, duration, clientId.c_str(), &callId);
    free(wav);
    const String uploadMsg = ok ? ("ok #" + String(callId)) : String("failed");
    gDisplay.showPttUpload(uploadMsg.c_str());
    gStatusLine = ok ? "tx ok" : "tx fail";
  }
}
}  // namespace

void setup() {
  Serial.begin(115200);
  delay(200);
  pinMode(PIN_STATUS_LED, OUTPUT);
  digitalWrite(PIN_STATUS_LED, LOW);

  Serial.println("[boot] SignalForge field unit");
  serialCliPrintHelp();
  gDisplay.begin();

  gSpeakerReady = gAudioOut.begin();
  if (!gSpeakerReady) {
    Serial.println("[audio] speaker I2S init failed");
  } else {
#if HANDHELD_ENABLE_AUDIO_PLAYBACK
    Serial.println("[audio] speaker path ready — live call playback enabled");
#else
    Serial.println("[audio] speaker path ready — playback disabled in config");
#endif
  }
  gMicReady = gAudioIn.begin(PTT_SAMPLE_RATE);
  if (!gMicReady) {
    Serial.println("[audio] mic I2S init failed (PTT disabled until wired)");
  } else {
    Serial.printf("[audio] mic path ready — max PTT %.0fs\n", gAudioIn.maxSeconds());
  }
  gPtt.begin(PIN_PTT);

  if (!gHub.begin(HUB_HOST, HUB_PORT, HUB_USE_TLS, HUB_TLS_INSECURE)) {
    gDisplay.showError("Hub", "bad host");
    return;
  }
  if (connectWifiBlocking(30000)) {
    startListen();
  } else {
    Serial.println("[wifi] continuing — auto-retry enabled");
  }
  if (gHub.ensureSession()) {
    Serial.println("[auth] cached PTT session found");
  } else {
    Serial.println("[auth] listen is live — PTT needs one-time serial login");
  }
}

void loop() {
  handleSerialInput();
  serviceWifi();
  gHub.listenLoop();
  gListenUp = gHub.listenConnected();
  serviceCallFetch();
  gAudioOut.loop();
  handlePtt();
  servicePlayback();

  static bool lastLive = false;
  const bool live = gWifiUp && gListenUp;
  if (live != lastLive || millis() - gLastUiMs > 500) {
    gLastUiMs = millis();
    lastLive = live;
    if (!gTransmitting) {
      FieldMonitorInfo mon;
      mon.wsUp = live;
      mon.rssi = gWifiUp ? WiFi.RSSI() : 0;
      mon.pttSession = gHub.ensureSession();
      mon.talkgroup = gLastTalkgroup.c_str();
      mon.system = (!gWifiUp)          ? "wifi retry"
                   : (!gListenUp)      ? "hub sync..."
                                       : gLastSystem.c_str();
      mon.origin = gLastOrigin.c_str();
      mon.sender = gLastSender.c_str();
      mon.durationSec = gLastDuration;
      mon.frequencyHz = gLastFrequency;
      mon.callId = gLastCallId;
      gDisplay.showMonitor(mon);
    }
  }
}
