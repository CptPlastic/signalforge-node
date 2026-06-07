#pragma once

#include <Arduino.h>

class PttButton {
 public:
  void begin(uint8_t pin);
  void loop();
  bool pressed() const { return pressed_; }
  bool justPressed() const { return justPressed_; }
  bool justReleased() const { return justReleased_; }

 private:
  uint8_t pin_ = 255;
  bool pressed_ = false;
  bool justPressed_ = false;
  bool justReleased_ = false;
  bool stable_ = false;
  uint32_t lastChangeMs_ = 0;
};
