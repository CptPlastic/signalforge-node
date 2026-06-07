#pragma once

// SignalForge handheld default wiring (change here if your board differs).
// OLED: I2C. Speaker: MAX98357A. Mic: INMP441. PTT: active-low to GND.

#define PIN_I2C_SDA 21
#define PIN_I2C_SCL 22

#define PIN_SPK_BCLK 26
#define PIN_SPK_LRC 25
#define PIN_SPK_DOUT 27

#define PIN_MIC_SCK 14
#define PIN_MIC_WS 15
#define PIN_MIC_SD 32

#define PIN_PTT 33
#define PIN_STATUS_LED 2

#define OLED_WIDTH 128
#define OLED_HEIGHT 64
#define OLED_I2C_ADDR 0x3C
