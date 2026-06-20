#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GOBIN="$(go env GOPATH)/bin"

cd "$ROOT"
go install ./cmd/signalforge
ln -sf "$GOBIN/signalforge" "$GOBIN/sf"

echo "Installed: $GOBIN/sf"
"$GOBIN/sf" version
