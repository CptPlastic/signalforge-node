#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

build_info_backup="$(mktemp)"
cp p7_recorder_build_info.py "$build_info_backup"
restore_build_info() {
    cp "$build_info_backup" p7_recorder_build_info.py
    rm -f "$build_info_backup"
}
trap restore_build_info EXIT

python3 - <<'PY'
import json
import os
from pathlib import Path

version = os.environ.get("P7_RECORDER_VERSION") or "dev"
repository = os.environ.get("P7_RECORDER_REPOSITORY") or ""
default_server_url = os.environ.get("P7_RECORDER_DEFAULT_SERVER_URL") or "https://p7hub.projectseven.us/"
api_url = f"https://api.github.com/repos/{repository}/releases/latest" if repository else ""
page_url = f"https://github.com/{repository}/releases/latest" if repository else ""

Path("p7_recorder_build_info.py").write_text(
    f"APP_VERSION = {json.dumps(version)}\n"
    f"RELEASES_API_URL = {json.dumps(api_url)}\n"
    f"RELEASES_PAGE_URL = {json.dumps(page_url)}\n"
    f"DEFAULT_SERVER_URL = {json.dumps(default_server_url)}\n",
    encoding="utf-8",
)
PY

if [ ! -d .venv ]; then
    python3 -m venv .venv
fi

.venv/bin/python -m pip install --upgrade pip
.venv/bin/python -m pip install -r requirements.txt
.venv/bin/python -m PyInstaller \
    --noconfirm \
    --clean \
    --windowed \
    --name "P7 Recorder" \
    --add-data "signalforge-icon.svg:." \
    p7_recorder_desktop.py

codesign --force --deep --sign - "dist/P7 Recorder.app"

echo "Built dist/P7 Recorder.app"
