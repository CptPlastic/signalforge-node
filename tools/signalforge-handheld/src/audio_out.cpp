#include "audio_out.h"

#include <AudioFileSource.h>
#include <AudioGeneratorMP3.h>
#include <AudioGeneratorWAV.h>
#include <AudioOutputI2S.h>

#include "pins.h"

namespace {
class RamAudioSource : public AudioFileSource {
 public:
  RamAudioSource(const uint8_t *data, size_t len) : data_(data), len_(len) {}

  uint32_t read(void *data, uint32_t len) override {
    if (pos_ >= len_) return 0;
    const uint32_t toRead = min(len, static_cast<uint32_t>(len_ - pos_));
    memcpy(data, data_ + pos_, toRead);
    pos_ += toRead;
    return toRead;
  }

  bool seek(int32_t pos, int dir) override {
    if (dir == SEEK_SET) {
      pos_ = static_cast<size_t>(max<int32_t>(0, pos));
      return true;
    }
    if (dir == SEEK_CUR) {
      pos_ = min(len_, pos_ + static_cast<size_t>(max<int32_t>(0, pos)));
      return true;
    }
    return false;
  }

  bool close() override { return true; }
  bool isOpen() override { return data_ != nullptr; }
  uint32_t getSize() override { return static_cast<uint32_t>(len_); }

 private:
  const uint8_t *data_ = nullptr;
  size_t len_ = 0;
  size_t pos_ = 0;
};

bool looksLikeMp3(const uint8_t *data, size_t len) {
  if (len < 3) return false;
  if (data[0] == 'I' && data[1] == 'D' && data[2] == '3') return true;
  if (data[0] == 0xff && (data[1] & 0xe0) == 0xe0) return true;
  return false;
}
}  // namespace

bool AudioOut::begin() {
  teardown();
  auto *out = new AudioOutputI2S();
  out->SetPinout(PIN_SPK_BCLK, PIN_SPK_LRC, PIN_SPK_DOUT);
  out->SetGain(0.35f);
  output_ = out;
  return true;
}

void AudioOut::teardown() {
  stop();
  if (mp3_) {
    delete static_cast<AudioGenerator *>(mp3_);
    mp3_ = nullptr;
  }
  if (source_) {
    delete static_cast<AudioFileSource *>(source_);
    source_ = nullptr;
  }
  if (output_) {
    delete static_cast<AudioOutputI2S *>(output_);
    output_ = nullptr;
  }
}

bool AudioOut::playMp3(const uint8_t *data, size_t len) {
  if (!output_ || !data || len == 0) return false;
  stop();
  auto *src = new RamAudioSource(data, len);
  auto *gen = new AudioGeneratorMP3();
  source_ = src;
  mp3_ = gen;
  if (!gen->begin(src, static_cast<AudioOutputI2S *>(output_))) {
    stop();
    return false;
  }
  playing_ = true;
  return true;
}

bool AudioOut::playWav(const uint8_t *data, size_t len) {
  if (!output_ || !data || len == 0) return false;
  stop();
  auto *src = new RamAudioSource(data, len);
  auto *gen = new AudioGeneratorWAV();
  source_ = src;
  mp3_ = gen;
  if (!gen->begin(src, static_cast<AudioOutputI2S *>(output_))) {
    stop();
    return false;
  }
  playing_ = true;
  return true;
}

void AudioOut::loop() {
  if (!playing_ || !mp3_) return;
  auto *gen = static_cast<AudioGenerator *>(mp3_);
  if (!gen->isRunning()) {
    playing_ = false;
    return;
  }
  if (!gen->loop()) {
    gen->stop();
    playing_ = false;
  }
}

void AudioOut::stop() {
  if (mp3_) {
    auto *gen = static_cast<AudioGenerator *>(mp3_);
    if (gen->isRunning()) gen->stop();
  }
  playing_ = false;
}
