#pragma once

#include <Arduino.h>

class AudioOut {
 public:
  bool begin();
  bool isReady() const { return output_ != nullptr; }
  bool playMp3(const uint8_t *data, size_t len);
  bool playWav(const uint8_t *data, size_t len);
  void loop();
  void stop();
  bool isPlaying() const { return playing_; }

 private:
  bool playing_ = false;
  void teardown();
  void *mp3_ = nullptr;
  void *source_ = nullptr;
  void *output_ = nullptr;
};
