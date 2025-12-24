# Admin Dashboard (Web UI)

EasyProxyPool includes an optional admin API and a lightweight embedded Web dashboard for runtime observability:

- Service status & pool stats
- Node health snapshot (from xray observatory when enabled)
- Live logs (SSE)

## MVP scope (what it is / is not)

**In scope (MVP)**

- **Status**: updater last run timestamps/error, pool sizes, server time.
- **Nodes**: per-node `alive` + `delay_ms` + last seen/try timestamps.
- **Logs**: tail + stream of recent logs for debugging/ops.

**Out of scope (MVP)**

- Editing config / mutating runtime state
- Persisted audit logs or guaranteed log retention
- Detailed per-request tracing

## Information architecture & refresh targets

- **Status cards**: poll `/api/status` every ~4s.
- **Nodes table**: poll `/api/nodes` every ~6s (node counts can be large; avoid tight loops).
- **Live logs**: connect to `/api/events/logs` via SSE (with a heartbeat).

## Data sources / endpoints

- `GET /healthz`: health check (can be configured to allow unauthenticated access)
- `GET /status` or `GET /api/status`: JSON snapshot for updater + pool stats
- `GET /api/info`: build/runtime info (Go version, build info, uptime)
- `GET /api/nodes`: node health snapshot
- `GET /api/events/logs`: live logs via SSE (supports `since` + `level`)
- `GET /ui/`: embedded dashboard UI (redirect from `/` when enabled)

## Redaction rules (must not leak secrets)

**Never return or log**:

- Proxy credentials (`auth.password`, upstream usernames/passwords, xray per-node accounts)
- Admin shared token (`admin.auth.token`) and any bearer tokens
- Full upstream URLs that may contain query tokens/secrets
- Full generated xray config JSON

When adding new fields to admin API responses, treat them as public and verify they contain only operational metadata.

## Default-safe behavior

- Admin is **disabled by default**: `admin.enabled: false`.
- When enabled and `admin.addr` is not provided, it defaults to **loopback**: `127.0.0.1:17287`.
- If you bind the admin port to non-loopback interfaces, you should:
  - Enable admin auth (`admin.auth.mode`) — **use `shared_token` for the Web UI + SSE**
  - Restrict access via firewall / security group / reverse proxy ACL

Note: browser `EventSource` (SSE) cannot set custom headers; `shared_token` supports `?token=...` for SSE.

## References

- `internal/server/admin/admin.go:1` (admin routes, auth, UI, SSE)
- `internal/orchestrator/status.go:8` (status + node health snapshot)
- `internal/logging/logbuffer.go:1` (bounded log buffer)

