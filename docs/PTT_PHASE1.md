# PTT (Push-to-Talk) — Phase 1 Design

**Status:** design / not yet implemented
**Scope:** single-hub PTT. Cross-hub federation explicitly deferred to Phase 2.

## Goal

Let authenticated users on a hub press-and-hold a button in the SignalForge client (web, mobile) to record a short audio message that is delivered to other users currently listening to the same Radio Set, alongside the real radio traffic on that set.

In other words: a Zello-style team-chat channel that lives inside a Radio Set, mixed with the real RX talkgroups.

## Non-goals

- **No RF transmission.** PTT does not key any radio. It's purely IP-to-IP via the hub.
- **No cross-hub federation.** Phase 1 PTT is scoped to users on the same hub. Federation is Phase 2.
- **No live full-duplex.** PTT is half-duplex, record-then-deliver. Users wait their turn like on a real radio.
- **No public-share TX.** Public share URLs remain read-only. Only authenticated hub users with TX permission can transmit.

## Why "record-then-deliver" instead of streaming

The single biggest design constraint is that PTT users are on mobile phones with spotty connectivity (in cars, buildings, etc.). Streaming audio over a live WebSocket while the user talks fails badly when the connection drops mid-sentence — listeners get a partial, choppy transmission and the speaker has no idea anything was lost.

Recording the full utterance locally first, then uploading as a single HTTP request, gives us:

- **All-or-nothing semantics.** A transmission either arrives complete or doesn't. No partial garbage.
- **Standard HTTP retry.** Lost connection → retry the POST with exponential backoff. Same model as any other mobile app upload.
- **Persistable queue.** If the app is killed before upload completes, the file sits on disk and gets resent on next launch.
- **Network-agnostic.** Works the same on LTE, 5G, WiFi handoff, captive portal, anything.

Trade-off: there is a 0.5–2 second delay between the user releasing the button and other listeners hearing the audio (record finalization + upload + fan-out). This matches Zello / Voxer / other PTT-over-IP apps and is well within what users expect from PTT.

## Data model changes

Two new concepts plus a couple of flags on existing tables.

### `users` (existing) — add column

```sql
ALTER TABLE users ADD COLUMN tx_enabled BOOLEAN NOT NULL DEFAULT FALSE;
```

A hub admin enables `tx_enabled` per user. Without it, the PTT button is hidden / disabled in the client.

### `talkgroups` (existing) — add column

```sql
ALTER TABLE talkgroups ADD COLUMN origin TEXT NOT NULL DEFAULT 'rf';
-- origin ∈ ('rf', 'ptt')
```

A `'ptt'` talkgroup is a virtual TG that exists only inside the hub — no recorder will ever produce calls for it. Numerically these get IDs in a reserved range (e.g. `9_000_000+`) so they can't collide with real-radio TG numbers.

### `radio_sets` (existing) — add column

```sql
ALTER TABLE radio_sets ADD COLUMN ptt_talkgroup_id BIGINT NULL
  REFERENCES talkgroups(id);
```

When a Radio Set is created (or upgraded), the server auto-creates one virtual PTT talkgroup tied to it and stores its ID here. The Radio Set's `talkgroups` array continues to be the list of *real* TGs the set listens to; the PTT TG is implicitly included for any subscriber.

This means a Radio Set has:
- N real RF talkgroups (what scanners RX)
- 1 virtual PTT talkgroup (where humans on this set chat)

### `calls` (existing) — add column

```sql
ALTER TABLE calls ADD COLUMN origin TEXT NOT NULL DEFAULT 'rf';
ALTER TABLE calls ADD COLUMN sender_user_id BIGINT NULL REFERENCES users(id);
```

`origin = 'ptt'` calls have a `sender_user_id` populated; `origin = 'rf'` calls don't.

## API

### `POST /api/v1/radio-sets/{id}/ptt`

Authenticated. Requires `Authorization: Bearer <user-session-token>` and the authenticated user must have `tx_enabled = true`.

