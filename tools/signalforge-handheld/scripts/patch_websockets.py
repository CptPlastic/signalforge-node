"""Patch links2004/WebSockets for ESP32 handheld constraints."""

import os

Import("env")  # type: ignore[name-defined]  # PlatformIO SCons


def patch_file(path: str, replacements: list[tuple[str, str]], label: str) -> None:
    if not os.path.isfile(path):
        return
    with open(path, encoding="utf-8") as fh:
        text = fh.read()
    changed = False
    for needle, replacement in replacements:
        if needle in text and replacement not in text:
            text = text.replace(needle, replacement)
            changed = True
    if changed:
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(text)
        print(f"[patch] {label}: {path}")


def patch_websockets() -> None:
    base = os.path.join(
        env["PROJECT_DIR"],
        ".pio",
        "libdeps",
        env["PIOENV"],
        "WebSockets",
        "src",
    )

    patch_file(
        os.path.join(base, "WebSockets.h"),
        [
            (
                "#define WEBSOCKETS_MAX_DATA_SIZE (15 * 1024)",
                "#ifndef WEBSOCKETS_MAX_DATA_SIZE\n"
                "#define WEBSOCKETS_MAX_DATA_SIZE (15 * 1024)\n"
                "#endif",
            )
        ],
        "WEBSOCKETS_MAX_DATA_SIZE overridable",
    )

    # ESP32 WiFiClientSecure::connected() is unreliable on idle TLS sockets.
    patch_file(
        os.path.join(base, "WebSocketsClient.cpp"),
        [
            (
                """bool WebSocketsClient::clientIsConnected(WSclient_t * client) {
    if(!client->tcp) {
        return false;
    }

    if(client->tcp->connected()) {
        if(client->status != WSC_NOT_CONNECTED) {
            return true;
        }
    } else {
        // client lost
        if(client->status != WSC_NOT_CONNECTED) {
            DEBUG_WEBSOCKETS("[WS-Client] connection lost.\\n");
            // do cleanup
            clientDisconnect(client, "Connection lost");
        }
    }

    if(client->tcp) {
        // do cleanup
        clientDisconnect(client, "TCP connection cleanup");
    }

    return false;
}""",
                """bool WebSocketsClient::clientIsConnected(WSclient_t * client) {
    if(!client->tcp) {
        return false;
    }

    // ESP32 TLS often reports disconnected() while the session is still valid.
    // Stay up until read/ping fails; heartbeat handles dead peers.
    if(client->status == WSC_CONNECTED) {
        return true;
    }

    if(client->tcp->connected()) {
        if(client->status != WSC_NOT_CONNECTED) {
            return true;
        }
    } else if(client->status != WSC_NOT_CONNECTED) {
        DEBUG_WEBSOCKETS("[WS-Client] connection lost.\\n");
        clientDisconnect(client, "Connection lost");
    }

    if(client->tcp) {
        clientDisconnect(client, "TCP connection cleanup");
    }

    return false;
}""",
            )
        ],
        "ESP32 idle TLS connected() fix",
    )

    patch_file(
        os.path.join(base, "WebSockets.cpp"),
        [
            (
                """        if(!client->tcp->connected()) {
            DEBUG_WEBSOCKETS("[readCb] not connected!\\n");
            if(cb) {
                cb(client, false);
            }
            return false;
        }""",
                """        if(client->status != WSC_CONNECTED && !client->tcp->connected()) {
            DEBUG_WEBSOCKETS("[readCb] not connected!\\n");
            if(cb) {
                cb(client, false);
            }
            return false;
        }""",
            )
        ],
        "ESP32 TLS readCb connected() fix",
    )

    patch_file(
        os.path.join(base, "WebSockets.cpp"),
        [
            (
                """    if(header->payloadLen > WEBSOCKETS_MAX_DATA_SIZE) {
        DEBUG_WEBSOCKETS("[WS][%d][handleWebsocket] payload too big! (%u)\\n", client->num, header->payloadLen);
        clientDisconnect(client, 1009);
        return;
    }""",
                """    if(header->payloadLen > WEBSOCKETS_MAX_DATA_SIZE) {
        Serial.printf("[WS] frame too large %uB (max %u)\\n",
                      static_cast<unsigned>(header->payloadLen),
                      static_cast<unsigned>(WEBSOCKETS_MAX_DATA_SIZE));
        DEBUG_WEBSOCKETS("[WS][%d][handleWebsocket] payload too big! (%u)\\n", client->num, header->payloadLen);
        clientDisconnect(client, 1009);
        return;
    }""",
            ),
            (
                """        if(!payload) {
            DEBUG_WEBSOCKETS("[WS][%d][handleWebsocket] to less memory to handle payload %d!\\n", client->num, header->payloadLen);
            clientDisconnect(client, 1011);
            return;
        }""",
                """        if(!payload) {
            Serial.printf("[WS] frame malloc failed len=%u heap=%u\\n",
                          static_cast<unsigned>(header->payloadLen),
                          static_cast<unsigned>(GET_FREE_HEAP));
            DEBUG_WEBSOCKETS("[WS][%d][handleWebsocket] to less memory to handle payload %d!\\n", client->num, header->payloadLen);
            clientDisconnect(client, 1011);
            return;
        }""",
            ),
        ],
        "WS oversize/malloc serial logs",
    )


patch_websockets()
