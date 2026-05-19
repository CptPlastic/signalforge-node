#!/usr/bin/env python3
"""P7 local audio recorder agent.

Records voice-activated audio from a local input device, stores each call as a
WAV file, and uploads queued clips to a P7 Scanner /api/call-upload endpoint.
"""

from __future__ import annotations

import argparse
import array
import json
import queue
import signal
import sys
import time
import tomllib
import wave
from collections import deque
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urljoin

import requests
import sounddevice as sd


@dataclass(frozen=True)
class P7Config:
    base_url: str
    source_key: str
    timeout_sec: float


@dataclass(frozen=True)
class AudioConfig:
    device: int | str | None
    sample_rate: int
    channels: int
    block_ms: int
    threshold: int
    silence_ms: int
    min_duration_ms: int
    max_duration_sec: int
    pre_roll_ms: int


@dataclass(frozen=True)
class MetadataConfig:
    system: int
    system_label: str
    talkgroup: int
    talkgroup_label: str
    talkgroup_group: str
    talkgroup_tag: str
    frequency: int


@dataclass(frozen=True)
class QueueConfig:
    directory: Path


@dataclass(frozen=True)
class RecorderConfig:
    p7: P7Config
    audio: AudioConfig
    metadata: MetadataConfig
    queue: QueueConfig


def load_config(path: Path) -> RecorderConfig:
    with path.open("rb") as handle:
        raw = tomllib.load(handle)

    p7 = raw.get("p7", {})
    audio = raw.get("audio", {})
    metadata = raw.get("metadata", {})
    queue_cfg = raw.get("queue", {})

    base_url = str(p7.get("base_url", "")).strip()
    source_key = str(p7.get("source_key", "")).strip()
    if not base_url:
        raise ValueError("p7.base_url is required")
    if not source_key or source_key.startswith("sk_live_REPLACE"):
        raise ValueError("p7.source_key must be set to a generated source API key")

    queue_dir = Path(str(queue_cfg.get("directory", "queue"))).expanduser()
    if not queue_dir.is_absolute():
        queue_dir = path.parent / queue_dir

    return RecorderConfig(
        p7=P7Config(
            base_url=base_url.rstrip("/") + "/",
            source_key=source_key,
            timeout_sec=float(p7.get("timeout_sec", 20)),
        ),
        audio=AudioConfig(
            device=audio.get("device"),
            sample_rate=int(audio.get("sample_rate", 16000)),
            channels=int(audio.get("channels", 1)),
            block_ms=int(audio.get("block_ms", 100)),
            threshold=int(audio.get("threshold", 500)),
            silence_ms=int(audio.get("silence_ms", 1200)),
            min_duration_ms=int(audio.get("min_duration_ms", 400)),
            max_duration_sec=int(audio.get("max_duration_sec", 120)),
            pre_roll_ms=int(audio.get("pre_roll_ms", 300)),
        ),
        metadata=MetadataConfig(
            system=int(metadata.get("system", 1)),
            system_label=str(metadata.get("system_label", "GMRS")),
            talkgroup=int(metadata.get("talkgroup", 1)),
            talkgroup_label=str(metadata.get("talkgroup_label", "GMRS Channel")),
            talkgroup_group=str(metadata.get("talkgroup_group", "GMRS")),
            talkgroup_tag=str(metadata.get("talkgroup_tag", "voice")),
            frequency=int(metadata.get("frequency", 0)),
        ),
        queue=QueueConfig(directory=queue_dir),
    )


def list_devices() -> None:
    print(sd.query_devices())


def rms_int16(block: bytes) -> int:
    samples = array.array("h")
    samples.frombytes(block)
    if sys.byteorder != "little":
        samples.byteswap()
    if not samples:
        return 0
    total = sum(sample * sample for sample in samples)
    return int((total / len(samples)) ** 0.5)


def write_wav(path: Path, blocks: list[bytes], sample_rate: int, channels: int) -> None:
    with wave.open(str(path), "wb") as handle:
        handle.setnchannels(channels)
        handle.setsampwidth(2)
        handle.setframerate(sample_rate)
        handle.writeframes(b"".join(blocks))


def metadata_fields(config: RecorderConfig, started_at: int, duration_sec: float, audio_name: str) -> dict[str, str]:
    meta = config.metadata
    return {
        "key": config.p7.source_key,
        "system": str(meta.system),
        "systemLabel": meta.system_label,
        "talkgroup": str(meta.talkgroup),
        "talkgroupLabel": meta.talkgroup_label,
        "talkgroupGroup": meta.talkgroup_group,
        "talkgroupTag": meta.talkgroup_tag,
        "frequency": str(meta.frequency),
        "dateTime": str(started_at),
        "duration": f"{duration_sec:.3f}",
        "audioName": audio_name,
        "audioType": "audio/wav",
    }


