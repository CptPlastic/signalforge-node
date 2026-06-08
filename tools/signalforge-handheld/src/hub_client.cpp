#include "hub_client.h"

#include <ArduinoJson.h>
#include <HTTPClient.h>
#include <Preferences.h>
#include <WebSocketsClient.h>
#include <WiFiClientSecure.h>
#include <mbedtls/base64.h>

#include "hub_config.h"

#ifndef HANDHELD_ENABLE_AUDIO_PLAYBACK
#define HANDHELD_ENABLE_AUDIO_PLAYBACK 1
#endif
#ifndef HANDHELD_AUDIO_MIN_HEAP
#define HANDHELD_AUDIO_MIN_HEAP 70000
#endif

namespace {
constexpr const char *kPrefsNs = "sfhandheld";
constexpr const char *kPrefsToken = "token";

bool decodeBase64Slice(const char *input, size_t inLen, uint8_t **outData, size_t *outLen) {
  if (!input || inLen == 0 || !outData || !outLen) return false;
  size_t olen = 0;
  if (mbedtls_base64_decode(nullptr, 0, &olen,
                            reinterpret_cast<const unsigned char *>(input), inLen) !=
      MBEDTLS_ERR_BASE64_BUFFER_TOO_SMALL) {
    return false;
  }
  uint8_t *buf = static_cast<uint8_t *>(malloc(olen));
  if (!buf) return false;
  if (mbedtls_base64_decode(buf, olen, &olen,
                            reinterpret_cast<const unsigned char *>(input), inLen) != 0) {
    free(buf);
    return false;
  }
  *outData = buf;
  *outLen = olen;
  return true;
}

bool decodeBase64(const char *input, uint8_t **outData, size_t *outLen) {
  if (!input) return false;
  return decodeBase64Slice(input, strlen(input), outData, outLen);
}

bool findCallAudioInJson(const char *json, size_t jsonLen, const char **b64Out, size_t *b64LenOut) {
  if (!json || jsonLen < 12 || !b64Out || !b64LenOut) return false;

  const char *markers[] = {"\"audio\":\"", "\"audio\": \""};
  const char *start = nullptr;
  for (const char *marker : markers) {
    const size_t markerLen = strlen(marker);
    if (jsonLen <= markerLen) continue;
    const char *hit = strstr(json, marker);
    if (hit && static_cast<size_t>(hit - json) + markerLen < jsonLen) {
      start = hit + markerLen;
      break;
    }
  }
  if (!start) return false;

  const char *end = start;
  while (end < json + jsonLen && *end != '"') {
    end++;
  }
  if (end == start) return false;

  *b64Out = start;
  *b64LenOut = static_cast<size_t>(end - start);
  return true;
}

bool attachCallAudio(HubCallEvent &event, const char *payload, size_t payloadLen) {
#if !HANDHELD_ENABLE_AUDIO_PLAYBACK
  (void)event;
  (void)payload;
  (void)payloadLen;
  return false;
#else
  const char *b64 = nullptr;
  size_t b64Len = 0;
  if (!findCallAudioInJson(payload, payloadLen, &b64, &b64Len) || b64Len < 16) {
    return false;
  }

  const uint32_t heap = ESP.getFreeHeap();
  if (heap < HANDHELD_AUDIO_MIN_HEAP) {
    Serial.printf("[audio] skip decode heap=%u need>=%u\n", heap, HANDHELD_AUDIO_MIN_HEAP);
    return false;
  }

  if (!decodeBase64Slice(b64, b64Len, &event.audio, &event.audioLen)) {
    Serial.printf("[audio] decode failed heap=%u\n", ESP.getFreeHeap());
    return false;
  }
  Serial.printf("[audio] decoded %uB heap=%u\n", static_cast<unsigned>(event.audioLen), ESP.getFreeHeap());
  return true;
#endif
}
}  // namespace

bool HubClient::begin(const char *host, uint16_t port, bool useTls, bool tlsInsecure) {
  host_ = host;
  port_ = port;
  useTls_ = useTls;
  tlsInsecure_ = tlsInsecure;

  Preferences prefs;
  if (prefs.begin(kPrefsNs, true)) {
    sessionToken_ = prefs.getString(kPrefsToken, "");
    prefs.end();
  }
  return host_.length() > 0;
}

