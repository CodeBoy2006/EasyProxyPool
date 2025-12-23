## [2025-12-23 20:32] Add AGENTS contributor guide
- **Changes:** Added `AGENTS.md` with repo structure, commands, style, testing, and PR guidance.
- **Status:** Completed
- **Next Steps:** (Optional) Add unit tests for pool selection and health-check edge cases.
- **Context:** Go tooling writes to the Go build cache outside the workspace sandbox (may require an escalated run for `go test`).