Multipart body:
- `audio`: the recorded clip, m4a/aac preferred (matches what iOS AVAudioRecorder produces by default)
- `duration`: float seconds (client-measured)
- `client_id`: opaque string the client generated for idempotency (so a retry doesn't produce a duplicate Call)

Server behavior:
1. Validate user `tx_enabled`. Reject 403 if not.
2. Validate the requested radio set exists and the user has access to it.
3. Look up the set's `ptt_talkgroup_id`. If null, 400 (set wasn't migrated).
4. Check `client_id` against a recent-uploads table; if seen, 200 with the existing Call ID (idempotent).
5. Insert a Call row with `origin='ptt'`, `talkgroup=<ptt_tg>`, `sender_user_id=<user>`, store the audio.
6. Enqueue for transcription (same worker, same path).
7. Push to the stream hub. Every WebSocket subscriber of that set's TGs gets it instantly.
8. Return `{ call_id, talkgroup, duration }`.

### `GET /api/v1/radio-sets/{id}/ptt/queue` (optional, post-MVP)

Returns calls the client previously uploaded with this `client_id` namespace in the last hour, so a client can reconcile its outbox after reinstall.

### Admin: `PATCH /api/v1/users/{id}` extended

Add `tx_enabled` to the writable fields for hub admins.

## Client UX — Mobile (signalforge-mobile)

### PTT button placement

On the Now Playing screen, when:
1. The user is authenticated to the hub (not a public-share session), and
2. The user has `tx_enabled = true`, and
3. The active Radio Set has a `ptt_talkgroup_id`

…show a large hold-to-talk button below the playback controls.

### Recording flow

1. **Touch down** on PTT button:
   - Vibrate (haptic) briefly
   - Pause the playback queue (so the user hears themselves, not someone else, while talking — and so the recorded audio doesn't pick up the speaker's voice through the phone speaker)
   - Show a level meter driven by the mic input
   - Start AVAudioRecorder (iOS) / MediaRecorder (Android) to a local temp file in `caches/ptt/<uuid>.m4a`
   - Show "RECORDING" indicator, elapsed timer
2. **Touch up** or **drag-off**:
   - Stop recording
   - Generate a `client_id` UUID
   - Hand the file to the upload queue (see below)
   - Resume playback queue
   - Show "QUEUED" or "SENT" toast depending on result

### Upload queue (the spotty-connection answer)

A persistent FIFO that survives app restarts.

- New uploads are appended with status `pending`
- A single uploader task processes the queue one item at a time
- Each item: `POST /api/v1/radio-sets/{id}/ptt` with the audio file
- On 2xx: mark `delivered`, remove file from disk, persist
- On 4xx (non-retryable, e.g. 403 disabled): mark `failed`, surface to user, retain file 24h then delete
- On 5xx / network error / timeout: backoff (1s, 2s, 4s, 8s, 16s, 30s, then every 30s) up to 1 hour, then mark `stalled` and pause
- Connectivity events (NetInfo): kick the queue when the device comes back online

UI:
- A small "outbox" badge on the Now Playing screen showing pending count
- Tap → outbox list view: each entry shows talkgroup, duration, timestamp, status (▲ uploading / ⌛ pending / ✓ delivered / ✕ failed)
- User can long-press a failed/stalled item to retry or delete

### When in airplane mode / no signal at touch-down

The PTT button still works. Recording happens locally. On release, the item goes to the queue with status `pending` and uploads when connectivity returns. User sees the outbox counter increment.

## Client UX — Web (p7-scanner/client)

Same as mobile but with browser realities:

- Use `MediaRecorder` API with `audio/webm;codecs=opus` (browser native) or fall back to `audio/mp4` where supported. Server should accept both.
- Spacebar can also act as PTT key (push-and-hold)
- No persistent disk queue across page reloads in v1 — failed uploads are surfaced in a toast and the user can retry from a recent-transmissions panel until they reload
- Phase 1.5: persist queue via IndexedDB if the browser-side outbox proves useful

## Transcription

PTT calls go through the same transcriber worker. Two small tweaks:

