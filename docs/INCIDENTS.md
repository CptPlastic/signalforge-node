# Hub incidents — design spec

Incident-scoped radio sets, optional SchedKit sync, and integration rooms (Discord voice, public player, mobile).

**Status:** design — not implemented  
**Last updated:** 2026-06-20

---

## Opt-in incident management (handler hubs)

Not every hub runs incidents. Community cells can stay listen-only. **Incident management is opt-in** and tied to federation.

### Roles

| Role | Hub | Capability |
|------|-----|------------|
| **Incident handler** | Official / trusted root hub (e.g. `p7hub.projectseven.us`) | Define regional templates, monitor external alerts, open incidents, **push incident directives** to opted-in peers |
| **Participant hub** | Community / verified cell | Opt in → receive handler directives, create local incident radio sets, optional local dispatch |
| **Standalone hub** | No handler peer | Full local incident CRUD if admin enables `incidentManagementEnabled` locally (no upstream push) |

### Participant requirements (A + B)

To receive handler-driven incidents or use federated incident templates:

1. **A — Connected to handler hub**  
   - SignalHub peer status `connected` to an hub with `trustLevel` ≥ `verified` (or admin-approved handler list)  
   - Handler hub ID stored on participant: `incidentHandlerHubId`

2. **B — Incident management enabled**  
   - Hub flag: `incidentManagementEnabled=true` (admin toggle in HUB tab)  
   - Handler may require approval before first activation (directory + manual ack)

```text
canUseIncidentManagement(hub) =
  hub.incidentManagementEnabled == true
  AND (
    hub.incidentHandlerHubId == ""           → standalone mode OK
    OR peerConnected(hub.incidentHandlerHubId)
  )

canReceiveHandlerIncidents(hub) =
  hub.incidentManagementEnabled == true
  AND peerConnected(hub.incidentHandlerHubId)
  AND handlerTrustLevel >= verified
```

### Handler → participant flow

1. Handler detects signal (NWS alert, storm report, operator open, AI suggest confirmed).
2. Handler opens **network incident** with template + geo scope (county, WFO zone, lat/lon radius).
3. Handler `POST /api/v1/federation/incidents/push` to opted-in peers (new federation message type).
4. Participant hub creates **local incident** + radio set (may adjust TGs for local sources).
5. Optional: Discord VC on handler Discord *or* participant's Discord via integration webhook.

Standalone hubs skip step 3–4 push — they only see local incidents they create themselves.

### Hub identity fields (new)

| Field | Notes |
|-------|-------|
| `incident_management_enabled` | bool, default false |
| `incident_handler_hub_id` | nullable — official handler peer |
| `incident_auto_suggest` | bool — external alerts → draft only |
| `incident_auto_open` | bool — opt-in auto-open on `urgent` NWS warnings (handler only by default) |
| `incident_watch_zones` | json — NWS zone/county codes, e.g. `["OKC143", "OKZ004"]` for Canadian County |

---

## External signal sources (weather / Skywarn)

**Skywarn** is not a single public API — it's spotters + NWS. For automation, stack **authoritative free feeds** first, then **hub-native signals**, then **news** last.

### Tier 1 — Use these (free, reliable)

**NWS Weather API** — watches, warnings, advisories (CAP/JSON-LD). No API key.

```text
# All active alerts for Oklahoma
https://api.weather.gov/alerts/active?area=OK

# Alerts for a point (Yukon, OK approx)
https://api.weather.gov/alerts/active?point=35.5067,-97.7625

# Resolve zones for a point (then watch zone codes)
https://api.weather.gov/points/35.5067,-97.7625
```

