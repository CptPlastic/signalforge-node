#pragma once

#include <Arduino.h>

class AudioIn {
 public:
  bool begin(uint32_t sampleRate);
  void start();
  void stop();
  bool isRecording() const { return recording_; }

  // Returns WAV bytes allocated in heap/PSRAM. Caller must free().
  void captureLoop();
  uint8_t *buildWav(size_t *outLen, float *outDurationSec);

  uint32_t maxSeconds() const { return maxSeconds_; }

 private:
  uint32_t sampleRate_ = 16000;
  uint32_t maxSeconds_ = 30;
  bool recording_ = false;
  int16_t *pcm_ = nullptr;
  size_t pcmCapacitySamples_ = 0;
  size_t pcmSamples_ = 0;
  void freeBuffer();
};
