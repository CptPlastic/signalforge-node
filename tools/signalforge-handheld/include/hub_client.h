#pragma once

#include <Arduino.h>
#include <functional>

struct HubCallEvent {
  int64_t id = 0;
  String talkgroupLabel;
  String systemLabel;
  String origin;
  String senderEmail;
  String audioType;
  uint8_t *audio = nullptr;
  size_t audioLen = 0;
};

using HubCallHandler = std::function<void(const HubCallEvent &)>;

class HubClient {
 public:
  bool begin(const char *host, uint16_t port, bool useTls, bool tlsInsecure);
  bool login(const char *email, const char *password);
  bool ensureSession();
  void clearSession();
  const String &sessionToken() const { return sessionToken_; }

  bool connectListen(const char *shareToken, HubCallHandler onCall);
  void listenLoop();
  void disconnectListen();

  bool uploadPtt(const char *radioSetId,
                 const uint8_t *wavData,
                 size_t wavLen,
                 float durationSec,
                 const char *clientId,
                 int64_t *outCallId);

  bool handleListenMessage(const char *payload, size_t len);

 private:
  String host_;
  uint16_t port_ = 443;
  bool useTls_ = true;
  bool tlsInsecure_ = false;
  String sessionToken_;
  HubCallHandler onCall_;
  void *ws_ = nullptr;

  bool httpRequest(const char *method,
                   const char *path,
                   const char *contentType,
                   const uint8_t *body,
                   size_t bodyLen,
                   const char *authBearer,
                   int *outStatus,
                   String *outBody);
};