- The `INITIAL_PROMPT` is tuned for radio dispatch jargon, which works fine for PTT chatter too
- We may want to track transcription separately so PTT calls can be filtered/searched independently — the existing `origin` column on calls handles this

## Auth + permissions matrix

| Caller | Action | Result |
|---|---|---|
| Logged-in user, `tx_enabled=true`, has set access | PTT POST | 200 + delivered |
| Logged-in user, `tx_enabled=false` | PTT POST | 403 |
| Logged-in user, no access to that radio set | PTT POST | 403 |
| Public share token | PTT POST | 401 (route doesn't accept share tokens at all) |
| Anonymous | PTT POST | 401 |

## What this gets you, concretely

A Radio Set displaying:

```
TG  30221  PD DISP 1       0.8 MHz  RX  "advise location"
TG  30245  FD ALPHA        ...      RX  "engine 4 responding"
TG  9000001 [PTT] team      —       PTT "yeah I saw that too, dispatch"
TG  9000001 [PTT] team      —       PTT "copy, heading there now"
TG  30221  PD DISP 1       0.8 MHz  RX  "10-4 standby"
```

Real radio calls and team chat interleave in the same timeline, same player, same transcript. That's the killer feature.

## Build order (rough estimate)

1. Schema migrations + virtual-TG auto-create on radio set create. *~0.5 day*
2. `POST /ptt` endpoint with idempotency. *~1 day*
3. Hook the stream hub fan-out + transcriber. *~0.5 day*
4. Admin UI to toggle `tx_enabled` per user. *~0.5 day*
5. Mobile PTT button + recording + outbox queue. *~2 days*
6. Web PTT button + spacebar binding. *~1 day*
7. End-to-end testing on spotty network. *~1 day*

**Total: roughly 6–7 days of focused work for Phase 1.**

## Phase 2 (federation) — explicitly out of scope here

Cross-hub PTT participation requires inter-hub identity, trust, and routing. Sketched but not designed in detail:

- A "publisher hub" hosts a federated TG
- "Subscriber hubs" are explicitly paired (mutual API key handshake) and can include the federated TG in their radio sets
- TX from a subscriber routes upstream to the publisher, fans out to all subscribers
- Star topology only in Phase 2; mesh is Phase 3+

This warrants its own design doc when the time comes.

## Phase 1.5 — Bluetooth PTT handsets

Hardware PTT buttons (Bluetooth headsets like the Sena 50R, AINA PTT, FreedomMic, etc.) are a natural extension because they trigger the *exact same code path* as the on-screen button — they just generate the press/release events from hardware instead of touch. The upload pipeline (record locally → POST → outbox) doesn't change at all.

Two platform-specific integrations are needed:

**iOS — Apple's PushToTalk framework (iOS 15+).** This is the canonical way to build PTT apps on iOS. Highlights:
- Apple-provided system PTT UI (banner, control center, lock screen)
- Hardware-button events delivered to the app even from the background
- Background mic capture as a first-class capability (no separate background-audio hack needed)
- Works with both built-in remote-control buttons and BT headset PTT buttons
- Requires the `com.apple.developer.push-to-talk` entitlement (Apple-gated like CarPlay, but PTT is in a different review queue)

**Android — MediaSession + InCall PTT.** Android exposes PTT button events to the app through `MediaSession.Callback`. For BT headsets that emit standard HID button codes (most do), the MediaSessionService that react-native-track-player already registers can intercept them. Custom-protocol headsets (some Sena models) require vendor SDKs.

Both flow into the same `POST /api/v1/radio-sets/{id}/ptt` endpoint, so no server-side changes are needed for Phase 1.5.

## Open questions for Phase 1

- **Max PTT duration cap?** Suggest 30s hard limit, with a soft warning at 20s. Anything longer should be a voice note feature, not PTT.
- **Should PTT audio be redactable like RX audio?** Probably yes; the existing redaction flow can apply.
- **Notification when someone PTTs on a set you're not currently listening to?** Out of scope for Phase 1.
- **Per-talkgroup TX permission** vs. global `tx_enabled`? Phase 1 = global. If abuse appears, add per-set TX granularity in Phase 1.5.
