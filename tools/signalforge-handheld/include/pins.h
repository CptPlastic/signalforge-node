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
// Dual-color 0.96" modules: pixels 0–15 yellow, 16–63 blue. Keep big text below 16.
#define OLED_YELLOW_BOTTOM 16
#define OLED_Y_STATUS 0
#define OLED_Y_BLUE_TITLE 17
#define OLED_Y_BLUE_LINE1 26
#define OLED_Y_BLUE_LINE2 36
#define OLED_Y_BLUE_LINE3 46
#define OLED_Y_BLUE_LINE4 56
#define OLED_I2C_ADDR 0x3C
// Charge-pump mode for most 0.96" modules (VCC on 3.3V or 5V through onboard reg).
// If detected but very dim/blank, try 1 (VCC wired straight to 3.3V only).
#define OLED_EXTERNAL_VCC 0
// If white flash works but text is invisible, try 2 (common on SH1106 1.3" clones).
#define OLED_X_OFFSET 0
// Pixel brightness: 0x00–0xFF. Lower = dimmer (better night light discipline).
#define OLED_CONTRAST 0x5F
