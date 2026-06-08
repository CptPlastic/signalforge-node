#include <Arduino.h>
#include <WiFi.h>

#include "hub_config.h"

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
bool gWsConnected = false;
uint32_t gLastUiMs = 0;
uint32_t gRecordStartMs = 0;
bool gTransmitting = false;
int64_t gLastCallId = 0;

struct PendingClip {
  uint8_t *data = nullptr;
  size_t len = 0;
  bool isMp3 = true;
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

void queueClip(uint8_t *data, size_t len, const String &audioType) {
  if (!data || len == 0) return;
  if (gTransmitting) {
    free(data);
    return;
  }
  freePending();
  gPending.data = data;
  gPending.len = len;
  const String type = audioType;
  gPending.isMp3 = type.indexOf("mpeg") >= 0 || type.indexOf("mp3") >= 0 || looksLikeMp3(data, len);
  if (!gPending.isMp3 && looksLikeWav(data, len)) {
    gPending.isMp3 = false;
  }
}

void onHubCall(const HubCallEvent &event) {
  if (event.id > 0 && event.id <= gLastCallId) {
    return;
  }
  if (event.id > gLastCallId) {
    gLastCallId = event.id;
  }

  gLastTalkgroup = event.talkgroupLabel.length() ? event.talkgroupLabel : String("TG #") + event.id;
  if (event.origin == "ptt" && event.senderEmail.length()) {
    gStatusLine = "PTT " + event.senderEmail;
  } else {
    gStatusLine = event.systemLabel.length() ? event.systemLabel : "call";
  }
  Serial.printf("[call] id=%lld tg=%s sys=%s origin=%s type=%s\n",
                static_cast<long long>(event.id),
                gLastTalkgroup.c_str(),
                event.systemLabel.c_str(),
                event.origin.c_str(),
                event.audioType.c_str());

  if (!event.audio || event.audioLen == 0) {
    return;
  }
  uint8_t *copy = static_cast<uint8_t *>(malloc(event.audioLen));
  if (!copy) return;
  memcpy(copy, event.audio, event.audioLen);
  queueClip(copy, event.audioLen, event.audioType);
}

bool connectWifi() {
  gDisplay.showBoot();
  Serial.printf("[wifi] connecting to %s\n", WIFI_SSID);
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
  gStatusLine = "wifi...";
  uint32_t start = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - start < 30000) {
    delay(250);
  }
  if (WiFi.status() != WL_CONNECTED) {
    Serial.println("[wifi] connect failed");
    gDisplay.showError("WiFi", "connect failed");
    return false;
  }
  WiFi.setSleep(false);
  Serial.printf("[wifi] connected ip=%s rssi=%d sleep=off\n", WiFi.localIP().toString().c_str(),
                WiFi.RSSI());
  gDisplay.showWifi(WIFI_SSID, WiFi.RSSI());
  return true;
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

void startListen() {
  Serial.printf("[listen] opening wss://%s/public/ws/%s\n", HUB_HOST, HUB_SHARE_TOKEN);
  gHub.connectListen(HUB_SHARE_TOKEN, onHubCall);
  gStatusLine = "listen...";
}

void servicePlayback() {
  if (gTransmitting || gAudioOut.isPlaying() || !gPending.data) return;
  const bool ok = gPending.isMp3 ? gAudioOut.playMp3(gPending.data, gPending.len)
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
  serialCliHandleLine(line.c_str(), gHub);
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
    startListen();
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
  if (!gDisplay.begin()) {
    Serial.println("[oled] not found — serial-only mode is fine");
  } else {
    gDisplay.showBoot();
  }

  if (!gAudioOut.begin()) {
    Serial.println("[audio] speaker path not ready (ok for log-only bench)");
  }
  if (!gAudioIn.begin(PTT_SAMPLE_RATE)) {
    Serial.println("[audio] mic path not ready (PTT disabled until wired)");
  }
  gPtt.begin(PIN_PTT);

  if (!gHub.begin(HUB_HOST, HUB_PORT, HUB_USE_TLS, HUB_TLS_INSECURE)) {
    gDisplay.showError("Hub", "bad host");
    return;
  }
  if (!connectWifi()) return;
  startListen();
  if (gHub.ensureSession()) {
    Serial.println("[auth] cached PTT session found");
  } else {
    Serial.println("[auth] listen is live — PTT needs one-time serial login");
  }
}

void loop() {
  handleSerialInput();
  gHub.listenLoop();
  gAudioOut.loop();
  handlePtt();
  servicePlayback();

  static bool lastWs = false;
  const bool wsUp = WiFi.status() == WL_CONNECTED;
  gWsConnected = wsUp;
  if (wsUp != lastWs || millis() - gLastUiMs > 500) {
    gLastUiMs = millis();
    lastWs = wsUp;
    if (!gTransmitting) {
      gDisplay.showMonitor(gWsConnected, gLastTalkgroup.c_str(), gStatusLine.c_str());
    }
  }
}
