# Architecture

## Overview

| Layer | Stack | Purpose |
| --- | --- | --- |
| Client | React 18, Vite, TypeScript, Tailwind | Console-style real-time UI |
| Server | Go 1.22, chi, PostgreSQL | API, scanner orchestration, transport |

## Display modes

Hub console and the **public radio-set player** (`/public/player/{token}`) support **DARK / NITE / NVG / LIGHT** operator palettes (SchedKit-style tactical cycle). See [DISPLAY-MODES.md](https://signalforge.org/DISPLAY-MODES.md). Both surfaces persist `localStorage.sf-display-mode` and apply CSS vars on `html[data-sf-display-mode]` (Tailwind `console.*` in the console; inline tokens in the embeddable player).

## Design tokens

- `bg`: `#0a0a0a`
- `surface`: `#111111`
- `border`: `#1f1f1f`
- `text`: `#c9c9c9`
- `muted`: `#555555`
- `accent`: `#ffaa00` (hub PWA — warmer than mobile `#ffc700`; see [BRAND.md](https://signalforge.org/BRAND.md))
- `amber`: `#ffc700`
- `error`: `#ff4444`

## Package layout

- `server/internal/config`: environment configuration and validation
- `server/internal/api`: router, middleware, and HTTP handlers
- `server/internal/scanner`: scanner domain placeholder for Phase 2
- `client/src/lib`: typed API and WebSocket clients
- `client/src/components`: reusable UI components (Phase 3)

## Phase tracker

- [x] Phase 1 scaffold
- [ ] Phase 2 scanner integration
- [ ] Phase 3 feature UI modules
- [ ] Phase 4 hardening and observability