- Docs: [weather.gov/documentation/services-web-api](https://www.weather.gov/documentation/services-web-api)
- Rate limit: be polite (~1 req / 30s per zone set); set `User-Agent` with contact email (NWS asks for this)
- Map event → template: `Tornado Warning` → `weather-severe`, `Severe Thunderstorm Warning` → `weather-severe`, etc.

**Iowa Environmental Mesonet (IEM) — Local Storm Reports**  
Real-time LSRs (spotter / law enforcement / public reports parsed from NWS):

```text
# Last 24h, Oklahoma, GeoJSON
https://mesonet.agron.iastate.edu/cgi-bin/request/gis/lsr.py?state=OK&hours=24&fmt=geojson

# Bounding box around Yukon
https://mesonet.agron.iastate.edu/cgi-bin/request/gis/lsr.py?north=35.6&south=35.4&west=-97.9&east=-97.6&hours=12&fmt=geojson
```

- Includes tornado, hail, wind, flooding reports — closest thing to a **Skywarn feed** without paid APIs
- IEM also has SPC outlooks and WPC products if you want broader severe weather context

**NOAA MapServer — LSR layer** (GeoJSON, ~30 min refresh):

```text
https://mapservices.weather.noaa.gov/vector/rest/services/obs/nws_local_storm_reports/MapServer/0/query?where=1%3D1&outFields=*&f=geojson
```

### Tier 2 — Hub-native (you already have this)

| Signal | Source |
|--------|--------|
| Call volume spike | Hub call log by talkgroup group |
| Keyword hits | Transcription worker (`tornado`, `structure fire`, `gas leak`, …) |
| Multi-TG correlation | Fire + EMS + Command active together |
| PTT dispatch surge | `origin: ptt-dispatch` rate |

These are **local** in a way no news API is — they reflect what's actually on the radio in Yukon.

### Tier 3 — Local news (weak / optional)

There is **no good free "Yukon OK news incidents" API**. Options:

| Source | Reality |
|--------|---------|
| Local TV RSS (KFOR, News 9, KOCO) | Free but unstructured; keyword filter (`tornado`, `Yukon`, `closed`) — brittle |
| Google News / Bing News API | Mostly deprecated or paid |
| Broadcastify / RadioReference | Scanner metadata, not news; paid tiers |
| X/Twitter fire accounts | API expensive; unofficial |
| Citizen app / PulsePoint | Limited public APIs |

**Recommendation:** Don't depend on local news for v1. Use **NWS + IEM LSR + hub transcripts**. Add RSS keyword watch as optional `incident_signal_rss_urls` per hub if operators want KOCO/News9 headlines as **suggest-only** input.

### Signal → action pipeline

```text
External poll (every 60–120s)
  → NWS active alerts for hub.incident_watch_zones
  → IEM LSR in bounding box (new since last cursor)
  → optional RSS keyword match

Hub internal (real-time)
  → transcript keywords
  → TG spike detector

All signals → incident_signals table (idempotent by source+external_id)
  → severity scored
  → if auto_suggest: create draft + notify dispatchers
  → if auto_open (handler only, urgent NWS): open incident + push to peers
  → else: post to Discord #ops + hub notification
```

Example NWS events that should **suggest** or **auto-open** (handler policy):

| NWS event | Suggest | Auto-open (opt-in) |
|-----------|---------|-------------------|
| Tornado Warning | yes | yes |
| Severe Thunderstorm Warning | yes | no |
| Tornado Watch | yes | no |
| Flash Flood Warning | yes | no |
| Winter Storm Warning | yes | no |

---

## Goals

- During severe weather, multi-agency events, or local emergencies, operators can open an **incident** that **auto-builds a radio set** from a template (talkgroups / talkgroup groups).
- Each incident gets its own **listening surface** — hub console, public player, Discord voice, mobile — without manual TG picking under stress.
- **Hub works standalone.** SchedKit sync is optional for orgs that use it (e.g. projectseven); most community hub operators will not have SchedKit.
- **Separate archive** from normal call retention — incident history survives call DB pruning.

---

## Decisions (locked)

| # | Question | Decision |
|---|----------|----------|
| 1 | Where does the incident live? | **Hub is source of truth.** Optional **outbound sync** to SchedKit when configured. Inbound webhook from SchedKit can create/update hub incidents when both exist. |
| 2 | Who can open/manage incidents? | **`admin` role** or users with **`dispatcher_enabled`** (hub flags today). Guests and normal users: listen only if exposure allows. |
| 3 | Exposure (see below) | **Default: hub members.** Per-incident toggle for wider public / Discord VC access. |
| 4 | Archives | **Separate from call retention.** Closing an incident archives incident metadata + radio set config + integration links; does not rely on call archive S3 paths. |
| 5 | Who runs incident management? | **Opt-in per hub.** Participant hubs need **handler peer connected** + **`incidentManagementEnabled`**. Handler hub (official) monitors NWS/IEM and can push network incidents. |

---

## Exposure — what “public vs private” means

When an incident is open, who is allowed to **listen**?

| Mode | Hub console | Public player link | Discord voice | Typical use |
|------|-------------|-------------------|---------------|-------------|
| **`members`** (default) | Logged-in hub users | Off / revoked | Discord role-gated VC (Operator+) | Structure fire, sensitive LE |
| **`community`** | Logged-in users | Share link works | Public VC or `#live` style channel | Severe weather, Skywarn, public interest |
| **`internal`** | Admin + dispatchers only | Off | No Discord bridge | Test / staging |

**Share token:** Only generated when exposure is `community` (or explicitly enabled). Revoked automatically when incident closes or downgraded to `members`.

**Discord:** Bot only joins voice and plays audio when integration is enabled **and** exposure allows it. `members` incidents use a private VC; `community` incidents can use a public listen channel.

Operators choose exposure at create time; admins can change it while open.

---

## Permissions

```text
canManageIncidents(user) =
  user.role == "admin" OR user.dispatcher_enabled == true

canListenIncident(user, incident) =
  incident.status != "draft" AND (
    incident.exposure == "community"  → any authenticated hub user (or public if share token)
    incident.exposure == "members"    → authenticated hub user
    incident.exposure == "internal"   → canManageIncidents(user)
  )
```

Discord bot actions (create VC, `/listen`) require a hub-issued **integration token** tied to the incident — not raw share tokens in chat.

---

## Hub data model

### `incidents`

| Column | Type | Notes |
|--------|------|-------|
| `id` | text | `inc_…` |
| `title` | text | e.g. `Severe Weather — Yukon` |
| `type` | text | `weather`, `fire`, `ems`, `eoc`, `custom`, … |
| `status` | text | `draft` → `active` → `monitoring` → `closed` → `archived` |
| `priority` | text | `low`, `normal`, `high`, `urgent` |
| `exposure` | text | `internal`, `members`, `community` |
| `radio_set_id` | text | FK → `radio_sets.id` |
| `template_id` | text | optional — which template was used |
| `opened_by_user_id` | text | |
| `opened_at` | int | unix |
| `closed_at` | int | nullable |
| `archived_at` | int | nullable |
| `notes` | text | operator summary |
| `schedkit_incident_id` | text | nullable — remote ID if synced |
| `metadata` | jsonb | NWS alert id, geo, tags, AI suggestions |

### `incident_templates`

Hub-local templates (admin-managed):

```json
{
  "id": "weather-severe",
  "name": "Severe Weather",
  "selectionMode": "groups",
  "talkgroupGroups": ["Fire", "EMS", "Public Works", "Skywarn"],
  "defaultExposure": "community",
  "defaultPriority": "high"
}
```

### `incident_integrations`

| Column | Type | Notes |
|--------|------|-------|
| `id` | text | |
| `incident_id` | text | |
| `kind` | text | `discord_voice`, `discord_text`, `public_player`, `webhook` |
| `config` | jsonb | channel IDs, share token ref, webhook URL |
| `status` | text | `pending`, `active`, `stopped`, `archived` |

### Radio set linkage

- Creating an incident **creates a dedicated radio set** (or clones template → new set).
- Set name: `INC-{shortId} · {title}`.
- Set is tagged in metadata: `{ "incidentId": "inc_…" }` (new optional column or JSON on `radio_sets`).
- On **close**: set stays read-only; share token revoked; PTT optionally disabled.
- On **archive**: set hidden from default UI; still queryable in incident archive view.

---

## SchedKit sync (optional)

Most hubs: **no SchedKit** — ignore this section.

When `SCHEDKIT_URL` + `SCHEDKIT_API_KEY` (or portal secret) configured on hub:

| Event | Direction | Action |
|-------|-----------|--------|
| Hub incident `active` | Hub → SchedKit | `POST /v1/incidents` (or tickets) with title, priority, hub link |
| Hub incident updated | Hub → SchedKit | PATCH status, notes |
| Hub incident closed | Hub → SchedKit | Resolve/close remote ticket |
| SchedKit incident created (webhook) | SchedKit → Hub | Create draft incident; dispatcher confirms radio scope |

Hub remains authoritative for **radio scope** and **audio**. SchedKit remains authoritative for **SLA / customer status / war room** when present.

Sync fields on hub incident: `schedkit_incident_id`, `schedkit_sync_status`, `schedkit_last_sync_at`.

---

## Incident archive (separate from call retention)

**Call retention** (`CALL_RETENTION_DAYS`, S3 archive): prunes **call rows and audio** from Postgres.

**Incident archive**: keeps **operational record** after close:

- Incident title, type, timeline (opened / closed / archived)
- Template used + talkgroup snapshot at open time
- Operator notes and status changes
- Integration history (Discord channel IDs, whether public link was issued)
- Optional: transcript **excerpts** (not full audio dump) — pointers to call IDs if still in DB
- Link to call archive prefix if audio was exported under `incidents/{incident_id}/` (future)

Retention policy (hub config, separate from call retention):

```text
INCIDENT_ARCHIVE_DAYS=365   # default 1 year; set 0 to disable
```

When enabled, a background job runs every 6 hours and **deletes** closed/archived
incidents older than the retention window, including:

- Incident row + Discord integration rows (CASCADE)
- Linked dedicated radio set
- Signal → incident links (cleared, signals kept)

Active / monitoring / draft incidents are never purged.
Archived incidents appear in **Hub → Incidents → Ended** until retention removes them.

---

## API sketch (hub)

All routes require session auth unless noted.

```text
GET    /api/v1/incidents                    list (active + optional archived)
POST   /api/v1/incidents                    create (admin | dispatcher)
GET    /api/v1/incidents/{id}               detail + radio set + integrations
PATCH  /api/v1/incidents/{id}               update status, exposure, notes
POST   /api/v1/incidents/{id}/close         close + revoke share + stop integrations
POST   /api/v1/incidents/{id}/archive       move to archive store

GET    /api/v1/incident-templates           list templates
POST   /api/v1/incident-templates           admin — create template

POST   /api/v1/incidents/{id}/integrations/discord   request VC + bot listen
DELETE /api/v1/incidents/{id}/integrations/discord   tear down

POST   /api/v1/webhooks/schedkit            optional inbound sync (secret)
```

Create body example:

```json
{
  "title": "Severe Weather — Yukon",
  "type": "weather",
  "templateId": "weather-severe",
  "priority": "high",
  "exposure": "community",
  "notes": "NWS tornado watch until 9pm"
}
```

Response includes `radioSet`, `shareUrl` (if community), `publicPlayerUrl`.

---

## Discord integration flow

1. Dispatcher opens incident (exposure `community` or `members`).
2. Hub creates radio set + share token (if community).
3. Optional: `POST …/integrations/discord` → bot receives webhook from hub:
   - Create category `// ACTIVE INCIDENTS` if missing
   - Create `// INC-042 severe-weather` (voice) + `#inc-042-ops` (text)
   - Bot joins voice, streams from `wss://hub/public/ws/{token}?format=mp3`
4. On close: bot leaves, channels moved to `// ARCHIVE` or deleted per policy.

Bot never stores long-lived share tokens in Discord messages — only hub integration service holds the mapping.

---

## AI assist (later phases)

Not required for v1. Planned signals — see **External signal sources** above.

v1: **manual open only** on participant hubs. Handler hub may **suggest** from NWS/IEM; dispatcher confirms.

Phase 2: handler `incident_auto_suggest=true` (draft incidents from NWS + LSR + transcript keywords).

Phase 3: handler `incident_auto_open=true` for `Tornado Warning`-class events only, with peer push.

---

## UI (hub console)

New nav area or section under **Radio Sets**:

- **Active incidents** — cards with status, exposure badge, Listen, Open player, Discord status
- **Open incident** wizard — template picker → title → exposure → confirm
- **Archive** — closed/archived incidents, read-only detail

Dispatcher view: quick-open from template + link to PTT broadcast for incident set.

---

## Implementation phases

| Phase | Deliverable |
|-------|-------------|
| **1** | DB + API + templates + manual create/close/archive + opt-in flags |
| **2** | Hub UI wizard + handler peer settings + incident-linked radio sets |
| **3** | NWS + IEM poller on handler hub → draft suggest |
| **4** | Discord bot: dynamic VC + voice bridge |
| **5** | Federation incident push to opted-in peers |
| **6** | SchedKit optional sync (projectseven) |
| **7** | Transcript/spike AI + optional RSS news keywords |

---

## Open questions (remaining)

- Auto-delete archived Discord channels after N days, or keep forever?
- Federation: share incident radio scope to peer hubs via SignalHub?
- Mobile: dedicated “Incidents” tab with one-tap monitor?

---

## Related docs

- [TRANSCRIPTION.md](./TRANSCRIPTION.md) — keyword assist input
- [SIGNALHUB_MVP.md](./SIGNALHUB_MVP.md) — peer trust, official hub
- [PTT_PHASE1.md](./PTT_PHASE1.md) — dispatcher broadcast
- SchedKit: `docs/concepts/incidents.mdx`
- Discord: `signalforge.org/discord/SERVER.md`
