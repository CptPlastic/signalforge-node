#pragma once

#include <Arduino.h>

struct FieldMonitorInfo {
  bool wsUp = false;
  int8_t rssi = 0;
  bool pttSession = false;
  const char *talkgroup = "";
  const char *system = "";
  const char *origin = "";
  const char *sender = "";
  float durationSec = 0;
  int frequencyHz = 0;
  int64_t callId = 0;
};

class FieldDisplay {
 public:
  bool begin();
  void showBoot();
  void showWifi(const char *ssid, int8_t rssi);
  void showLogin(bool ok, const char *detail);
  void showMonitor(const FieldMonitorInfo &info);
  void showPttRecording(float seconds);
  void showPttUpload(const char *detail);
  void showError(const char *title, const char *detail);

 private:
  bool ready_ = false;
  void clearLine(int row, const char *text, uint8_t size = 1);
};
