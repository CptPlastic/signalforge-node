#pragma once

#include <Arduino.h>

class HubClient;

struct FieldDeviceStatus {
  bool speakerReady = false;
  bool micReady = false;
  bool audioPlaybackEnabled = false;
  bool wifiUp = false;
  bool listenUp = false;
};

void serialCliPrintHelp();
void serialCliHandleLine(const char *line, HubClient &hub, const FieldDeviceStatus &status);
