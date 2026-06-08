#include "display.h"

#include <Wire.h>
#include <Adafruit_GFX.h>
#include <Adafruit_SSD1306.h>

#include "pins.h"

namespace {
Adafruit_SSD1306 display(OLED_WIDTH, OLED_HEIGHT, &Wire, -1);
uint8_t g_oledAddr = 0;
bool g_i2cStarted = false;

bool i2cProbe(uint8_t addr) {
  Wire.beginTransmission(addr);
  return Wire.endTransmission() == 0;
}

void ensureI2c() {
  if (!g_i2cStarted) {
    Wire.begin(PIN_I2C_SDA, PIN_I2C_SCL);
    Wire.setClock(100000);
    g_i2cStarted = true;
  }
}

void applyNightMode() {
  display.invertDisplay(false);
  display.dim(false);
  display.ssd1306_command(SSD1306_SETCONTRAST);
  display.ssd1306_command(OLED_CONTRAST);
}

void logI2cScan() {
  ensureI2c();
  Serial.printf("[oled] scan sda=%d scl=%d:\n", PIN_I2C_SDA, PIN_I2C_SCL);
  uint8_t found = 0;
  for (uint8_t addr = 1; addr < 127; addr++) {
    if (i2cProbe(addr)) {
      Serial.printf("[oled]   device 0x%02X\n", addr);
      found++;
    }
  }
  if (found == 0) {
    Serial.println("[oled]   (no devices — check wiring / power)");
  }
}

uint8_t findOledAddress() {
  constexpr uint8_t kCandidates[] = {OLED_I2C_ADDR, 0x3D};
  for (uint8_t addr : kCandidates) {
    if (i2cProbe(addr)) {
      return addr;
    }
  }
  return 0;
}

void pushFrame(const char *tag) {
  if (!g_oledAddr) return;
  ensureI2c();
  display.display();
  if (!i2cProbe(g_oledAddr)) {
    Serial.printf("[oled] lost on bus after %s\n", tag);
  }
}

void printYellowStatus(const char *text) {
  display.setTextSize(1);
  display.setCursor(OLED_X_OFFSET, OLED_Y_STATUS);
  display.println(text);
}

constexpr size_t kOledCharsPerLine = 21;

const char *findMiddleDotUtf8(const char *text) {
  if (!text) return nullptr;
  for (const char *p = text; p[0] != '\0'; p++) {
    if (static_cast<uint8_t>(p[0]) == 0xC2 && static_cast<uint8_t>(p[1]) == 0xB7) {
      return p;
    }
  }
  return nullptr;
}

void copyTruncated(char *dest, size_t destLen, const char *src) {
  if (!dest || destLen == 0) return;
  if (!src || !src[0]) {
    dest[0] = '\0';
    return;
  }
  strncpy(dest, src, destLen - 1);
  dest[destLen - 1] = '\0';
  if (strlen(src) > destLen - 1 && destLen >= 4) {
    dest[destLen - 4] = '.';
    dest[destLen - 3] = '.';
    dest[destLen - 2] = '.';
    dest[destLen - 1] = '\0';
  }
}

void splitTalkgroupLabel(const char *label, char *primary, size_t primaryLen, char *secondary,
                         size_t secondaryLen) {
  primary[0] = secondary[0] = '\0';
  if (!label || !label[0]) {
    copyTruncated(primary, primaryLen, "waiting");
    return;
  }
  const char *dot = findMiddleDotUtf8(label);
  if (dot && dot > label) {
    size_t firstLen = static_cast<size_t>(dot - label);
    while (firstLen > 0 && label[firstLen - 1] == ' ') {
      firstLen--;
    }
    if (firstLen >= primaryLen) {
      copyTruncated(primary, primaryLen, label);
      return;
    }
    memcpy(primary, label, firstLen);
    primary[firstLen] = '\0';
    const char *rest = dot + 2;
    while (*rest == ' ') {
      rest++;
    }
    copyTruncated(secondary, secondaryLen, rest);
    return;
  }
  copyTruncated(primary, primaryLen, label);
}

void printBlueTitle(const char *text) {
  display.setTextSize(1);
  display.setCursor(OLED_X_OFFSET, OLED_Y_BLUE_TITLE);
  display.println(text);
}

void printBlueLine(int y, const char *text) {
  display.setTextSize(1);
  display.setCursor(OLED_X_OFFSET, y);
  display.println(text);
}
}  // namespace

bool FieldDisplay::begin() {
  ready_ = false;
  g_oledAddr = 0;
  ensureI2c();

  g_oledAddr = findOledAddress();
  if (g_oledAddr == 0) {
    Serial.println("[oled] not found — serial-only mode is fine");
    logI2cScan();
    return false;
  }

  Serial.printf("[oled] detected at 0x%02X\n", g_oledAddr);

  const uint8_t vccMode =
      OLED_EXTERNAL_VCC ? SSD1306_EXTERNALVCC : SSD1306_SWITCHCAPVCC;
  if (!display.begin(vccMode, g_oledAddr, false, false)) {
    Serial.println("[oled] driver init failed (malloc?)");
    return false;
  }

  applyNightMode();

  ready_ = true;
  Serial.printf("[oled] ready night mode contrast=0x%02X offset=%d\n", OLED_CONTRAST, OLED_X_OFFSET);
  showBoot();
  return true;
}

