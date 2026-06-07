#!/usr/bin/env python3
"""Log live calls from a hub public share WebSocket (same path as the handheld).

No ESP32 required. Useful while hardware is on order or unwired.

  pip install websockets
  python3 scripts/listen_probe.py --host p7hub.projectseven.us --token YOUR_SHARE_TOKEN

Reads defaults from ../include/hub_config.h when flags are omitted.
"""

from __future__ import annotations

import argparse
import asyncio
import base64
import json
import re
import ssl
import sys
from pathlib import Path

DEFAULT_CONFIG = Path(__file__).resolve().parent.parent / "include" / "hub_config.h"


def read_config_value(name: str) -> str | None:
    if not DEFAULT_CONFIG.exists():
        return None
    text = DEFAULT_CONFIG.read_text(encoding="utf-8")
    match = re.search(rf'#define\s+{name}\s+"([^"]*)"', text)
    if not match:
        return None
    value = match.group(1).strip()
    if not value or "paste_" in value or value == "YourNetwork":
        return None
    return value


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="SignalForge public listen log probe")
    parser.add_argument("--host", default=read_config_value("HUB_HOST"))
    parser.add_argument("--port", type=int, default=443)
    parser.add_argument("--token", default=read_config_value("HUB_SHARE_TOKEN"))
    parser.add_argument("--insecure", action="store_true", default=True,
                        help="Skip TLS cert verification (default: on)")
    parser.add_argument("--secure", action="store_true", help="Verify TLS certificates")
    return parser.parse_args()


def format_call(msg: dict) -> str:
    if msg.get("cmd") != "call":
        return json.dumps(msg)[:200]
    tg = msg.get("talkgroupLabel") or msg.get("talkgroup")
    system = msg.get("systemLabel") or "-"
    origin = msg.get("origin") or "rf"
    sender = msg.get("senderEmail") or ""
    audio = msg.get("audio")
    audio_len = 0
    if isinstance(audio, str):
        try:
            audio_len = len(base64.b64decode(audio, validate=False))
        except Exception:
            audio_len = len(audio)
    elif isinstance(audio, (bytes, bytearray)):
        audio_len = len(audio)
    dur = msg.get("duration")
    parts = [f"call#{msg.get('id')}", f"tg={tg}", f"sys={system}", f"origin={origin}"]
    if sender:
        parts.append(f"from={sender}")
    if dur is not None:
        parts.append(f"{dur}s")
    parts.append(f"audio={audio_len}B")
    return " | ".join(parts)


async def run_probe(host: str, port: int, token: str, insecure: bool) -> None:
    try:
        import websockets
    except ImportError:
        print("Install: pip install websockets", file=sys.stderr)
        raise SystemExit(1) from None

    url = f"wss://{host}:{port}/public/ws/{token}"
    ctx = ssl.create_default_context()
    if insecure:
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE

    print(f"connecting {url}")
    print("waiting for calls (Ctrl+C to stop)...")
    async with websockets.connect(url, ssl=ctx, open_timeout=20) as ws:
        print("connected")
        async for raw in ws:
            try:
                msg = json.loads(raw)
            except json.JSONDecodeError:
                print(f"non-json frame ({len(raw)} bytes)")
                continue
            print(format_call(msg))


def main() -> None:
    args = parse_args()
    insecure = args.insecure and not args.secure
    if not args.host or not args.token:
        print("Set --host and --token, or fill HUB_HOST / HUB_SHARE_TOKEN in include/hub_config.h",
              file=sys.stderr)
        raise SystemExit(2)
    try:
        asyncio.run(run_probe(args.host, args.port, args.token, insecure))
    except KeyboardInterrupt:
        print("\nstopped")


if __name__ == "__main__":
    main()
