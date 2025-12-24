## [2025-12-23 20:32] Add AGENTS contributor guide
- **Changes:** Added `AGENTS.md` with repo structure, commands, style, testing, and PR guidance.
- **Status:** Completed
- **Next Steps:** (Optional) Add unit tests for pool selection and health-check edge cases.
- **Context:** Go tooling writes to the Go build cache outside the workspace sandbox (may require an escalated run for `go test`).

## [2025-12-23 21:24] Plan: Clash YAML nodes via xray-core
- **Changes:** Updated plan to choose xray-core (MPL-2.0) as the protocol adapter runtime and refined the connector approach, support scope, and downgrade strategy.
- **Status:** Completed
- **Next Steps:** Implement `sources` config + Clash YAML parser + xray-core connector manager, then add unit tests and update docs/examples.
- **Context:** MVP approach uses an external xray-core process to expose per-node local SOCKS5 inbounds (`127.0.0.1:port`) to keep the existing health-check and pool logic unchanged.

## [2025-12-23 22:17] Plan update: xray observatory-driven per-node health
- **Changes:** Updated plan to avoid “one node one local port” by using a single SOCKS inbound with per-connection user routing, and to source per-outbound alive/delay from xray (burst)Observatory via expvar `/debug/vars`.
- **Status:** Completed
- **Next Steps:** Decide strict/relaxed deployment (two xray instances vs per-outbound TLS toggles), then implement xray process manager + metrics poller and integrate into updater/pool.
- **Context:** This keeps “per-node management” while scaling to large node counts; config size becomes O(N) due to accounts + routing rules, so `max_nodes` and clear observability are required.

## [2025-12-23 22:22] Issues CSV: Clash YAML xray observatory
- **Changes:** Generated `issues/issues.csv` and a timestamped snapshot from the plan, with acceptance/review/test requirements for each phase.
- **Status:** Completed
- **Next Steps:** Assign owners, then start implementing issues in priority order (P0 first).
- **Context:** CSV is UTF-8 with BOM for Excel compatibility; all tasks start at `未开始`/`未提交`.

## [2025-12-24 12:34] Relaxed-only runtime
- **Changes:** Run only RELAXED mode (single pool) and start only one set of proxy listeners; xray adapter now runs a single instance/metrics path. Updated docs, config example, Dockerfile and docker-compose ports.
- **Status:** Completed
- **Next Steps:** (Optional) Remove strict-related config fields and API keys in a breaking-change release, or keep as deprecated for compatibility.
- **Context:** Strict/relaxed pool stats in `/status` may both show the same pool because strict is no longer a distinct mode.

## [2025-12-24 12:39] Docs tweak: ports wording
- **Changes:** Updated README wording so `ports.*` no longer implies 4 listeners.
- **Status:** Completed
- **Next Steps:** None.
- **Context:** N/A.

## [2025-12-24 13:03] HTTP traceparent sticky upstream
- **Changes:** Added `selection.sticky` config and implemented HTTP proxy sticky upstream selection keyed by W3C `traceparent` trace-id (TTL + max entries), with optional header overrides and soft/hard failover behavior.
- **Status:** Completed
- **Next Steps:** (Optional) Expose a safe way to discover `entry.Key()` (nodeID) for `X-EasyProxyPool-Upstream` without leaking secrets.
- **Context:** Sticky selection applies only to HTTP proxy requests where `traceparent` is visible to the proxy (e.g. `curl --proxy-header` for CONNECT).

## [2025-12-24 13:17] Replace sticky-map with Rendezvous (HRW) selection
- **Changes:** Replaced the TTL sticky-map approach with session-key based Rendezvous (HRW) hashing over alive upstreams; session key can come from `Proxy-Authorization` username or `X-EasyProxyPool-Session` (fallback `traceparent`), and failures try the next-ranked upstream (retries).
- **Status:** Completed
- **Next Steps:** Consider adding an admin endpoint to list safe upstream keys (no secrets) to make `X-EasyProxyPool-Upstream` easier to use.
- **Context:** `X-EasyProxyPool-Session` is only honored when `selection.sticky.header_override=true`; the header is stripped from forwarded requests.

## [2025-12-24 14:24] Add shared-password auth mode
- **Changes:** Added `auth.mode=shared_password` to allow any username and validate only the password (shared secret) for both HTTP and SOCKS5, enabling “username as session/tenant key” while keeping a shared credential.
- **Status:** Completed
- **Next Steps:** (Optional) Add rate limiting / audit logs for auth failures if exposed beyond localhost.
- **Context:** `auth.mode` defaults to `disabled` when `auth.username` is empty, otherwise `basic` for backward compatibility.

## [2025-12-24 14:30] Prune completed issues CSV
- **Changes:** Removed all completed rows from `issues/issues.csv`, leaving only the header for a clean slate.
- **Status:** Completed
- **Next Steps:** Add new issues rows as new work is defined.
- **Context:** File kept as UTF-8 with BOM for Excel compatibility.

## [2025-12-24 14:41] Web Dashboard Plan
- **Changes:** Added implementation plan document for lightweight web dashboard (status/stats/live logs/node health).
- **Status:** Completed
- **Next Steps:** Follow the plan phases; start by finalizing MVP scope + security defaults, then extend admin API and add SSE log streaming.
- **Context:** Plan assumes reuse of existing admin server (/healthz, /status) and xray node health (/debug/vars) with strict data sanitization.

## [2025-12-24 14:45] Update Web Dashboard Plan: Add Auth
- **Changes:** Updated the web dashboard plan to include simple WebUI authentication (recommended shared token; SSE constraints considered).
- **Status:** Completed
- **Next Steps:** Implement admin auth middleware and shared-token flow first, then protect UI/API/SSE endpoints and document safe defaults.
- **Context:** Browser EventSource can’t set headers, so token-in-query (or cookie) is required for SSE; ensure tokens never appear in logs.

## [2025-12-24 14:48] Generate issues CSV: Web dashboard
- **Changes:** Generated issues tracking CSV for the lightweight web dashboard plan (8 tasks) and updated issues/issues.csv (UTF-8 BOM).
- **Status:** Completed
- **Next Steps:** Assign owners and start with WEBUI-010 (admin auth + API), then WEBUI-030 (SSE logs) and WEBUI-040 (UI).
- **Context:** CSV derived from plan/2025-12-24_14-39-49-lightweight-web-dashboard.md; includes explicit acceptance/review/test MCP per task.

## [2025-12-24 14:58] [WEBUI-010] Admin API + auth middleware
- **Changes:** Added admin auth modes (disabled/basic/shared_token) and new admin API endpoints (/api/status, /api/info, /api/nodes placeholder) with auth protection.
- **Status:** Completed
- **Next Steps:** Implement real node health in /api/nodes (WEBUI-020) and SSE logs (WEBUI-030).
- **Context:** Sandbox disallows binding TCP listeners, so runtime curl validation is limited here; go test ./... passes.
