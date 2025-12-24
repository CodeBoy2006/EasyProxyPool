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
