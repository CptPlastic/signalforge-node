# Authentication Gating Implementation

**Completed**: May 10, 2026

## Overview
All protected views and data endpoints now require user authentication. Unauthenticated users see a login-first screen with no access to scanner data.

## Backend Changes

### Protected Endpoints (Require Authentication)
All data endpoints now enforce authentication via `requireAuthenticated()` checks:

- `GET /api/v1/calls` — call list
- `GET /api/v1/calls/{id}/audio` — call audio download
- `GET /api/v1/sources` — source listing
- `PUT /api/v1/sources` — source creation/updates
- `DELETE /api/v1/sources/{id}` — source deletion
- `POST /api/v1/sources/{id}/keys` — API key generation
- `GET /api/v1/sources/{id}/keys` — API key listing
- `DELETE /api/v1/sources/{id}/keys/{keyId}` — API key revocation
- `GET /api/v1/talkgroups/settings` — talkgroup settings
- `PUT /api/v1/talkgroups/{talkgroup}/settings` — talkgroup setting updates
- `GET /ws` — WebSocket live stream

**Files Modified**:
- [server/internal/api/calls.go](server/internal/api/calls.go) — Added auth checks to `handleListCalls` and `handleCallAudio`
- [server/internal/api/sources.go](server/internal/api/sources.go) — Added auth checks to source endpoints and key management
- [server/internal/api/talkgroups.go](server/internal/api/talkgroups.go) — Added auth checks to talkgroup settings
- [server/internal/api/hub.go](server/internal/api/hub.go) — Updated WebSocket handler to require auth
- [server/internal/api/health.go](server/internal/api/health.go) — Added `handleWebSocket` wrapper that enforces auth
- [server/internal/api/router.go](server/internal/api/router.go) — Wired handler's WebSocket wrapper

### Public Endpoints (No Auth Required)
These endpoints remain public for bootstrapping auth and health checks:
- `GET /api/v1/health` — health check
- `GET /api/v1/version` — version/build metadata
- `POST /api/v1/auth/magic-link` — request login link
- `GET /api/v1/auth/verify?token=X` — verify and login
- `POST /api/call-upload` — ingestion endpoint (uses API key from source)

## Frontend Changes

### Login-First UI
When unauthenticated, users now see:
- A prominent `🔐 AUTHENTICATION REQUIRED` message
- Instructions to sign in
- Only the `account` tab is available

**File Modified**: [client/src/App.tsx](client/src/App.tsx)

### Protected Views
The following views now require authentication:
- `monitor` — call log and playback
- `integrations` — source management and upload endpoint
- `talkgroups` — talkgroup viewer and settings

These views are hidden if `!authUser`, and tabs are disabled with conditional rendering:
```tsx
{authUser && activeView === 'monitor' && ( ... )}
{authUser && activeView === 'integrations' && ( ... )}
{authUser && activeView === 'talkgroups' && ( ... )}
```

### Header Auth Indicator
Added visual login status indicator in the header:
- **Locked icon** (🔒) + email when logged in — shows user's email
- **Unlocked icon** (🔓) + "GUEST" when not logged in
- Color-coded: `text-console-accent` when authenticated, `text-console-muted` when not
- Positioned in top-right of header next to system status

## Usage Flow

### For Unauthenticated Users
1. Visit the app
2. See `🔓 GUEST` indicator and authentication required message
3. Only `account` tab is clickable
4. Click "account" to access login form
5. Enter email and request access or a magic link
6. New non-bootstrap accounts are created as `pending` and must be approved by an admin before login
7. Approved users receive a magic link, verify it, and create a session
8. Session cookie is stored and used for all API requests

### For Authenticated Users
1. Logged in indicator shows `🔒 user@example.com`
2. All tabs (`monitor`, `integrations`, `talkgroups`, `account`) are clickable
3. Full access to call log, source management, and settings
4. Session remains active until logout or expiration

## Security Notes

- **Session-based**: Uses HTTP-only session cookies (configured in Go server)
- **Approval-gated signup**: The first account bootstraps as active admin; later new accounts are `pending` until an admin approves them
- **Per-request auth**: `withUserContext` middleware loads user from session cookie for each request
- **Inactive-account rejection**: Pending and disabled users cannot create active sessions from magic links
- **Cascading protection**: Each handler explicitly checks `requireAuthenticated()` to ensure no accidental bypasses
- **WebSocket auth**: WebSocket connections require valid session before upgrade
- **API key uploads**: `/api/call-upload` still allows unauthenticated requests if valid source API key provided

## Testing

To verify login gating:

1. **Open app without logging in**:
   - Should see `🔓 GUEST` and "AUTHENTICATION REQUIRED" message
   - Tabs should be disabled except `account`
   - Try clicking monitor/integrations/talkgroups — nothing renders

2. **Request magic link**:
   - Use account tab to request magic link
   - Existing active users should receive email (or see mock token in logs)
   - New users should see an approval-pending message and appear in admin user management as `pending`
   - Admins can approve pending users by changing status to `active` or clicking `APPROVE`
   - After approval, request a fresh magic link and copy token/visit link

3. **Verify login works**:
   - After magic link verification, should see `🔒 user@example.com`
   - All tabs should be enabled
   - Should see call log, sources, and talkgroups

4. **Test logout**:
   - Click account tab → logout button
   - Session cookie is deleted
   - Indicator reverts to `🔓 GUEST`
   - Protected views become inaccessible again

5. **Test API endpoint gating**:
   - Without session: `curl https://localhost/api/v1/calls` → `401 Unauthorized`
   - With session: Same request → `200 OK` with call list

## Deployment Notes

- Rebuild containers with `docker-compose --profile mock up -d --build --force-recreate`
- All existing deployments inherit the auth gates automatically
- No database migrations needed (auth schema already exists)
- Magic link emails require SMTP configuration (check server logs for tokens in dev)

---

**Next Steps**: See [PLAN.md](PLAN.md) for multi-tenancy and per-user data scoping roadmap.
