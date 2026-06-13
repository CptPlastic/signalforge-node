# SignalForge Hub API

SignalForge Hub exposes one API per hub. The browser console, recorder clients, public players, and trusted peer hubs all talk to the same HTTP surface.

This document is the operator API guide. It is intentionally compact: use it to understand what can be automated, which endpoints are public, and which routes require an authenticated admin session.

## Base URL

Use the public hub URL as the base URL:

```text
https://p7hub.projectseven.us
```

API routes are under `/api/v1` unless noted. Public player and recorder ingest routes are intentionally outside the authenticated console API.

## Authentication

Console API authentication uses the same magic-link session cookie as the web UI.

- Public: health, version, update check, recorder ingest, public player routes.
- Authenticated user: call browsing, radio sets, talkgroup preferences, source visibility.
- Admin: users, audit logs, source management, hub identity, federation peers, directory refresh.

Source upload uses a source API key submitted as multipart form field `key` to `POST /api/call-upload`.

## Public System Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/health` | Basic hub health. |
| `GET` | `/api/v1/version` | Server build/version metadata. |
| `GET` | `/api/v1/update-check` | Current deployment versus public image manifest. |

Example:

```bash
curl https://p7hub.projectseven.us/api/v1/health
```

## Auth Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/v1/auth/magic-link` | Request a login token for an email address. |
| `GET` | `/api/v1/auth/verify?token=...` | Verify a magic-link token and create a session cookie. |
| `POST` | `/api/v1/auth/logout` | Revoke the current session. |
| `GET` | `/api/v1/auth/me` | Return current session/user state. |

## Hub Identity And Directory

Admin session required unless noted.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/hub/identity` | Read local hub identity. Requires any authenticated user. |
| `PUT` | `/api/v1/hub/identity` | Update name, public URL, region, contact, and federation toggle. |
| `POST` | `/api/v1/hub/identity/keypair` | Generate the local Ed25519 hub keypair if missing. Returns only the public key. |
| `POST` | `/api/v1/hub/directory/refresh` | Fetch the configured public directory feed and update local trust fields. |
| `GET` | `/api/v1/hub/federation/status` | Admin status view for identity, trust, peer pull health, shared sources, and imports. |

The hub private key is stored locally and is never returned by API responses.

## Federation Peers

Admin session required.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/hub/invites` | List active/recent hub invites. |
| `POST` | `/api/v1/hub/invites` | Create a peer invite token. |
| `POST` | `/api/v1/hub/invites/accept` | Accept a remote invite. Used by peer hubs. |
| `DELETE` | `/api/v1/hub/invites/{id}` | Revoke an invite. |
| `GET` | `/api/v1/hub/peers` | List known peer hubs. |
| `POST` | `/api/v1/hub/peers` | Connect to a remote hub with its URL and invite token. |
| `PATCH` | `/api/v1/hub/peers/{id}/enable` | Re-enable a disabled peer. |
| `DELETE` | `/api/v1/hub/peers/{id}` | Remove a peer and imported calls/sources from that peer. |

## Federation Pull API

These routes are read by peer hubs. Remote hubs send `X-SignalHub-Peer-ID` so the serving hub can label the peer request.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/federation/sources` | List exportable shared local sources. |
| `GET` | `/api/v1/federation/calls?since=0&limit=100` | Pull exportable calls newer than `since` (live incremental sync). |
| `GET` | `/api/v1/federation/calls?recent=1&limit=100` | Pull the newest exportable calls (historical backfill). Optional `before=<callId>` pages older than that ID. |

Imported remote calls are readable locally but are not exported again.

## Calls

Authenticated session required.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/calls` | List calls with pagination, search, talkgroup, group, source, and radio-set filters. |
| `GET` | `/api/v1/calls/{id}/audio` | Download/play call audio if readable by the current user. |
| `GET` | `/api/v1/calls/groups` | List distinct talkgroup groups. |
| `GET` | `/api/v1/talkgroups/distinct` | List distinct talkgroups for radio-set editing. |
| `GET` | `/api/v1/talkgroups/settings` | List talkgroup preferences. |
| `PUT` | `/api/v1/talkgroups/{talkgroup}/settings` | Save favorite/muted settings. |
| `DELETE` | `/api/v1/talkgroups/{talkgroup}` | Delete local talkgroup settings. |

Common `/api/v1/calls` query parameters:

| Parameter | Meaning |
| --- | --- |
| `limit` | Page size. |
| `offset` | Page offset. |
| `q` | Search text. |
| `talkgroups` | Comma-separated talkgroup IDs. |
| `group` | Talkgroup group substring. |
| `radioSetId` | Filter by saved radio set. |
| `sourceId` | Filter by source. |
| `sort` | Field such as `datetime`, `duration`, `talkgroup`, or `frequency`. |
| `order` | `asc` or `desc`. |

## Call Storage And Retention (Admin)

Call audio is stored in Postgres (`BYTEA`). On busy hubs this grows quickly and can fill the database volume. Configure archival to export old calls to disk and remove them from the database.

Environment:

