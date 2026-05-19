#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

version="${P7_RECORDER_GO_VERSION:-dev}"
mkdir -p dist
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=${version}" -o dist/p7-recorder-go ./cmd/p7-recorder-go
cp config.example.toml dist/config.example.toml
cp README.md dist/README.md
echo "Built dist/p7-recorder-go"