bool HubClient::httpRequest(const char *method,
                            const char *path,
                            const char *contentType,
                            const uint8_t *body,
                            size_t bodyLen,
                            const char *authBearer,
                            int *outStatus,
                            String *outBody) {
  WiFiClient *plain = nullptr;
  WiFiClientSecure tls;
  WiFiClient *stream = nullptr;

  if (useTls_) {
    tls.setInsecure();
    (void)tlsInsecure_;
    stream = &tls;
  } else {
    plain = new WiFiClient();
    stream = plain;
  }

  HTTPClient http;
  const String url = String(useTls_ ? "https://" : "http://") + host_ + path;
  if (!http.begin(*stream, url)) {
    delete plain;
    return false;
  }
  http.setTimeout(30000);
  if (contentType && contentType[0]) {
    http.addHeader("Content-Type", contentType);
  }
  if (authBearer && authBearer[0]) {
    http.addHeader("Authorization", String("Bearer ") + authBearer);
  }

  int status = -1;
  if (strcmp(method, "POST") == 0) {
    uint8_t *payload = nullptr;
    if (bodyLen > 0) {
      payload = static_cast<uint8_t *>(malloc(bodyLen));
      if (!payload) {
        http.end();
        delete plain;
        return false;
      }
      memcpy(payload, body, bodyLen);
    }
    status = http.POST(payload, bodyLen);
    free(payload);
  } else if (strcmp(method, "GET") == 0) {
    status = http.GET();
  } else {
    http.end();
    delete plain;
    return false;
  }

  if (outStatus) *outStatus = status;
  if (outBody) *outBody = http.getString();
  http.end();
  delete plain;
  return status > 0;
}

bool HubClient::httpGetBinary(const char *path, int *outStatus, uint8_t **outData, size_t *outLen, String *outType) {
  if (!path || !outData || !outLen) return false;
  *outData = nullptr;
  *outLen = 0;

  WiFiClient *plain = nullptr;
  WiFiClientSecure tls;
  WiFiClient *stream = nullptr;

  if (useTls_) {
    tls.setInsecure();
    stream = &tls;
  } else {
    plain = new WiFiClient();
    stream = plain;
  }

  HTTPClient http;
  const String url = String(useTls_ ? "https://" : "http://") + host_ + path;
  if (!http.begin(*stream, url)) {
    delete plain;
    return false;
  }
  http.setTimeout(45000);
  const int status = http.GET();
  if (outStatus) *outStatus = status;
  if (status != 200) {
    http.end();
    delete plain;
    return false;
  }
  if (outType) {
    *outType = http.header("Content-Type");
  }
  const int contentLen = http.getSize();
  WiFiClient *tcp = http.getStreamPtr();
  if (!tcp) {
    http.end();
    delete plain;
    return false;
  }

  size_t cap = contentLen > 0 ? static_cast<size_t>(contentLen) : 65536;
  if (cap > 262144) cap = 262144;
  uint8_t *buf = static_cast<uint8_t *>(malloc(cap));
  if (!buf) {
    http.end();
    delete plain;
    return false;
  }

  size_t total = 0;
  const uint32_t start = millis();
  while (millis() - start < 45000) {
    const int avail = tcp->available();
    if (avail > 0) {
      if (total + static_cast<size_t>(avail) > cap) {
        const size_t newCap = total + static_cast<size_t>(avail) + 4096;
        uint8_t *grown = static_cast<uint8_t *>(realloc(buf, newCap));
        if (!grown) break;
        buf = grown;
        cap = newCap;
      }
      const int n = tcp->read(buf + total, avail);
      if (n > 0) total += static_cast<size_t>(n);
    } else if (!tcp->connected() && !http.connected()) {
      break;
    } else if (contentLen > 0 && total >= static_cast<size_t>(contentLen)) {
      break;
    } else {
      delay(1);
    }
  }
  http.end();
  delete plain;

  if (total == 0) {
    free(buf);
    return false;
  }
  *outData = buf;
  *outLen = total;
  return true;
}

bool HubClient::fetchPublicCallAudio(const char *shareToken,
                                     int64_t callId,
                                     uint8_t **outData,
                                     size_t *outLen,
                                     String *outType) {
  if (!shareToken || callId <= 0) return false;
  const String path = String("/public/calls/") + shareToken + "/" + String(callId) + "/audio?format=mp3";
  int status = 0;
  const bool ok = httpGetBinary(path.c_str(), &status, outData, outLen, outType);
  if (!ok) {
    Serial.printf("[audio] GET %s status=%d heap=%u\n", path.c_str(), status, ESP.getFreeHeap());
  } else {
    Serial.printf("[audio] fetched %uB id=%lld heap=%u\n", static_cast<unsigned>(*outLen),
                  static_cast<long long>(callId), ESP.getFreeHeap());
  }
  return ok;
}