void FieldDisplay::clearLine(int row, const char *text, uint8_t size) {
  if (!ready_) return;
  ensureI2c();
  applyNightMode();
  const int y = (row == 0) ? OLED_Y_STATUS : OLED_Y_BLUE_LINE2;
  display.setTextSize(size);
  display.setTextColor(SSD1306_WHITE);
  display.setCursor(OLED_X_OFFSET, y);
  display.fillRect(0, y, OLED_WIDTH, size >= 2 ? 16 : 8, SSD1306_BLACK);
  display.println(text);
  pushFrame("line");
}

void FieldDisplay::showBoot() {
  if (!ready_) return;
  ensureI2c();
  display.clearDisplay();
  applyNightMode();
  display.setTextColor(SSD1306_WHITE);
  printYellowStatus("SignalForge");
  printBlueTitle("Booting...");
  pushFrame("boot");
}

void FieldDisplay::showWifi(const char *ssid, int8_t rssi) {
  if (!ready_) return;
  ensureI2c();
  display.clearDisplay();
  applyNightMode();
  display.setTextColor(SSD1306_WHITE);
  char status[22];
  snprintf(status, sizeof(status), "WiFi %ddBm", rssi);
  printYellowStatus(status);
  printBlueTitle("Connected");
  printBlueLine(OLED_Y_BLUE_LINE1, ssid);
  pushFrame("wifi");
}

void FieldDisplay::showLogin(bool ok, const char *detail) {
  if (!ready_) return;
  ensureI2c();
  display.clearDisplay();
  applyNightMode();
  display.setTextColor(SSD1306_WHITE);
  printYellowStatus(ok ? "Auth OK" : "Auth FAIL");
  printBlueTitle(ok ? "LOGIN OK" : "LOGIN FAIL");
  if (detail && detail[0]) {
    printBlueLine(OLED_Y_BLUE_LINE1, detail);
  }
  pushFrame("login");
}

void FieldDisplay::showMonitor(const FieldMonitorInfo &info) {
  if (!ready_) return;
  ensureI2c();
  display.clearDisplay();
  applyNightMode();
  display.setTextColor(SSD1306_WHITE);

  char header[22];
  snprintf(header, sizeof(header), "%s %ddBm", info.wsUp ? "LIVE" : "DOWN", info.rssi);
  if (info.pttSession) {
    strlcat(header, " PTT", sizeof(header));
  }
  printYellowStatus(header);

  char tgPrimary[22];
  char tgSecondary[22];
  splitTalkgroupLabel(info.talkgroup, tgPrimary, sizeof(tgPrimary), tgSecondary, sizeof(tgSecondary));
  printBlueTitle(tgPrimary);

  int nextY = OLED_Y_BLUE_LINE1;
  if (tgSecondary[0]) {
    printBlueLine(nextY, tgSecondary);
    nextY += 10;
  } else if (info.sender && info.sender[0] && strcmp(info.origin, "ptt") == 0) {
    printBlueLine(nextY, info.sender);
    nextY += 10;
  }

  if (info.system && info.system[0] && nextY <= OLED_Y_BLUE_LINE3) {
    char sysLine[22];
    copyTruncated(sysLine, sizeof(sysLine), info.system);
    printBlueLine(nextY, sysLine);
    nextY += 10;
  }

  if (nextY <= OLED_Y_BLUE_LINE3) {
    char detail[22];
    detail[0] = '\0';
    if (info.durationSec > 0.05f) {
      snprintf(detail, sizeof(detail), "%.1fs", info.durationSec);
    }
    if (info.origin && info.origin[0]) {
      if (detail[0]) {
        strlcat(detail, " ", sizeof(detail));
      }
      strlcat(detail, info.origin, sizeof(detail));
    }
    if (detail[0]) {
      printBlueLine(nextY, detail);
      nextY += 10;
    }
  }

  if (nextY <= OLED_Y_BLUE_LINE4) {
    char footer[22];
    footer[0] = '\0';
    if (info.frequencyHz > 0) {
      if (info.frequencyHz >= 1000000) {
        snprintf(footer, sizeof(footer), "%.3fM", info.frequencyHz / 1000000.0f);
      } else if (info.frequencyHz >= 1000) {
        snprintf(footer, sizeof(footer), "%.3fk", info.frequencyHz / 1000.0f);
      } else {
        snprintf(footer, sizeof(footer), "%dHz", info.frequencyHz);
      }
    }
    if (info.callId > 0) {
      char idbuf[12];
      snprintf(idbuf, sizeof(idbuf), " #%lld", static_cast<long long>(info.callId));
      strlcat(footer, idbuf, sizeof(footer));
    }
    if (footer[0]) {
      printBlueLine(nextY, footer);
    }
  }

  pushFrame("mon");
}

void FieldDisplay::showPttRecording(float seconds) {
  char line[16];
  snprintf(line, sizeof(line), "TX %.1fs", seconds);
  clearLine(1, line, 2);
}

void FieldDisplay::showPttUpload(const char *detail) {
  if (!ready_) return;
  ensureI2c();
  display.clearDisplay();
  applyNightMode();
  display.setTextColor(SSD1306_WHITE);
  printYellowStatus("PTT upload");
  printBlueTitle("PTT");
  if (detail && detail[0]) {
    printBlueLine(OLED_Y_BLUE_LINE1, detail);
  }
  pushFrame("ptt");
}

void FieldDisplay::showError(const char *title, const char *detail) {
  if (!ready_) return;
  ensureI2c();
  display.clearDisplay();
  applyNightMode();
  display.setTextColor(SSD1306_WHITE);
  printYellowStatus("Error");
  printBlueTitle(title);
  if (detail && detail[0]) {
    printBlueLine(OLED_Y_BLUE_LINE1, detail);
  }
  pushFrame("err");
}
