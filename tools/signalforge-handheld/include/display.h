#pragma once

#include <Arduino.h>

class FieldDisplay {
 public:
  bool begin();
  void showBoot();
  void showWifi(const char *ssid, int8_t rssi);
  void showLogin(bool ok, const char *detail);
  void showMonitor(bool wsConnected, const char *talkgroup, const char *statusLine);
  void showPttRecording(float seconds);
  void showPttUpload(const char *detail);
  void showError(const char *title, const char *detail);

 private:
  bool ready_ = false;
  void clearLine(int row, const char *text, uint8_t size = 1);
};
