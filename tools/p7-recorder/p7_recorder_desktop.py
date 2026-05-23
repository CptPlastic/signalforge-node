#!/usr/bin/env python3
"""Small desktop wrapper for the P7 Recorder Agent."""

from __future__ import annotations

import os
import json
import re
import sys
import webbrowser
from pathlib import Path

import p7_recorder
import p7_recorder_build_info as build_info
import sounddevice as sd
from PySide6.QtCore import QProcess, Qt, QUrl
from PySide6.QtGui import QIcon
from PySide6.QtNetwork import QNetworkAccessManager, QNetworkRequest
from PySide6.QtWidgets import (
    QApplication,
    QComboBox,
    QFormLayout,
    QGridLayout,
    QGroupBox,
    QHBoxLayout,
    QLabel,
    QLineEdit,
    QMainWindow,
    QMessageBox,
    QPushButton,
    QSpinBox,
    QTextEdit,
    QVBoxLayout,
    QWidget,
)

APP_NAME = "P7 Recorder"


def bundled_path(name: str) -> Path:
    return Path(getattr(sys, "_MEIPASS", Path(__file__).parent)) / name


def app_data_dir() -> Path:
    base = os.environ.get("LOCALAPPDATA")
    if base:
        return Path(base) / APP_NAME
    return Path.home() / ".p7-recorder"


def default_config_path() -> Path:
    return app_data_dir() / "config.toml"


def recorder_script_path() -> Path:
    return Path(__file__).with_name("p7_recorder.py")


def default_server_url() -> str:
    value = getattr(build_info, "DEFAULT_SERVER_URL", "").strip()
    return value or "https://p7hub.projectseven.us/"


def toml_string(value: str) -> str:
    return json.dumps(value.strip())


def normalize_version(value: str) -> tuple[int, ...]:
    cleaned = value.strip().lower().removeprefix("v")
    parts = re.findall(r"\d+", cleaned)
    return tuple(int(part) for part in parts[:3])


def is_newer_version(latest: str, current: str) -> bool:
    latest_parts = normalize_version(latest)
    current_parts = normalize_version(current)
    if not latest_parts or not current_parts:
        return False
    max_len = max(len(latest_parts), len(current_parts))
    return latest_parts + (0,) * (max_len - len(latest_parts)) > current_parts + (0,) * (max_len - len(current_parts))


