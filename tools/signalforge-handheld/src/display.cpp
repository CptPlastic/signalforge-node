#include "display.h"

#include <Wire.h>
#include <Adafruit_GFX.h>
#include <Adafruit_SSD1306.h>

#include "pins.h"

namespace {
Adafruit_SSD1306 display(OLED_WIDTH, OLED_HEIGHT, &Wire, -1);
}

bool FieldDisplay::begin() {
  Wire.begin(PIN_I2C_SDA, PIN_I2C_SCL);
  if (!display.begin(SSD1306_SWITCHCAPVCC, OLED_I2C_ADDR)) {
    ready_ = false;
    return false;
  }
  display.clearDisplay();
  display.display();
  ready_ = true;
  return true;
}

void FieldDisplay::clearLine(int row, const char *text, uint8_t size) {
  if (!ready_) return;
  display.setTextSize(size);
  display.setTextColor(SSD1306_WHITE);
  display.setCursor(0, row * 10);
  display.fillRect(0, row * 10, OLED_WIDTH, 10, SSD1306_BLACK);
  display.println(text);
  display.display();
}

void FieldDisplay::showBoot() {
  if (!ready_) return;
  display.clearDisplay();
  display.setTextSize(1);
  display.setCursor(0, 0);
  display.println("SignalForge");
  display.println("Field Unit");
  display.println("");
  display.println("Booting...");
  display.display();
}

void FieldDisplay::showWifi(const char *ssid, int8_t rssi) {
  if (!ready_) return;
  display.clearDisplay();
  display.setCursor(0, 0);
  display.println("WiFi");
  display.print("SSID: ");
  display.println(ssid);
  display.print("RSSI: ");
  display.println(rssi);
  display.display();
}

void FieldDisplay::showLogin(bool ok, const char *detail) {
  if (!ready_) return;
  display.clearDisplay();
  display.setCursor(0, 0);
  display.println(ok ? "Login OK" : "Login FAIL");
  display.println(detail);
  display.display();
}

void FieldDisplay::showMonitor(bool wsConnected, const char *talkgroup, const char *statusLine) {
  if (!ready_) return;
  display.clearDisplay();
  display.setCursor(0, 0);
  display.print(wsConnected ? "MON " : "OFF ");
  display.println(wsConnected ? "LIVE" : "DOWN");
  display.println(talkgroup);
  display.println(statusLine);
  display.display();
}

void FieldDisplay::showPttRecording(float seconds) {
  clearLine(3, ("TX " + String(seconds, 1) + "s").c_str());
}

void FieldDisplay::showPttUpload(const char *detail) {
  if (!ready_) return;
  display.clearDisplay();
  display.setCursor(0, 0);
  display.println("PTT UPLOAD");
  display.println(detail);
  display.display();
}

void FieldDisplay::showError(const char *title, const char *detail) {
  if (!ready_) return;
  display.clearDisplay();
  display.setCursor(0, 0);
  display.println(title);
  display.println(detail);
  display.display();
}
