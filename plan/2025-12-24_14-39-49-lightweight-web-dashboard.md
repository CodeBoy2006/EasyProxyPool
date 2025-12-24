---
mode: plan
cwd: /home/codeboy/EasyProxyPool
task: 增加轻量的 Web 仪表盘（状态/统计/实时日志/节点状态）
complexity: complex
planning_method: builtin
created_at: 2025-12-24T14:39:57+08:00
---

# Plan: Lightweight Web Dashboard

🎯 任务概述
当前项目已有可选的 Admin API（`/healthz`、`/status`），但缺少可视化与实时观测能力。
目标是在不引入重型前端构建链的前提下，增加一个轻量 Web 仪表盘：展示运行状态与统计、逐节点实时状态（xray observatory）、以及实时日志流；同时保持默认安全（避免敏感信息泄漏、默认不建议暴露到公网）。

📋 执行计划
1. 明确仪表盘 MVP 范围与安全边界（"轻量" 的定义）
   - 输出：页面信息架构（状态卡片/节点表格/日志面板）、刷新频率目标、数据脱敏规则（不返回密码/完整 upstream URL 等）。
   - 约定：仪表盘随 `admin.enabled` 启用；监听地址建议回环；如需公网访问，必须启用简易鉴权并配合 ACL。
   - 鉴权建议：优先采用“共享 token”（浏览器与 SSE 更友好），可选支持 Basic Auth 便于 curl/脚本。

2. 设计并扩展 Admin API（在保持 `/status` 兼容的前提下）
   - 新增（建议）：`GET /api/status`（兼容现有 `/status` 的 JSON 结构，或内部复用 handler）、`GET /api/info`（版本/启动时间/运行时信息）、`GET /api/nodes`（节点健康与延迟快照）。
   - 增加鉴权中间件：默认保护 `/`、`/ui/*`、`/api/*`、`/status`、`/api/events/*`；`/healthz` 可选择保持无鉴权（便于 liveness/readiness）或也受保护（通过配置控制）。
   - 原则：所有响应只包含“可观测信息”，不包含 `Password` 等敏感字段；字段结构稳定并带 `server_time_utc`。

3. 在 orchestrator 层持久化“节点健康快照”（供 UI 读取）
   - 方式 A（更轻）：在 `orchestrator.Status` 中新增只读快照字段（例如 `LastNodeHealthRelaxed map[string]xray.NodeHealth` + 更新时间），`Updater.runOnceXray` 在 `metricsRelaxed.Fetch` 后写入。
   - 方式 B（更可控）：新增 `internal/observability/` 模块，负责保存节点健康快照（可做容量限制/裁剪），admin 只依赖该模块读取。
   - 输出：`/api/nodes` 可以返回按 nodeID/outboundTag 聚合的（alive/delay/last_seen/last_try）列表，并提供总数/存活数/延迟分位数（p50/p90，可选）。

4. 实现轻量“实时日志”采集与推送（建议 SSE）
   - 为 `slog` 增加一个“扇出 handler”：在保留现有 stdout 输出的同时，将结构化日志写入内存环形缓冲区（固定容量，例如 1k~5k 行），并允许订阅。
   - Admin 新增：`GET /api/events/logs`（SSE），支持参数：`since`（从某个序号开始）、`level`（过滤）。
   - SSE 鉴权注意：浏览器 `EventSource` 不支持自定义 Header；建议支持 `?token=...`（或 Cookie）方式鉴权，并确保服务端不把 token 打进日志。
   - 目标：单客户端/少量客户端低开销；客户端断开自动释放；服务端对慢客户端做丢弃/断开策略。

