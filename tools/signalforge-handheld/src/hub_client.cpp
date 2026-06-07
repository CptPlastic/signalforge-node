#include "hub_client.h"

#include <ArduinoJson.h>
#include <HTTPClient.h>
#include <Preferences.h>
#include <WebSocketsClient.h>
#include <WiFiClientSecure.h>
#include <mbedtls/base64.h>

#include "hub_config.h"

namespace {
constexpr const char *kPrefsNs = "sfhandheld";
constexpr const char *kPrefsToken = "token";

bool decodeBase64(const char *input, uint8_t **outData, size_t *outLen) {
  if (!input || !outData || !outLen) return false;
  const size_t inLen = strlen(input);
  if (inLen == 0) return false;
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

bool HubClient::handleListenMessage(const char *payload, size_t len) {
  if (!payload || len == 0) return false;
  char *json = static_cast<char *>(malloc(len + 1));
  if (!json) return false;
  memcpy(json, payload, len);
  json[len] = '\0';

  JsonDocument doc;
  const DeserializationError err = deserializeJson(doc, json);
  free(json);
  if (err) {
    return false;
  }

  const char *cmd = doc["cmd"] | "";
  if (strcmp(cmd, "call") != 0) {
    return false;
  }

  HubCallEvent event;
  event.id = doc["id"] | 0LL;
  event.talkgroupLabel = doc["talkgroupLabel"].as<String>();
  event.systemLabel = doc["systemLabel"].as<String>();
  event.origin = doc["origin"].as<String>();
  event.senderEmail = doc["senderEmail"].as<String>();
  event.audioType = doc["audioType"].as<String>();

  const char *audioB64 = doc["audio"];
  if (!audioB64 || !onCall_) {
    return false;
  }
  if (!decodeBase64(audioB64, &event.audio, &event.audioLen)) {
    return false;
  }

  onCall_(event);
  free(event.audio);
  return true;
}

namespace {
HubClient *g_activeHub = nullptr;

void onWebSocketEvent(WStype_t type, uint8_t *payload, size_t length) {
  if (!g_activeHub) return;
  switch (type) {
    case WStype_CONNECTED:
      Serial.println("listen ws connected");
      break;
    case WStype_DISCONNECTED:
      Serial.println("listen ws disconnected");
      break;
    case WStype_TEXT:
      g_activeHub->handleListenMessage(reinterpret_cast<const char *>(payload), length);
      break;
    default:
      break;
  }
}
}  // namespace

bool HubClient::connectListen(const char *shareToken, HubCallHandler onCall) {
  onCall_ = onCall;
  disconnectListen();

  auto *ws = new WebSocketsClient();
  const String path = String("/public/ws/") + shareToken;
  if (useTls_) {
    // Empty fingerprint uses WebSocketsClient's ESP32 TLS path (setInsecure when no CA set).
    // Set HUB_TLS_INSECURE=1 in hub_config.h for self-signed / lab hubs.
    (void)tlsInsecure_;
    ws->beginSSL(host_.c_str(), port_, path.c_str(), "");
  } else {
    ws->begin(host_.c_str(), port_, path.c_str());
  }
  ws->onEvent(onWebSocketEvent);
  ws->setReconnectInterval(5000);
  ws_ = ws;
  g_activeHub = this;
  return true;
}

void HubClient::listenLoop() {
  if (!ws_) return;
  static_cast<WebSocketsClient *>(ws_)->loop();
}

void HubClient::disconnectListen() {
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
