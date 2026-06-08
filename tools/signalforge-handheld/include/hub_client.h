#pragma once

#include <Arduino.h>
#include <functional>

struct HubCallEvent {
  int64_t id = 0;
  String talkgroupLabel;
  String talkgroupGroup;
  String systemLabel;
  String origin;
  String senderEmail;
  String audioType;
  float durationSec = 0;
  int frequencyHz = 0;
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
  bool listenConnected() const { return listenUp_; }
  bool listenClientActive() const { return ws_ != nullptr; }
  bool hasPendingListenFrame() const { return pendingListenFrame_ != nullptr; }

  void setListenConnected(bool up) { listenUp_ = up; }

  bool uploadPtt(const char *radioSetId,
                 const uint8_t *wavData,
                 size_t wavLen,
                 float durationSec,
                 const char *clientId,
                 int64_t *outCallId);

  bool handleListenMessage(const char *payload, size_t len);
  void onListenTextFrame(const uint8_t *payload, size_t length);
  bool fetchPublicCallAudio(const char *shareToken,
                            int64_t callId,
                            uint8_t **outData,
                            size_t *outLen,
                            String *outType);

 private:
  String host_;
  uint16_t port_ = 443;
  bool useTls_ = true;
  bool tlsInsecure_ = false;
  String sessionToken_;
  HubCallHandler onCall_;
  void *ws_ = nullptr;
  bool listenUp_ = false;
  char *pendingListenFrame_ = nullptr;
  size_t pendingListenFrameLen_ = 0;

  void queueListenFrame(const uint8_t *payload, size_t length);
  void drainPendingListenFrame();

  bool httpRequest(const char *method,
                   const char *path,
                   const char *contentType,
                   const uint8_t *body,
                   size_t bodyLen,
                   const char *authBearer,
                   int *outStatus,
                   String *outBody);

  bool httpGetBinary(const char *path, int *outStatus, uint8_t **outData, size_t *outLen, String *outType);
};