bool HubClient::login(const char *email, const char *password) {
  JsonDocument doc;
  doc["email"] = email;
  doc["password"] = password;
  String body;
  serializeJson(doc, body);

  int status = 0;
  String response;
  if (!httpRequest("POST", "/api/v1/auth/login", "application/json",
                   reinterpret_cast<const uint8_t *>(body.c_str()), body.length(), nullptr, &status,
                   &response)) {
    return false;
  }
  if (status != 200) {
    Serial.printf("login failed status=%d body=%s\n", status, response.c_str());
    return false;
  }

  JsonDocument resp;
  if (deserializeJson(resp, response)) {
    return false;
  }
  sessionToken_ = resp["sessionToken"].as<String>();
  if (sessionToken_.isEmpty()) {
    return false;
  }

  Preferences prefs;
  if (prefs.begin(kPrefsNs, false)) {
    prefs.putString(kPrefsToken, sessionToken_);
    prefs.end();
  }
  return true;
}

bool HubClient::ensureSession() {
  return sessionToken_.length() > 0;
}

void HubClient::clearSession() {
  sessionToken_ = "";
  Preferences prefs;
  if (prefs.begin(kPrefsNs, false)) {
    prefs.remove(kPrefsToken);
    prefs.end();
  }
}

bool HubClient::handleListenMessage(const char *payload, size_t len) {
  if (!payload || len == 0 || !onCall_) return false;

  // Skip the base64 audio blob — it is most of the ~30KB frame and exhausts ESP32 RAM.
  JsonDocument filter;
  filter["cmd"] = true;
  filter["id"] = true;
  filter["talkgroupLabel"] = true;
  filter["talkgroupGroup"] = true;
  filter["systemLabel"] = true;
  filter["origin"] = true;
  filter["senderEmail"] = true;
  filter["audioType"] = true;
  filter["duration"] = true;
  filter["frequency"] = true;

  JsonDocument doc;
  const DeserializationError err =
      deserializeJson(doc, payload, len, DeserializationOption::Filter(filter));
  if (err) {
    Serial.printf("[listen] json parse failed: %s heap=%u\n", err.c_str(), ESP.getFreeHeap());
    return false;
  }

  const char *cmd = doc["cmd"] | "";
  if (strcmp(cmd, "call") != 0) {
    return false;
  }

  HubCallEvent event;
  event.id = doc["id"] | 0LL;
  event.talkgroupLabel = doc["talkgroupLabel"].as<String>();
  event.talkgroupGroup = doc["talkgroupGroup"].as<String>();
  event.systemLabel = doc["systemLabel"].as<String>();
  event.origin = doc["origin"].as<String>();
  event.senderEmail = doc["senderEmail"].as<String>();
  event.audioType = doc["audioType"].as<String>();
  event.durationSec = doc["duration"] | 0.0f;
  event.frequencyHz = doc["frequency"] | 0;

  attachCallAudio(event, payload, len);
  onCall_(event);
  if (event.audio) {
    free(event.audio);
    event.audio = nullptr;
    event.audioLen = 0;
  }
  return true;
}

namespace {
HubClient *g_activeHub = nullptr;

void onWebSocketEvent(WStype_t type, uint8_t *payload, size_t length) {
  if (!g_activeHub) return;
  switch (type) {
    case WStype_CONNECTED:
      g_activeHub->setListenConnected(true);
      Serial.printf("[listen] connected heap=%u max=%u\n", ESP.getFreeHeap(),
                    static_cast<unsigned>(WEBSOCKETS_MAX_DATA_SIZE));
      break;
    case WStype_DISCONNECTED:
      g_activeHub->setListenConnected(false);
      if (payload && length >= 2) {
        const uint16_t code = (static_cast<uint16_t>(payload[0]) << 8) | payload[1];
        Serial.printf("[listen] disconnected code=%u reason=%.*s heap=%u\n", code,
                      static_cast<int>(length > 2 ? length - 2 : 0),
                      length > 2 ? reinterpret_cast<const char *>(payload + 2) : "",
                      ESP.getFreeHeap());
      } else if (payload && length > 0) {
        Serial.printf("[listen] disconnected (%.*s) heap=%u\n", static_cast<int>(length),
                      reinterpret_cast<const char *>(payload), ESP.getFreeHeap());
      } else {
        Serial.printf("[listen] disconnected (tcp/ssl) heap=%u\n", ESP.getFreeHeap());
      }
      break;
    case WStype_ERROR:
      Serial.printf("listen ws error heap=%u\n", ESP.getFreeHeap());
      break;
    case WStype_TEXT:
      g_activeHub->onListenTextFrame(payload, length);
      break;
    default:
      break;
  }
}
}  // namespace

void HubClient::onListenTextFrame(const uint8_t *payload, size_t length) {
  queueListenFrame(payload, length);
}

