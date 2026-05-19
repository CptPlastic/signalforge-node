# SignalHub MVP

SignalHub is the planned peer-to-peer pub/sub federation layer for P7 Scanner hubs.

## Repository Model

- `p7-scanner` remains the root source of truth for core development.
- SignalForge is the public downstream surface for docs, releases, operator onboarding, and verified hub discovery.
- Downstream SignalForge builds should be generated from the root P7 Scanner codebase, not maintained as a separate fork of the core logic.

## MVP Goals

1. Let each P7 Scanner instance identify itself as a hub.
2. Let admins generate peer invite tokens.
3. Let admins subscribe to trusted peer hubs by URL and token.
4. Import remote sources with clear `REMOTE` origin labeling.
5. Preserve origin hub metadata on every shared call.
6. Prevent remote-of-remote resharing by default.
7. Allow optional SignalForge directory validation for known-good hubs.

## MVP Test Path

The first SignalHub implementation should be testable with normal Docker Compose. Most operators should be able to run a hub without knowing Go, Node, or the internal build chain.

### Local Developer Loop

Run one local hub with the existing compose file:

```bash
docker-compose up --build -d
```

For hub-to-hub testing, run two local stacks with separate project names and ports:

```bash
COMPOSE_PROJECT_NAME=p7hub_a CLIENT_PORT=3000 SERVER_PORT=8080 POSTGRES_PORT=5432 docker-compose up --build -d
COMPOSE_PROJECT_NAME=p7hub_b CLIENT_PORT=3001 SERVER_PORT=8081 POSTGRES_PORT=5433 docker-compose up --build -d
```

Each stack gets its own Docker project name and named volume, so identity, peers, sources, and calls stay separate.

Expected local MVP flow:

1. Start Hub A and Hub B locally.
2. Create an admin account on each hub.
3. Configure hub identity on both hubs.
4. Generate a peer invite on Hub A.
5. Add Hub A as a peer on Hub B.
6. Mark one Hub A source as shareable.
7. Verify Hub B can see Hub A's shared source as `REMOTE`.
8. Send mock calls into Hub A.
9. Verify Hub B can pull recent remote call metadata.

### Plesk/Portainer Operator Path

The hosted/operator path should stay close to `docker-compose.plesk.yml`:

```bash
docker-compose --env-file .env -f docker-compose.plesk.yml up -d
```

SignalHub-specific Plesk variables should be minimal:

- `APP_BASE_URL`: public URL for this hub.
- `HUB_PUBLIC_URL`: optional explicit federation URL, defaulting to `APP_BASE_URL`.
- `HUB_NAME`: display name for discovery and remote source labels.
- `HUB_REGION`: rough region such as `Yukon, OK` or `Canadian County, OK`.
- `HUB_FEDERATION_ENABLED`: `true` or `false`.

Plesk deployments should expose only the client/public app through the reverse proxy. Federation endpoints can still route through the client-facing domain if the frontend proxy passes `/api` requests to the server.

For a second hosted hub, use `docker-compose.peer.yml` with `.env.peer.example`. Give it a separate Plesk/Portainer stack name, a separate public subdomain, and a separate `CLIENT_PORT`. The stack can still use the same published images and image tag as the main hub.

Public update discovery uses `https://signalforge.org/p7-scanner-update.json`. CI writes that manifest after public images publish, and each hub exposes `GET /api/v1/update-check` so the UI can warn admins when their deployed tag is behind the current SignalForge image tag.

Second-stack mental model:

- Hub A is your main P7 Scanner deployment.
- Hub B is the peer stack from `docker-compose.peer.yml`.
- Hub A generates the invite token.
- Hub B connects to Hub A using Hub A's public URL and that token.
- After the handshake, Hub A stores Hub B as `inbound`; Hub B stores Hub A as `outbound`.

### MVP Acceptance Checks

- A fresh local compose hub can configure identity from the UI.
- A second local compose hub can add the first hub as a peer.
- Shared remote sources appear with origin labels.
- Remote sources are not re-shared by default.
- Disabling a peer hides or deactivates its remote sources locally.
- Restarting containers preserves hub identity, peers, and remote source metadata.
- The same flow works in a Plesk-style compose deployment with only environment changes.

## Initial Data Model

Hub identity:

- Hub ID
- Display name
- Public URL
- Region
- Contact email or handle
- Public key
- Directory validation status

Peer subscription:

- Peer hub ID
- Peer URL
- Invite token or shared secret
- Enabled state
- Last sync time
- Allowed topics or source IDs

Remote source metadata:

- Origin hub ID
- Publisher hub ID
- Source ID
- Source label
- System ID and label
- Share policy
- Last seen time

## Federation Topics

Start with simple HTTP/SSE/WebSocket pub/sub semantics instead of requiring a broker.

Candidate topics:

- `hub.{hubId}.sources`
- `hub.{hubId}.calls.live`
- `hub.{hubId}.calls.metadata`
- `hub.{hubId}.sources.{sourceId}`
- `region.{region}.calls`

## Share Policies

- `localOnly`: do not publish outside the origin hub.
- `directPeers`: approved direct peers may subscribe, but cannot relay.
- `federated`: approved peers may relay downstream.
- `directoryListed`: source or hub can appear in SignalForge discovery.

## V1 Endpoints

Potential starting endpoints:

- `GET /api/v1/hub/identity`
- `PUT /api/v1/hub/identity`
- `GET /api/v1/hub/invites`
- `POST /api/v1/hub/invites`
- `POST /api/v1/hub/invites/accept`
- `DELETE /api/v1/hub/invites/{id}`
- `GET /api/v1/hub/peers`
- `POST /api/v1/hub/peers`
- `DELETE /api/v1/hub/peers/{id}`
- `GET /api/v1/federation/sources`
- `GET /api/v1/federation/calls?since=...`
- `GET /api/v1/federation/stream?topics=...`

## MVP Non-Goals

- No global central broker requirement.
- No anonymous public relay by default.
- No automatic remote-of-remote resharing.
- No guarantee that SignalForge is required for hub-to-hub operation.

## Launch Positioning

P7 Scanner is the self-hosted scanner hub. SignalHub is the federation protocol. SignalForge is the public community surface for discovery, docs, releases, and trusted validation.