5. 实现仪表盘前端（无构建链、可 embed）
   - 形式：`internal/server/admin` 通过 Go `embed` 打包静态资源（单页 `index.html` + 极少量 JS/CSS），路由：`/` 或 `/ui/`。
   - 页面能力：
     - 状态区：轮询 `GET /api/status`（2s~5s）展示 updater 上次更新、池规模、错误信息。
     - 节点区：轮询 `GET /api/nodes`（2s~5s）展示 alive/delay/last_seen，支持搜索与排序。
     - 日志区：连接 `GET /api/events/logs` SSE，追加显示并提供“暂停/清空/关键字过滤”。
   - UI 简易鉴权交互：提供 token 输入框（或 basic 用户名/密码），写入 localStorage；之后所有 `fetch` 与 SSE 连接都附带 token（Header 或 query 参数）。

6. 配置项与文档补齐（默认安全）
   - 配置建议（新增到 `admin` 节）：
     - `ui_enabled`（默认 true 或与 `admin.enabled` 同步，需决定兼容策略）
     - `auth`（简易鉴权；建议独立于代理的 `auth.*`，避免混用）
       - `auth.mode`: `disabled|basic|shared_token`（建议默认 `disabled`，但当监听非回环地址时强烈建议启用）
       - `auth.username` / `auth.password`（mode=basic）
       - `auth.token`（mode=shared_token，UI/SSE 推荐）
       - `auth.allow_unauthenticated_healthz`（默认 true）
     - `log_buffer_lines`（默认 2000）
     - `sse_heartbeat_seconds`（默认 10）
   - 更新：`config.yaml` 示例与 `README.md` / `README.zh-CN.md` 的使用说明（访问地址、curl 示例、日志/节点端点说明、安全提示）。

7. 测试与回归验证
   - 单测：
     - 日志 ring buffer：容量、顺序、并发订阅/取消订阅。
     - 节点健康快照：写入/读取一致性、脱敏与排序逻辑。
     - Admin 鉴权：未携带 token/凭证时返回 401；携带正确 token/凭证可访问 `/api/*` 与 SSE。
   - HTTP 测试：`/api/status`、`/api/nodes` 的 JSON schema 基本断言；SSE 连接建立与事件格式（可做最小验证）。
   - 回归：`go test ./...`，并手动跑一遍 `go run ./cmd/easyproxypool -config config.yaml` 验证 admin 页面能打开且不泄漏敏感信息。

8. 发布与降级/回滚策略
   - 若 UI/日志模块异常：保证不影响核心代理转发（admin server 与代理 listener 解耦，失败仅影响观测）。
   - SSE 默认限流：限制并发连接数；当超过限制时返回 429。
   - 回滚：通过配置关闭 `admin.enabled`/`ui_enabled` 即可完全禁用仪表盘与日志流。

⚠️ 风险与注意事项
- 安全：Admin/UI 若暴露公网将泄漏运行状态与节点信息；必须默认绑定回环、并提供可选认证；所有 API 响应必须彻底脱敏（尤其是 `pool.Entry.Password`、上游 URL、可能包含 token 的日志）。
- 鉴权实现细节：token 若通过 query 参数传递，必须避免进入 access log/错误日志（包括 panic/trace）；UI 端也要避免把 token 写入页面可见日志。
- 性能：节点数 1000+ 时，`/api/nodes` 频繁全量返回会有压力；需要分页/裁剪/降低频率，或仅返回变更（后续可演进）。
- 日志可靠性：内存 ring buffer 会丢旧日志；需要在文档中说明“仅用于实时观察，不保证持久化”。

📝 修订记录
- 2025-12-24：补充“WebUI 需要简易鉴权”的实现要点（推荐 shared_token，覆盖 SSE 的浏览器限制）。

📎 参考
- `internal/server/admin/admin.go:31`（现有 admin mux：/healthz、/status）
- `internal/orchestrator/status.go:8`（现有 Status/Snapshot，可扩展节点快照）
- `internal/orchestrator/updater.go:133`（xray 模式拉取 metrics 并构建 pool，可注入节点状态快照）
- `internal/xray/metrics.go:13`（NodeHealth 结构与 ParseDebugVars）
- `internal/logging/logging.go:9`（当前 slog 输出，可扩展为 fanout handler）
- `README.md:176`（Admin API 说明）
- `README.zh-CN.md:174`（管理接口说明）
