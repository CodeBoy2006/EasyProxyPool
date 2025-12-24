# EasyProxyPool

English | [中文](README.zh-CN.md)

EasyProxyPool is a local **SOCKS5 + HTTP/HTTPS (CONNECT)** proxy that rotates requests through a dynamic pool of upstream SOCKS5 proxies.

It continuously fetches proxy lists from multiple sources, health-checks them, and keeps a single pool (RELAXED mode for maximum compatibility).

## Features

- Multi-source proxy list fetch + de-duplication
- Concurrent health checks with latency thresholding
- Single pool with SOCKS5 + HTTP listeners
- Per-request upstream selection (`round_robin` or `random`)
- Optional sticky upstream selection via session key (HTTP proxy only)
- Retries with exponential backoff + temporary upstream disable on failures
- Optional authentication:
  - HTTP: `Proxy-Authorization: Basic ...`
  - SOCKS5: username/password
- Optional admin API with `/healthz` and `/status`
- Structured logging (Go `slog`)

## Quick start

### Build

```bash
go build -o easyproxypool ./cmd/easyproxypool
```

### Run

```bash
./easyproxypool -config config.yaml
```

Override log level (env overrides config):

```bash
LOG_LEVEL=debug ./easyproxypool -config config.yaml
```

### Test

```bash
# SOCKS5
curl --socks5 127.0.0.1:17283 https://api.ipify.org

# HTTP
curl -x http://127.0.0.1:17285 https://api.ipify.org
```

## Docker

```bash
docker build -t easyproxypool .
docker run -d \
  --name easyproxypool \
  -p 17283:17283 -p 17285:17285 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  --restart unless-stopped \
  easyproxypool
```

Or:

```bash
docker-compose up -d
```

## Configuration

Edit `config.yaml`.

Key options:

- `proxy_list_urls`: list sources (each should return `ip:port` lines; `socks5://ip:port` also accepted)
- `sources`: typed sources (e.g. `clash_yaml`) (optional; can be used instead of `proxy_list_urls`)
- `health_check.*`: timeouts + TLS handshake target and threshold
- `ports.*`: listening addresses for the local proxies
- `selection.*`: upstream selection + retries/backoff behavior
- `selection.sticky.*`: session-key sticky upstream selection (optional)
- `auth.*`: enable proxy auth (recommended if binding to non-local interfaces)
- `admin.*`: optional admin API + embedded dashboard (`/ui/`) + SSE logs
- `adapters.xray.*`: enable xray-core adapter for Clash-style nodes (optional; default disabled)

### Authentication

`auth.mode`:

- `disabled`: no auth
- `basic`: require exact username/password match
- `shared_password`: allow any username, require password match (useful when you want username as a session/tenant key)

Example (shared password):

```yaml
auth:
  mode: shared_password
  password: "shared-secret"
```

Then clients can send any username:

```bash
curl -x http://127.0.0.1:17285 -U 'tenantA:shared-secret' https://api.ipify.org
curl -x http://127.0.0.1:17285 -U 'tenantB:shared-secret' https://api.ipify.org
```

### Session-key sticky egress (HTTP proxy only)

If you want to pin a "session" to a fixed egress IP (upstream node), enable `selection.sticky`.
EasyProxyPool will use **Rendezvous (HRW) hashing** over the current alive upstream set, so when nodes
appear/disappear, most sessions keep their existing mapping.

Session key sources (highest priority first):

1) `X-EasyProxyPool-Session` (when `selection.sticky.header_override=true`)
2) `Proxy-Authorization` Basic username (works best with `auth.mode: shared_password`)
3) W3C `traceparent` trace-id (fallback)

Per-request overrides (optional; controlled by `selection.sticky.header_override`):

- `X-EasyProxyPool-Sticky: on|off`
- `X-EasyProxyPool-Failover: soft|hard`
- `X-EasyProxyPool-Upstream: <entryKey>` (forces a specific upstream key)

Examples:

```bash
# HTTPS via CONNECT (send headers to proxy)
curl -x http://127.0.0.1:17285 \
  --proxy-header 'X-EasyProxyPool-Session: s-123' \
  https://api.ipify.org

# Or: use traceparent as a session key fallback
curl -x http://127.0.0.1:17285 \
  --proxy-header 'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' \
  https://api.ipify.org
```

### Clash YAML + xray-core adapter (optional)

To use Clash format nodes (vmess/vless/trojan/ss/socks5/http) without implementing each protocol in Go,
enable the xray adapter and add a `clash_yaml` source:

```yaml
sources:
  - type: clash_yaml
    path: "./clash.yaml"   # or url: "https://example.com/clash.yaml"

adapters:
  xray:
    enabled: true
    binary_path: "/usr/local/bin/xray"
    # Keep xray on loopback. EasyProxyPool routes each connection by SOCKS username (= nodeID).
    socks_listen_relaxed: "127.0.0.1:17383"
    # Used for polling /debug/vars (observatory alive/delay)
    metrics_listen_relaxed: "127.0.0.1:17387"
    fallback_to_legacy_on_error: true
```

Notes:

- EasyProxyPool runs a single xray instance (RELAXED) and routes each connection by SOCKS username (= nodeID).
- Observatory uses HTTPing probes; tune `adapters.xray.observatory.*` for your environment.
- If xray fails to start or metrics are unavailable, EasyProxyPool keeps the existing pool; with
  `fallback_to_legacy_on_error: true` it will also try the legacy `proxy_list_urls` pipeline.

### Security / licensing

- Do not expose the local proxy ports publicly without auth and network controls.
- Keep xray SOCKS/metrics listeners on loopback interfaces.
- xray-core is MPL-2.0; if you redistribute xray binaries with your image/release, include its license and notices.

## Admin API / Dashboard (optional)

Enable `admin.enabled: true` (default addr `127.0.0.1:17287`).

Endpoints:

- Health check: `GET /healthz` (can be configured to allow unauthenticated access)
- Status JSON: `GET /status` or `GET /api/status`
- Build/runtime info: `GET /api/info`
- Node health snapshot: `GET /api/nodes`
- Live logs (SSE): `GET /api/events/logs`
- Web UI: `GET /ui/` (redirect from `/` when `admin.ui_enabled: true`)

Examples (no auth):

```bash
curl http://127.0.0.1:17287/healthz
curl http://127.0.0.1:17287/status
curl http://127.0.0.1:17287/ui/
```

Examples (recommended `admin.auth.mode: shared_token`):

```bash
curl -H 'Authorization: Bearer <token>' http://127.0.0.1:17287/api/info
curl -N 'http://127.0.0.1:17287/api/events/logs?token=<token>&since=0&level=info'
```

Security note: do not expose the admin/dashboard port publicly; keep it on loopback and use auth + network controls.
Rollback/disable: set `admin.enabled: false` (or `admin.ui_enabled: false` to keep API-only).

## Security notes

- Treat upstream “free proxy lists” as untrusted.
- Do not expose this proxy publicly without authentication and network controls.

## License

MIT