class RecorderWindow(QMainWindow):
    def __init__(self) -> None:
        super().__init__()
        self.setWindowTitle(APP_NAME)
        icon_path = bundled_path("signalforge-icon.svg")
        if icon_path.exists():
            self.setWindowIcon(QIcon(str(icon_path)))
        self.process: QProcess | None = None
        self.devices: list[tuple[int, str]] = []

        self.server_url = QLineEdit(default_server_url())
        self.source_key = QLineEdit()
        self.source_key.setEchoMode(QLineEdit.EchoMode.Password)
        self.device = QComboBox()
        self.refresh_devices_button = QPushButton("REFRESH")
        self.threshold = QSpinBox()
        self.threshold.setRange(1, 32000)
        self.threshold.setValue(500)
        self.silence_ms = QSpinBox()
        self.silence_ms.setRange(100, 10000)
        self.silence_ms.setValue(1200)
        self.talkgroup = QSpinBox()
        self.talkgroup.setRange(1, 999999)
        self.talkgroup.setValue(18)
        self.frequency = QSpinBox()
        self.frequency.setRange(0, 999999999)
        self.frequency.setValue(462625000)
        self.system_label = QLineEdit("GMRS")
        self.talkgroup_label = QLineEdit("GMRS Channel 18")
        self.talkgroup_group = QLineEdit("GMRS")
        self.start_button = QPushButton("START")
        self.stop_button = QPushButton("STOP")
        self.stop_button.setEnabled(False)
        self.save_button = QPushButton("SAVE CONFIG")
        self.version_label = QLabel(f"VERSION {build_info.APP_VERSION}")
        self.update_button = QPushButton("CHECK UPDATE")
        self.downloads_button = QPushButton("OPEN DOWNLOADS")
        self.log = QTextEdit()
        self.log.setReadOnly(True)
        self.network = QNetworkAccessManager(self)

        self.build_ui()
        self.refresh_devices()
        self.connect_signals()
        self.check_for_update(silent=True)

    def build_ui(self) -> None:
        root = QWidget()
        layout = QVBoxLayout(root)

        p7_box = QGroupBox("P7 CONNECTION")
        p7_form = QFormLayout(p7_box)
        p7_form.addRow("Server URL", self.server_url)
        p7_form.addRow("Source key", self.source_key)
        layout.addWidget(p7_box)

        audio_box = QGroupBox("AUDIO")
        audio_grid = QGridLayout(audio_box)
        audio_grid.addWidget(QLabel("Input device"), 0, 0)
        audio_grid.addWidget(self.device, 0, 1)
        audio_grid.addWidget(self.refresh_devices_button, 0, 2)
        audio_grid.addWidget(QLabel("Threshold"), 1, 0)
        audio_grid.addWidget(self.threshold, 1, 1)
        audio_grid.addWidget(QLabel("Silence ms"), 2, 0)
        audio_grid.addWidget(self.silence_ms, 2, 1)
        layout.addWidget(audio_box)

        meta_box = QGroupBox("CALL METADATA")
        meta_form = QFormLayout(meta_box)
        meta_form.addRow("System", self.system_label)
        meta_form.addRow("Channel ID", self.talkgroup)
        meta_form.addRow("Channel label", self.talkgroup_label)
        meta_form.addRow("Group", self.talkgroup_group)
        meta_form.addRow("Frequency Hz", self.frequency)
        layout.addWidget(meta_box)

        buttons = QHBoxLayout()
        buttons.addWidget(self.start_button)
        buttons.addWidget(self.stop_button)
        buttons.addStretch(1)
        buttons.addWidget(self.version_label)
        buttons.addWidget(self.update_button)
        buttons.addWidget(self.downloads_button)
        buttons.addWidget(self.save_button)
        layout.addLayout(buttons)
        layout.addWidget(self.log)

        self.setCentralWidget(root)
        self.resize(720, 640)
        self.apply_style()

    def apply_style(self) -> None:
        self.setStyleSheet("""
            QMainWindow, QWidget { background: #050705; color: #d8ffe1; font-family: Consolas, monospace; font-size: 12px; }
            QGroupBox { border: 1px solid #155f2c; margin-top: 12px; padding: 12px 8px 8px; color: #00ff41; font-weight: 700; }
            QGroupBox::title { subcontrol-origin: margin; left: 8px; padding: 0 4px; }
            QLabel { color: #9debb0; }
            QLineEdit, QComboBox, QSpinBox, QTextEdit { background: #0b100c; color: #eaffee; border: 1px solid #1d7a39; padding: 6px; selection-background-color: #00ff41; selection-color: #050705; }
            QPushButton { background: #0d1b10; color: #f4fff6; border: 1px solid #00ff41; padding: 7px 12px; font-weight: 700; }
            QPushButton:hover { background: #12391d; }
            QPushButton:disabled { color: #53705a; border-color: #234429; background: #080d09; }
            QTextEdit { min-height: 160px; }
        """)

    def connect_signals(self) -> None:
        self.refresh_devices_button.clicked.connect(self.refresh_devices)
        self.save_button.clicked.connect(self.save_config)
        self.start_button.clicked.connect(self.start_recorder)
        self.stop_button.clicked.connect(self.stop_recorder)
        self.update_button.clicked.connect(lambda: self.check_for_update(silent=False))
        self.downloads_button.clicked.connect(self.open_downloads)
        self.network.finished.connect(self.update_check_finished)

    def open_downloads(self) -> None:
        page_url = getattr(build_info, "RELEASES_PAGE_URL", "").strip() or "https://signalforge.org/#recorder"
        webbrowser.open(page_url)

    def check_for_update(self, silent: bool) -> None:
        if not build_info.RELEASES_API_URL:
            self.version_label.setText(f"VERSION {build_info.APP_VERSION}")
            if not silent:
                self.append_log("update check unavailable for this build")
            return

        self.update_button.setEnabled(False)
        self.update_button.setText("CHECKING")
        request = QNetworkRequest(QUrl(build_info.RELEASES_API_URL))
        request.setHeader(QNetworkRequest.KnownHeaders.UserAgentHeader, "P7 Recorder")
        reply = self.network.get(request)
        reply.setProperty("silent", silent)

    def update_check_finished(self, reply) -> None:  # type: ignore[no-untyped-def]
        silent = bool(reply.property("silent"))
        self.update_button.setEnabled(True)
        self.update_button.setText("CHECK UPDATE")

        try:
            if reply.error():
                if not silent:
                    self.append_log(f"update check failed: {reply.errorString()}")
                return

            payload = json.loads(bytes(reply.readAll()).decode("utf-8"))
            latest = str(payload.get("tag_name", "")).strip()
            current = build_info.APP_VERSION
            if is_newer_version(latest, current):
                self.version_label.setText(f"VERSION {current} / UPDATE {latest}")
                self.downloads_button.setText("OPEN UPDATE")
                self.append_log(f"update available: {latest}")
            else:
                self.version_label.setText(f"VERSION {current} / CURRENT")
                self.downloads_button.setText("OPEN DOWNLOADS")
                if not silent:
                    self.append_log("no update available")
        except Exception as exc:
            if not silent:
                self.append_log(f"update check failed: {exc}")
        finally:
            reply.deleteLater()

    def refresh_devices(self) -> None:
        self.device.clear()
        self.devices = []
        try:
            for index, info in enumerate(sd.query_devices()):
                if int(info.get("max_input_channels", 0)) <= 0:
                    continue
                label = f"{index}: {info['name']}"
                self.devices.append((index, label))
                self.device.addItem(label, index)
        except Exception as exc:
            self.append_log(f"device scan failed: {exc}")
        if not self.devices:
            self.device.addItem("No input devices found", None)

    def append_log(self, text: str) -> None:
        self.log.append(text.rstrip())
        self.log.verticalScrollBar().setValue(self.log.verticalScrollBar().maximum())

    def config_text(self) -> str:
        device_index = self.device.currentData()
        device_line = "" if device_index is None else f"device = {int(device_index)}\n"
        return f'''[p7]
base_url = {toml_string(self.server_url.text())}
source_key = {toml_string(self.source_key.text())}
timeout_sec = 20

[audio]
{device_line}sample_rate = 16000
channels = 1
block_ms = 100
threshold = {self.threshold.value()}
silence_ms = {self.silence_ms.value()}
min_duration_ms = 400
max_duration_sec = 120
pre_roll_ms = 300

[metadata]
system = 1
system_label = {toml_string(self.system_label.text())}
talkgroup = {self.talkgroup.value()}
talkgroup_label = {toml_string(self.talkgroup_label.text())}
talkgroup_group = {toml_string(self.talkgroup_group.text())}
talkgroup_tag = "voice"
frequency = {self.frequency.value()}

[queue]
directory = "queue"
'''

    def save_config(self) -> Path:
        config_path = default_config_path()
        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(self.config_text(), encoding="utf-8")
        self.append_log(f"saved {config_path}")
        return config_path

    def start_recorder(self) -> None:
        if self.process is not None:
            return
        if not self.source_key.text().strip():
            QMessageBox.warning(self, APP_NAME, "Set a source key before starting.")
            return
        config_path = self.save_config()
        if getattr(sys, "frozen", False):
            program = sys.executable
            arguments = ["--recorder-worker", "--config", str(config_path)]
        else:
            script_path = recorder_script_path()
            if not script_path.exists():
                QMessageBox.critical(self, APP_NAME, f"Recorder script not found: {script_path}")
                return
            program = sys.executable
            arguments = [str(script_path), "--config", str(config_path)]

        self.process = QProcess(self)
        self.process.setProgram(program)
        self.process.setArguments(arguments)
        self.process.setWorkingDirectory(str(config_path.parent))
        self.process.readyReadStandardOutput.connect(self.read_stdout)
        self.process.readyReadStandardError.connect(self.read_stderr)
        self.process.finished.connect(self.process_finished)
        self.process.start()
        self.start_button.setEnabled(False)
        self.stop_button.setEnabled(True)
        self.append_log("recorder starting")

    def stop_recorder(self) -> None:
        if self.process is None:
            return
        self.append_log("recorder stopping")
        self.process.terminate()
        if not self.process.waitForFinished(3000):
            self.process.kill()

    def read_stdout(self) -> None:
        if self.process is None:
            return
        text = bytes(self.process.readAllStandardOutput()).decode(errors="replace")
        if text:
            self.append_log(text)

    def read_stderr(self) -> None:
        if self.process is None:
            return
        text = bytes(self.process.readAllStandardError()).decode(errors="replace")
        if text:
            self.append_log(text)

    def process_finished(self) -> None:
        self.append_log("recorder stopped")
        self.process = None
        self.start_button.setEnabled(True)
        self.stop_button.setEnabled(False)

    def closeEvent(self, event) -> None:  # type: ignore[no-untyped-def]
        self.stop_recorder()
        event.accept()


def main() -> int:
    if len(sys.argv) > 1 and sys.argv[1] == "--print-build-info":
        payload = json.dumps({
            "app_version": build_info.APP_VERSION,
            "releases_api_url": build_info.RELEASES_API_URL,
            "releases_page_url": build_info.RELEASES_PAGE_URL,
            "default_server_url": default_server_url(),
        }, sort_keys=True)
        if len(sys.argv) > 2:
            Path(sys.argv[2]).write_text(payload + "\n", encoding="utf-8")
        else:
            print(payload)
        return 0

    if len(sys.argv) > 1 and sys.argv[1] == "--recorder-worker":
        sys.argv = ["p7_recorder", *sys.argv[2:]]
        return p7_recorder.main()

    app = QApplication(sys.argv)
    app.setApplicationName(APP_NAME)
    icon_path = bundled_path("signalforge-icon.svg")
    if icon_path.exists():
        app.setWindowIcon(QIcon(str(icon_path)))
    window = RecorderWindow()
    window.show()
    return app.exec()


if __name__ == "__main__":
    raise SystemExit(main())
