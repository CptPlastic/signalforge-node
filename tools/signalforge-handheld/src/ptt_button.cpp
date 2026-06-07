#include "ptt_button.h"

void PttButton::begin(uint8_t pin) {
  pin_ = pin;
  pinMode(pin_, INPUT_PULLUP);
  stable_ = digitalRead(pin_) == LOW;
  pressed_ = stable_;
}

void PttButton::loop() {
  justPressed_ = false;
  justReleased_ = false;
  if (pin_ == 255) return;

  const bool raw = digitalRead(pin_) == LOW;
  const uint32_t now = millis();
  if (raw != stable_ && (now - lastChangeMs_) > 25) {
    stable_ = raw;
    lastChangeMs_ = now;
    if (stable_ && !pressed_) {
      pressed_ = true;
      justPressed_ = true;
    } else if (!stable_ && pressed_) {
      pressed_ = false;
      justReleased_ = true;
    }
  }
}