| Variable | Meaning |
| --- | --- |
| `CALL_ARCHIVE_DIR` | Host path for exported call files (mount a spacious volume here). |
| `CALL_RETENTION_DAYS` | When set with `CALL_ARCHIVE_DIR`, the hub auto-archives calls older than this many days every 6 hours. `0` disables the scheduler (manual API still works). |
| `CALL_ARCHIVE_S3_URI` | Optional `s3://space-name/prefix` destination (DigitalOcean Spaces or other S3-compatible store). |
| `CALL_ARCHIVE_S3CFG` | Path to the [s3cmd](https://docs.digitalocean.com/products/spaces/reference/s3cmd/) config file inside the api container (default `/etc/signalforge/s3cfg`). |
| `CALL_ARCHIVE_DELETE_LOCAL_AFTER_S3` | When `true`, remove local day folders after a successful `s3cmd sync`. |
| `CALL_ARCHIVE_VACUUM_FULL` | When `true` (default), run `VACUUM FULL` on `calls` after the last batch of a retention sweep finishes (`remainingOld=0`). Intermediate batches run `VACUUM ANALYZE` only. |
| `SPACES_ACCESS_KEY` / `SPACES_SECRET_KEY` / `SPACES_ENDPOINT` | Optional — api entrypoint writes `CALL_ARCHIVE_S3CFG` at start (e.g. `nyc3.digitaloceanspaces.com`). |

Archive layout:

```text
/data/call-archive/
  2026-06-01/
    call-12345.json
    call-12345.mp3
```

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/admin/calls/storage` | Call count, total audio bytes, oldest/newest call timestamps, retention config. |
| `POST` | `/api/v1/admin/calls/archive` | Export then delete calls older than N days. Runs in batches of up to 500 until the backlog is cleared (`untilEmpty`, default `true`). |

Archive request body:

```json
{
  "olderThanDays": 30,
  "dryRun": true,
  "limit": 500,
  "untilEmpty": true
}
```

`dryRun: true` reports how many calls and bytes would be freed without writing or deleting anything.

`untilEmpty: true` (default for real runs) keeps batching until `remainingOld` is 0 or a 4-hour safety limit is hit. Set `untilEmpty: false` for a single batch only.

After large deletes, the hub vacuums the `calls` table automatically. `VACUUM FULL` (returning disk to the OS) runs when a retention sweep finishes; set `CALL_ARCHIVE_VACUUM_FULL=false` to skip the full shrink and rely on `VACUUM ANALYZE` only.

Example Spaces setup:

```bash
# In stack env (entrypoint generates /etc/signalforge/s3cfg):
SPACES_ACCESS_KEY=your-key
SPACES_SECRET_KEY=your-secret
SPACES_ENDPOINT=nyc3.digitaloceanspaces.com
CALL_ARCHIVE_S3_URI=s3://your-space/signalforge-hub/call-archive
CALL_ARCHIVE_DELETE_LOCAL_AFTER_S3=true
```

## Radio Sets And Public Player

Authenticated session required for radio-set management. Public player routes are unauthenticated when a share token exists.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/radio-sets` | List user radio sets. |
| `POST` | `/api/v1/radio-sets` | Create a radio set. |
| `GET` | `/api/v1/radio-sets/{id}` | Read one radio set. |
| `PUT` | `/api/v1/radio-sets/{id}` | Update name/talkgroups. |
| `DELETE` | `/api/v1/radio-sets/{id}` | Delete a radio set. |
| `POST` | `/api/v1/radio-sets/{id}/share` | Generate a public player share token. |
| `DELETE` | `/api/v1/radio-sets/{id}/share` | Revoke a public player share token. |
| `GET` | `/public/player/{token}` | Public embeddable player page. |
| `GET` | `/public/ws/{token}` | Public WebSocket carrying call metadata and base64 audio atomically. Query: `seed=0` skips recent-call seeding; `format=mp3` transcodes non-MP3 clips to `audio/mpeg` (for embedded players). |
| `GET` | `/public/last-call/{token}` | Most recent call audio for a public share token. |

## Sources And Source Keys

Authenticated session required. Admins can manage all sources; regular users can manage their own sources.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/sources` | List ingestion sources visible to the user. |
| `PUT` | `/api/v1/sources` | Create or update a source profile. |
| `DELETE` | `/api/v1/sources/{id}` | Soft-delete a source. |
| `GET` | `/api/v1/sources/{id}/shares` | List users a source is shared with. |
| `PUT` | `/api/v1/sources/{id}/shares` | Update source sharing. |
| `POST` | `/api/v1/sources/{id}/keys` | Generate a source upload API key. |
| `GET` | `/api/v1/sources/{id}/keys` | List source API keys. |
| `DELETE` | `/api/v1/sources/{id}/keys/{keyId}` | Revoke a source API key. |

## Recorder Upload API

Public route authenticated by source API key.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/call-upload` | Rdio Scanner-compatible call upload endpoint. |

Required multipart fields:

| Field | Meaning |
| --- | --- |
| `key` | Source API key generated in the hub console. |
| `talkgroup` | Talkgroup number. |
| `audio` | Audio file part. |

Common optional fields: `system`, `systemLabel`, `talkgroupLabel`, `talkgroupGroup`, `talkgroupTag`, `frequency`, `duration`, `audioName`, `audioType`.

Connectivity probe:

```bash
curl -F key=sk_live_REPLACE_WITH_SOURCE_KEY -F test=1 https://p7hub.projectseven.us/api/call-upload
```

A valid key returns HTTP `417` with `incomplete call data: no talkgroup`. That is the expected SDRTrunk-style probe response and means the key was accepted.

The Go recorder exposes the same check:

```bash
p7-recorder-go --config config.toml --check-hub
```

## Admin Operations

Admin session required.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/users` | List users. |
| `PATCH` | `/api/v1/users/{id}` | Update user role/status. |
| `DELETE` | `/api/v1/users/{id}` | Delete a user. |
| `GET` | `/api/v1/audit-logs?limit=100` | List audit events. |

## WebSocket Console Feed

Authenticated session required.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/ws` | Console live call/status WebSocket. |

Use the browser client unless you are building a custom operator console.