void HubClient::queueListenFrame(const uint8_t *payload, size_t length) {
  if (!payload || length == 0) return;
  if (pendingListenFrame_) {
    Serial.printf("[listen] frame replaced pending len=%u\n",
                  static_cast<unsigned>(pendingListenFrameLen_));
    free(pendingListenFrame_);
    pendingListenFrame_ = nullptr;
    pendingListenFrameLen_ = 0;
  }
  char *buf = static_cast<char *>(malloc(length + 1));
  if (!buf) {
    Serial.printf("[listen] frame drop malloc fail len=%u heap=%u\n",
                  static_cast<unsigned>(length), ESP.getFreeHeap());
    return;
  }
  memcpy(buf, payload, length);
  buf[length] = '\0';
  pendingListenFrame_ = buf;
  pendingListenFrameLen_ = length;
}

void HubClient::drainPendingListenFrame() {
  if (!pendingListenFrame_) return;
  char *frame = pendingListenFrame_;
  const size_t len = pendingListenFrameLen_;
  pendingListenFrame_ = nullptr;
  pendingListenFrameLen_ = 0;
  handleListenMessage(frame, len);
  free(frame);
}

bool HubClient::connectListen(const char *shareToken, HubCallHandler onCall) {
  onCall_ = onCall;
  if (ws_) {
    return true;
  }
  listenUp_ = false;

  auto *ws = new WebSocketsClient();
  const String path = String("/public/ws/") + shareToken + "?seed=0&format=mp3&inline=0";
  if (useTls_) {
    (void)tlsInsecure_;
    ws->beginSSL(host_.c_str(), port_, path.c_str(), "");
  } else {
    ws->begin(host_.c_str(), port_, path.c_str());
  }
  ws->onEvent(onWebSocketEvent);
  // Keepalive ping only — disconnectTimeoutCount=0 never tears down on missed pong.
  ws->enableHeartbeat(30000, 20000, 0);
  ws->setReconnectInterval(5000);
  ws_ = ws;
  g_activeHub = this;
  return true;
}

void HubClient::listenLoop() {
  if (!ws_) return;
  static_cast<WebSocketsClient *>(ws_)->loop();
  drainPendingListenFrame();
}

void HubClient::disconnectListen() {
  listenUp_ = false;
  if (pendingListenFrame_) {
    free(pendingListenFrame_);
    pendingListenFrame_ = nullptr;
    pendingListenFrameLen_ = 0;
  }
  if (!ws_) return;
  auto *ws = static_cast<WebSocketsClient *>(ws_);
  ws->disconnect();
  delete ws;
  ws_ = nullptr;
  if (g_activeHub == this) {
    g_activeHub = nullptr;
  }
}

bool HubClient::uploadPtt(const char *radioSetId,
                          const uint8_t *wavData,
                          size_t wavLen,
                          float durationSec,
                          const char *clientId,
                          int64_t *outCallId) {
  if (!sessionToken_.length() || !wavData || wavLen == 0) return false;

  const String boundary = "----SignalForgeHandheld7d4f";
  String prelude;
  prelude += "--" + boundary + "\r\n";
  prelude += "Content-Disposition: form-data; name=\"audio\"; filename=\"ptt.wav\"\r\n";
  prelude += "Content-Type: audio/wav\r\n\r\n";

  String fields;
  fields += "\r\n--" + boundary + "\r\n";
  fields += "Content-Disposition: form-data; name=\"duration\"\r\n\r\n";
  fields += String(durationSec, 2);
  fields += "\r\n--" + boundary + "\r\n";
  fields += "Content-Disposition: form-data; name=\"clientId\"\r\n\r\n";
  fields += clientId;
  fields += "\r\n--" + boundary + "--\r\n";

  const size_t totalLen = prelude.length() + wavLen + fields.length();
  uint8_t *body = static_cast<uint8_t *>(malloc(totalLen));
  if (!body) return false;

  size_t offset = 0;
  memcpy(body + offset, prelude.c_str(), prelude.length());
  offset += prelude.length();
  memcpy(body + offset, wavData, wavLen);
  offset += wavLen;
  memcpy(body + offset, fields.c_str(), fields.length());

  const String path = String("/api/v1/radio-sets/") + radioSetId + "/ptt";
  int status = 0;
  String response;
  const String contentType = "multipart/form-data; boundary=" + boundary;
  const bool ok = httpRequest("POST", path.c_str(), contentType.c_str(), body, totalLen,
                              sessionToken_.c_str(), &status, &response);
  free(body);
  if (!ok || status < 200 || status >= 300) {
    Serial.printf("ptt upload failed status=%d body=%s\n", status, response.c_str());
    return false;
  }

  JsonDocument doc;
  if (deserializeJson(doc, response)) {
    return false;
  }
  if (outCallId) {
    *outCallId = doc["callId"] | 0LL;
  }
  return true;
}
