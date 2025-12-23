# Xray Adapter Architecture (EasyProxyPool)

## Goals

- Parse Clash YAML nodes and make them usable as upstreams without implementing every protocol in Go.
- Keep **per-node** control (maximize exit IP diversity) while supporting **large node counts**.
- Preserve legacy behavior when `adapters.xray.enabled=false`.

## Data Plane

- EasyProxyPool connects only to local xray SOCKS5 inbounds:
  - STRICT: `adapters.xray.socks_listen_strict`
  - RELAXED: `adapters.xray.socks_listen_relaxed`
- Each request/connection selects a **nodeID** from the pool and sets SOCKS5 auth:
  - `username = nodeID` (e.g. `n-<hash>`)
  - `password = adapters.xray.user_password` (shared constant; keep xray on loopback)
- Xray routes by `routing.rules[].user -> outboundTag` so every connection can target a specific outbound.

## Control Plane

- EasyProxyPool loads sources:
  - `proxy_list_urls` (legacy line-based SOCKS5 lists)
  - `sources` (typed sources like `clash_yaml`)
- For xray mode:
  1. Convert sources into `UpstreamSpec` nodes (stable `nodeID`).
  2. Generate xray config for STRICT and RELAXED.
  3. Start/ensure xray processes (external binary).
  4. Poll xray `metrics.listen` expvar `/debug/vars` and read `observatory` results.
  5. Build STRICT/RELAXED pools from `alive` + `delay` per outbound.

## Health Source

- In xray mode, pool health comes from xray (burst)Observatory (HTTPing-based).
- In legacy mode, pool health comes from EasyProxyPool TLS handshake checks through upstream SOCKS5.

## Key Trade-offs

- Avoids “one node one local port” by using **single inbound + user routing**.
- Xray config size is **O(N)** due to accounts + routing rules; `adapters.xray.max_nodes` limits growth.
- Observatory probes may differ from real traffic; tune `adapters.xray.observatory.*` and inspect `/status`.

## References

- Example configuration: `config.yaml`
- Updater integration: `internal/orchestrator/updater.go`
- Xray config generator: `internal/xray/config.go`

