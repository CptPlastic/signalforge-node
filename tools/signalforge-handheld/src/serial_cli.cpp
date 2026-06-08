#include "serial_cli.h"

#include <Preferences.h>

#include "hub_client.h"

namespace {
constexpr const char *kPrefsNs = "sfhandheld";

void trimInPlace(char *line) {
  if (!line) return;
  size_t len = strlen(line);
  while (len > 0 && (line[len - 1] == '\r' || line[len - 1] == '\n' || line[len - 1] == ' ')) {
    line[--len] = '\0';
  }
  size_t start = 0;
  while (line[start] == ' ') start++;
  if (start > 0) memmove(line, line + start, strlen(line + start) + 1);
}

char *nextToken(char **cursor) {
  if (!cursor || !*cursor) return nullptr;
  while (**cursor == ' ') (*cursor)++;
  if (**cursor == '\0') return nullptr;
  char *start = *cursor;
  while (**cursor && **cursor != ' ') (*cursor)++;
  if (**cursor) {
    **cursor = '\0';
    (*cursor)++;
  }
  return start;
}
}  // namespace

void serialCliPrintHelp() {
  Serial.println("[serial] commands:");
  Serial.println("  help");
  Serial.println("  status");
  Serial.println("  login <email> <password>   (PTT only — session cached, password not stored)");
  Serial.println("  logout");
}

void serialCliHandleLine(const char *line, HubClient &hub, const FieldDeviceStatus &status) {
  char buf[256];
  strncpy(buf, line, sizeof(buf) - 1);
  buf[sizeof(buf) - 1] = '\0';
  trimInPlace(buf);
  if (buf[0] == '\0') return;

  char *cursor = buf;
  char *cmd = nextToken(&cursor);
  if (!cmd) return;

  if (strcmp(cmd, "help") == 0) {
    serialCliPrintHelp();
    return;
  }

  if (strcmp(cmd, "status") == 0) {
    Serial.printf("[status] wifi=%s listen=%s session=%s\n", status.wifiUp ? "up" : "down",
                  status.listenUp ? "up" : "down", hub.ensureSession() ? "ok" : "missing");
    Serial.printf("[status] speaker=%s mic=%s playback=%s heap=%u\n",
                  status.speakerReady ? "ready" : "off", status.micReady ? "ready" : "off",
                  status.audioPlaybackEnabled ? "on" : "off", ESP.getFreeHeap());
    return;
  }

  if (strcmp(cmd, "logout") == 0) {
    hub.clearSession();
    Serial.println("[auth] session cleared");
    return;
  }

  if (strcmp(cmd, "login") == 0) {
    char *email = nextToken(&cursor);
    char *password = nextToken(&cursor);
    if (!email || !password || !strchr(email, '@')) {
      Serial.println("[auth] usage: login <email> <password>");
      return;
    }
    if (hub.login(email, password)) {
      Serial.println("[auth] login ok — session cached for PTT");
    } else {
      Serial.println("[auth] login failed — check TX enabled on hub account");
    }
    return;
  }

  Serial.println("[serial] unknown command (help)");
}
