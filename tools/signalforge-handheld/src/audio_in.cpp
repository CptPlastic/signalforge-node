#include "audio_in.h"

#include <driver/i2s.h>

#include "hub_config.h"

#include "pins.h"

namespace {
constexpr i2s_port_t kMicPort = I2S_NUM_1;

bool hasPsram() {
#if defined(BOARD_HAS_PSRAM)
  return ESP.getPsramSize() > 0;
#else
  return false;
#endif
}

void *allocAudio(size_t bytes) {
#if defined(BOARD_HAS_PSRAM)
  if (ESP.getPsramSize() > 0) {
    return heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
  }
#endif
  return malloc(bytes);
}
}  // namespace

bool AudioIn::begin(uint32_t sampleRate) {
  sampleRate_ = sampleRate;
  maxSeconds_ = hasPsram() ? PTT_MAX_SECONDS : PTT_MAX_SECONDS_NO_PSRAM;

  i2s_config_t config = {};
  config.mode = (i2s_mode_t)(I2S_MODE_MASTER | I2S_MODE_RX);
  config.sample_rate = sampleRate_;
  config.bits_per_sample = I2S_BITS_PER_SAMPLE_16BIT;
  config.channel_format = I2S_CHANNEL_FMT_ONLY_LEFT;
  config.communication_format = (i2s_comm_format_t)0x01;
  config.intr_alloc_flags = ESP_INTR_FLAG_LEVEL1;
  config.dma_buf_count = 4;
  config.dma_buf_len = 256;
  config.use_apll = false;
  config.tx_desc_auto_clear = false;
  config.fixed_mclk = 0;

  i2s_pin_config_t pins = {};
  pins.bck_io_num = PIN_MIC_SCK;
  pins.ws_io_num = PIN_MIC_WS;
  pins.data_out_num = I2S_PIN_NO_CHANGE;
  pins.data_in_num = PIN_MIC_SD;

  if (i2s_driver_install(kMicPort, &config, 0, nullptr) != ESP_OK) {
    return false;
  }
  if (i2s_set_pin(kMicPort, &pins) != ESP_OK) {
    return false;
  }
  i2s_zero_dma_buffer(kMicPort);
  return true;
}

void AudioIn::freeBuffer() {
  if (pcm_) {
    free(pcm_);
    pcm_ = nullptr;
  }
  pcmCapacitySamples_ = 0;
  pcmSamples_ = 0;
}

void AudioIn::start() {
  stop();
  pcmCapacitySamples_ = sampleRate_ * maxSeconds_;
  pcm_ = static_cast<int16_t *>(allocAudio(pcmCapacitySamples_ * sizeof(int16_t)));
  if (!pcm_) {
    pcmCapacitySamples_ = 0;
    return;
  }
  pcmSamples_ = 0;
  recording_ = true;
  i2s_zero_dma_buffer(kMicPort);
}

void AudioIn::stop() {
  recording_ = false;
}

uint8_t *AudioIn::buildWav(size_t *outLen, float *outDurationSec) {
  if (!pcm_ || pcmSamples_ == 0) {
    *outLen = 0;
    *outDurationSec = 0;
    return nullptr;
  }

  const uint16_t channels = 1;
  const uint16_t bits = 16;
  const uint32_t byteRate = sampleRate_ * channels * (bits / 8);
  const uint16_t blockAlign = channels * (bits / 8);
  const uint32_t dataSize = pcmSamples_ * sizeof(int16_t);
  const uint32_t chunkSize = 36 + dataSize;

  *outLen = 44 + dataSize;
  *outDurationSec = static_cast<float>(pcmSamples_) / static_cast<float>(sampleRate_);

  uint8_t *wav = static_cast<uint8_t *>(allocAudio(*outLen));
  if (!wav) {
    *outLen = 0;
    return nullptr;
  }

  auto write32 = [&](size_t offset, uint32_t value) {
    wav[offset] = value & 0xff;
    wav[offset + 1] = (value >> 8) & 0xff;
    wav[offset + 2] = (value >> 16) & 0xff;
    wav[offset + 3] = (value >> 24) & 0xff;
  };
  auto write16 = [&](size_t offset, uint16_t value) {
    wav[offset] = value & 0xff;
    wav[offset + 1] = (value >> 8) & 0xff;
  };

  memcpy(wav, "RIFF", 4);
  write32(4, chunkSize);
  memcpy(wav + 8, "WAVE", 4);
  memcpy(wav + 12, "fmt ", 4);
  write32(16, 16);
  write16(20, 1);
  write16(22, channels);
  write32(24, sampleRate_);
  write32(28, byteRate);
  write16(32, blockAlign);
  write16(34, bits);
  memcpy(wav + 36, "data", 4);
  write32(40, dataSize);
  memcpy(wav + 44, pcm_, dataSize);

  freeBuffer();
  return wav;
}

// Called from main loop while recording.
void AudioIn::captureLoop() {
  if (!recording_ || !pcm_ || pcmSamples_ >= pcmCapacitySamples_) return;
  int16_t sample[128];
  size_t bytesRead = 0;
  if (i2s_read(kMicPort, sample, sizeof(sample), &bytesRead, pdMS_TO_TICKS(10)) != ESP_OK) {
    return;
  }
  const size_t samplesRead = bytesRead / sizeof(int16_t);
  for (size_t i = 0; i < samplesRead && pcmSamples_ < pcmCapacitySamples_; ++i) {
    pcm_[pcmSamples_++] = sample[i];
  }
}
