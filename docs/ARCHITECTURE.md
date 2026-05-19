# Architecture

## Overview

| Layer | Stack | Purpose |
| --- | --- | --- |
| Client | React 18, Vite, TypeScript, Tailwind | Console-style real-time UI |
| Server | Go 1.22, chi, PostgreSQL | API, scanner orchestration, transport |

## Design tokens

- `bg`: `#0a0a0a`
- `surface`: `#111111`
- `border`: `#1f1f1f`
- `text`: `#c9c9c9`
- `muted`: `#555555`
- `accent`: `#00ff41`
- `amber`: `#ffb000`
- `error`: `#ff4444`

## Package layout

- `server/internal/config`: environment configuration and validation
- `server/internal/api`: router, middleware, and HTTP handlers
- `server/internal/scanner`: scanner domain placeholder for Phase 2
- `client/src/lib`: typed API and WebSocket clients
- `client/src/components`: reusable UI components (Phase 3)