def queue_call(config: RecorderConfig, blocks: list[bytes], started_at: int) -> Path:
    config.queue.directory.mkdir(parents=True, exist_ok=True)
    duration_sec = len(b"".join(blocks)) / (config.audio.sample_rate * config.audio.channels * 2)
    stem = f"call-{started_at}-{int(time.time() * 1000)}"
    wav_path = config.queue.directory / f"{stem}.wav"
    json_path = config.queue.directory / f"{stem}.json"
    write_wav(wav_path, blocks, config.audio.sample_rate, config.audio.channels)
    fields = metadata_fields(config, started_at, duration_sec, wav_path.name)
    json_path.write_text(json.dumps(fields, indent=2), encoding="utf-8")
    return wav_path


def upload_one(config: RecorderConfig, json_path: Path) -> bool:
    wav_path = json_path.with_suffix(".wav")
    if not wav_path.exists():
        json_path.unlink(missing_ok=True)
        return True

    fields = json.loads(json_path.read_text(encoding="utf-8"))
    url = urljoin(config.p7.base_url, "api/call-upload")
    with wav_path.open("rb") as audio_file:
        response = requests.post(
            url,
            data=fields,
            files={"audio": (wav_path.name, audio_file, "audio/wav")},
            timeout=config.p7.timeout_sec,
        )
    if response.status_code < 200 or response.status_code >= 300:
        print(f"upload failed: {response.status_code} {response.text.strip()}")
        return False

    wav_path.unlink(missing_ok=True)
    json_path.unlink(missing_ok=True)
    print(f"uploaded {wav_path.name}")
    return True


def flush_queue(config: RecorderConfig) -> None:
    config.queue.directory.mkdir(parents=True, exist_ok=True)
    for json_path in sorted(config.queue.directory.glob("*.json")):
        try:
            if not upload_one(config, json_path):
                return
        except requests.RequestException as exc:
            print(f"upload unavailable: {exc}")
            return
        except OSError as exc:
            print(f"queue error: {exc}")
            return


class Recorder:
    def __init__(self, config: RecorderConfig) -> None:
        self.config = config
        self.blocks: queue.Queue[bytes] = queue.Queue(maxsize=200)
        self.running = True

    def stop(self, *_args: Any) -> None:
        self.running = False

    def audio_callback(self, indata: bytes, _frames: int, _time_info: Any, status: sd.CallbackFlags) -> None:
        if status:
            print(f"audio status: {status}")
        try:
            self.blocks.put_nowait(bytes(indata))
        except queue.Full:
            print("audio queue full; dropping block")

    def run(self) -> None:
        audio = self.config.audio
        block_frames = max(1, int(audio.sample_rate * audio.block_ms / 1000))
        silence_blocks_required = max(1, int(audio.silence_ms / audio.block_ms))
        min_blocks = max(1, int(audio.min_duration_ms / audio.block_ms))
        max_blocks = max(1, int(audio.max_duration_sec * 1000 / audio.block_ms))
        pre_roll_blocks = max(0, int(audio.pre_roll_ms / audio.block_ms))
        pre_roll: deque[bytes] = deque(maxlen=pre_roll_blocks)
        active_blocks: list[bytes] = []
        active = False
        silent_blocks = 0
        started_at = 0

        flush_queue(self.config)
        print("P7 recorder running. Press Ctrl+C to stop.")
        with sd.RawInputStream(
            samplerate=audio.sample_rate,
            blocksize=block_frames,
            channels=audio.channels,
            dtype="int16",
            device=audio.device,
            callback=self.audio_callback,
        ):
            while self.running:
                try:
                    block = self.blocks.get(timeout=0.25)
                except queue.Empty:
                    continue

                level = rms_int16(block)
                voice = level >= audio.threshold
                if not active:
                    pre_roll.append(block)
                    if voice:
                        active = True
                        silent_blocks = 0
                        started_at = int(time.time())
                        active_blocks = list(pre_roll)
                        print(f"voice start rms={level}")
                    continue

                active_blocks.append(block)
                if voice:
                    silent_blocks = 0
                else:
                    silent_blocks += 1

                too_quiet = silent_blocks >= silence_blocks_required
                too_long = len(active_blocks) >= max_blocks
                if too_quiet or too_long:
                    if len(active_blocks) >= min_blocks:
                        path = queue_call(self.config, active_blocks, started_at)
                        print(f"queued {path.name}")
                        flush_queue(self.config)
                    else:
                        print("discarded short burst")
                    active = False
                    active_blocks = []
                    pre_roll.clear()
                    silent_blocks = 0

        if active_blocks and len(active_blocks) >= min_blocks:
            path = queue_call(self.config, active_blocks, started_at or int(time.time()))
            print(f"queued {path.name}")
            flush_queue(self.config)


def main() -> int:
    parser = argparse.ArgumentParser(description="Record local radio audio and upload calls to P7 Scanner.")
    parser.add_argument("--config", default="config.toml", help="path to recorder config TOML")
    parser.add_argument("--list-devices", action="store_true", help="list audio devices and exit")
    args = parser.parse_args()

    if args.list_devices:
        list_devices()
        return 0

    try:
        config = load_config(Path(args.config).expanduser())
        recorder = Recorder(config)
        signal.signal(signal.SIGINT, recorder.stop)
        signal.signal(signal.SIGTERM, recorder.stop)
        recorder.run()
    except KeyboardInterrupt:
        return 0
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
