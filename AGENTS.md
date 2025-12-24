# Repository Guidelines

## Project Structure

- `cmd/easyproxypool/`: main entrypoint (`main.go`) and CLI flags.
- `internal/`: application modules (not imported by other repos):
  - `config/`: YAML config loading and defaults.
  - `fetcher/`, `health/`: proxy list retrieval and upstream health checks.
  - `pool/`: proxy pool storage and selection helpers.
  - `clash/`, `sources/`, `upstream/`, `xray/`: Clash YAML parsing, typed source loading, node specs, and optional xray-core adapter.
  - `orchestrator/`: background updater loop and shared status.
  - `server/`: listeners (`socks5proxy/`, `httpproxy/`) and optional admin API (`admin/`).
- Root files: `config.yaml` (runtime config), `Dockerfile`, `docker-compose.yml`, `README.md`.

## Build, Test, and Development Commands

- Build binary: `go build -o easyproxypool ./cmd/easyproxypool`
- Run locally: `./easyproxypool -config config.yaml`
- Run from source: `go run ./cmd/easyproxypool -config config.yaml`
- Tests (and compile check): `go test ./...`
- Format (required): `gofmt -w .`
- Docker: `docker build -t easyproxypool .` and `docker-compose up -d`

## Coding Style & Naming

- Go `gofmt` style; tabs for indentation (default Go formatting).
- Keep packages small and cohesive; prefer `internal/<area>` over cross-cutting utilities.
- Naming: exported identifiers use `CamelCase`; unexported use `camelCase`. Prefer descriptive names over abbreviations.
- Config changes must update `config.yaml` and relevant docs in `README.md` (and `README.zh-CN.md` if user-facing).

## Testing Guidelines

- Use Go’s standard testing (`*_test.go`). Name tests as `TestXxx` and table-test where appropriate.
- New behavior should include unit tests when feasible; at minimum ensure `go test ./...` passes.

## Commit & Pull Request Guidelines

- Commit messages in this repo are short and imperative (e.g., “Add Chinese README”).
- PRs should include: a clear description, rationale, and any config changes (with example snippets). Include curl examples for proxy/admin behavior when relevant.

## Security & Configuration Tips

- Do not expose proxy listeners publicly without authentication (`auth.*`) and network controls.
- Treat upstream proxy lists as untrusted input; keep sources minimal and audited.
