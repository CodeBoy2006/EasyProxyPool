# EasyProxyPool

EasyProxyPool is a local **SOCKS5 + HTTP/HTTPS (CONNECT)** proxy that rotates requests through a dynamic pool of upstream SOCKS5 proxies.

It continuously fetches proxy lists from multiple sources, health-checks them, and keeps two pools:

- **STRICT**: upstreams that pass a verified TLS handshake
- **RELAXED**: upstreams that pass a TLS handshake without certificate verification (more compatible)

## Features

- Multi-source proxy list fetch + de-duplication
- Concurrent health checks with latency thresholding
- Two pools (STRICT / RELAXED) with separate listeners for SOCKS5 + HTTP
- Per-request upstream selection (`round_robin` or `random`)
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
# SOCKS5 STRICT
curl --socks5 127.0.0.1:17283 https://api.ipify.org

# SOCKS5 RELAXED
curl --socks5 127.0.0.1:17284 https://api.ipify.org

# HTTP STRICT
curl -x http://127.0.0.1:17285 https://api.ipify.org

# HTTP RELAXED
curl -x http://127.0.0.1:17286 https://api.ipify.org
```

## Docker

```bash
docker build -t easyproxypool .
docker run -d \
  --name easyproxypool \
  -p 17283:17283 -p 17284:17284 -p 17285:17285 -p 17286:17286 \
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
- `health_check.*`: timeouts + TLS handshake target and threshold
- `ports.*`: listening addresses for the 4 local proxies
- `selection.*`: upstream selection + retries/backoff behavior
- `auth.*`: enable proxy auth (recommended if binding to non-local interfaces)
- `admin.*`: optional status API

## Admin API (optional)

Enable `admin.enabled: true` (default addr `:17287`), then:

```bash
curl http://127.0.0.1:17287/healthz
curl http://127.0.0.1:17287/status
```

## Security notes

- Treat upstream “free proxy lists” as untrusted.
- Do not expose this proxy publicly without authentication and network controls.

## License

MIT

