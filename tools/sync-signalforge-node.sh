#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <source_repo_path> <target_repo_path>" >&2
  exit 1
fi

SOURCE_REPO="$1"
TARGET_REPO="$2"

if [[ ! -d "$SOURCE_REPO" ]]; then
  echo "Source repo does not exist: $SOURCE_REPO" >&2
  exit 1
fi

if [[ ! -d "$TARGET_REPO" ]]; then
  echo "Target repo does not exist: $TARGET_REPO" >&2
  exit 1
fi

sync_dir() {
  local rel_path="$1"
  rsync -a --delete \
    "$SOURCE_REPO/$rel_path/" \
    "$TARGET_REPO/$rel_path/"
}

sync_file() {
  local rel_path="$1"
  rsync -a "$SOURCE_REPO/$rel_path" "$TARGET_REPO/$rel_path"
}

# Mirror runtime code and assets that should stay aligned.
sync_dir "client"
sync_dir "server"
sync_dir "docker"
sync_dir "docs"
sync_dir "tools"

# Mirror selected root files used for build/deploy/runtime.
sync_file ".env.peer.example"
sync_file ".env.production.example"
sync_file ".gitattributes"
sync_file ".gitignore"
sync_file "AUTH_GATING.md"
sync_file "Dockerfile.server"
sync_file "Makefile"
sync_file "docker-compose.yml"
sync_file "docker-compose.peer.yml"
sync_file "docker-compose.plesk.yml"
sync_file "docker-compose.prod.yml"
