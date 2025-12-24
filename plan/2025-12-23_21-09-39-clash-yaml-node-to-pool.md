---
mode: plan
cwd: /home/codeboy/EasyProxyPool
task: 增加解析 Clash 格式配置 YAML，并将常见机场协议节点转换为可加入代理池的上游代理
complexity: complex
planning_method: builtin
created_at: 2025-12-23T21:09:39+08:00
updated_at: 2025-12-24T12:34:00+08:00
---

# Plan: Clash YAML 节点导入代理池

🎯 任务概述
当前 EasyProxyPool 仅支持将“上游 SOCKS5 代理（ip:port）”加入代理池并轮询使用。目标是新增对 Clash 配置 YAML 的解析能力，从中提取常见机场协议（SS/SSR/Vmess/VLESS/Trojan/Hysteria(2)/等）节点，并将其转换为可被本项目使用的“上游代理”，最终加入代理池参与测活与轮询。

本计划确定使用 xray-core (MPL-2.0) 作为协议适配运行时，但避免“一节点一端口”的低效模式：改为“单 SOCKS 入站 + 每条连接指定节点(outbound)”，并利用 xray 的 (burst)Observatory + metrics(expvar `/debug/vars`) 直接获得每个 outbound 的 alive/delay，来维护可用节点集合与延迟数据。

本计划后续修订为“仅跑 RELAXED（兼容优先）”：只维护一套代理池与一套对外监听端口（SOCKS5 + HTTP 一组），xray 只启动一个实例。

🏗️ 目标架构（关键决策）
- 数据面：EasyProxyPool 永远只连接本机 xray 的 SOCKS5（单实例、单端口），每次连接通过 SOCKS5 用户名携带 `nodeID`，由 xray 路由到同名 outbound。
- 控制面：EasyProxyPool 负责“解析 Clash→生成节点 specs→生成/重启 xray 配置→拉取 observatory 结果→更新池”；xray 负责“协议细节 + outbound 逐个测活（HTTPing）”。
- 测活来源：优先使用 xray expvar 的 `observatory.<tag>.*`（alive/delay/last_seen_time/last_try_time）。必要时保留 fallback：若 observatory/metrics 不可用，可降级使用当前 health.Checker 做少量抽检或完全回退到 raw socks5 列表逻辑。

📋 执行计划
1. 明确“转换为 proxy”的运行形态与边界：是仅导入 `type: socks5/http` 节点，还是要让本项目直接支持 SS/VMess/Trojan/Hysteria 等协议；基于现有实现（仅 dial SOCKS5 上游）优先选择“协议适配层”，并确定 MVP 支持列表与不支持时的降级行为。
2. 设计配置与来源模型：在 `config.yaml` 中引入新的 source 配置（例如 `sources:`），支持 `raw_list`（现有 ip:port 列表）与 `clash_yaml`（URL/本地文件）；保留 `proxy_list_urls` 向后兼容，并定义迁移策略与默认行为。
3. 定义统一的节点/上游抽象（内部数据结构）：新增 `UpstreamSpec`（含 `id/name/type/server/port/params` 等），并明确去重规则（稳定 ID：可由 `type+server+port+关键参数` 哈希生成），同时保证日志/状态输出不会泄露密码/uuid 等敏感字段。
4. 实现 Clash YAML 解析器：解析 `proxies:` 列表，按 `type` 映射到 `UpstreamSpec`（至少覆盖 ss/ssr/vmess/vless/trojan/socks5/http；hysteria/hy2 等先解析为 spec 但标记为“当前适配器不支持”），对字段缺失做校验与错误聚合；对 `proxy-groups` 仅作为可选扩展（MVP 先忽略，仅导入 `proxies`）。
5. 重构拉取与解析流水线：将 `fetcher.Fetch` 从“逐行扫描 ip:port”演进为“拉取原始内容 + 根据 source 类型选择解析器”；输出从 `[]string` 升级为 `[]UpstreamSpec`（或并行保留 raw 模式），并在 orchestrator 侧集中做汇总、去重与统计。
6. 增加“协议到可用上游”的适配层（关键里程碑，xray-core + 单入站按 user 路由 + observatory 测活）：
   - 运行方式（MVP）：以外部进程方式启动 xray-core（不作为 Go module 直接链接），仅启动一个 RELAXED 实例（端口固定、兼容优先），并生成/写入临时 xray 配置文件；当节点集合发生变化时做“幂等对比”，必要时重启 xray。
   - 数据面（避免一节点一端口）：每个 xray 实例只开放 1 个本地 SOCKS5 inbound（`auth=password`），EasyProxyPool 每次连接使用 `username=nodeID`（password 固定常量即可），xray 使用 routing `user` 字段将该连接路由到 `outboundTag=nodeID`。
   - 可观测测活：启用 `burstObservatory`（或 `observatory`）并设置 `subjectSelector=["n-"]`（前缀匹配 tag），令 xray 对所有节点 outbound 做 HTTPing 探测；同时启用 `metrics.listen` 以提供 expvar `/debug/vars`，让 EasyProxyPool 拉取 `observatory.<tag>.alive/delay/...` 作为“逐节点存活与延迟”的权威来源。
   - 配置项建议（面向可运维）：`adapters.xray.enabled`、`adapters.xray.binary_path`、`adapters.xray.work_dir`、`adapters.xray.socks_listen_relaxed`、`adapters.xray.metrics_listen_relaxed`、`adapters.xray.user_password`（固定常量）、`adapters.xray.observatory.mode(observatory|burst)`、`adapters.xray.observatory.destination`、`adapters.xray.observatory.connectivity`、`adapters.xray.observatory.interval`、`adapters.xray.observatory.sampling`、`adapters.xray.observatory.timeout`、`adapters.xray.max_nodes`。
   - 支持范围（随 xray-core 能力）：优先打通 vmess/vless/trojan/shadowsocks；对 SSR / Hysteria(2) 等不在 xray-core 能力范围内的节点，MVP 先跳过并在状态中统计原因，为后续引入多适配器预留扩展点。
7. 接入 updater（以 observatory 为健康来源）：`Updater.runOnce` 改为“拉取 sources → 解析 specs → 生成并启动/更新 xray → 拉取 xray metrics `/debug/vars` → 根据 `observatory` 结果构建 pool entries → 更新池”；补充 status 统计（总节点数、可用节点数、按协议/来源计数、适配失败原因汇总、以及 xray 进程状态与最后一次配置版本哈希）。
8. 测试与回归：新增 YAML 解析单元测试（用典型 Clash 样例覆盖多协议字段），为 `ConnectorManager` 提供可 mock 的 runner（避免测试依赖外部二进制）；回归验证 raw list 行为不变，并确保 `go test ./...` 通过。
9. 文档与可运维性：更新 `README.md`/`README.zh-CN.md` 与 `config.yaml` 示例，说明 Clash YAML 支持范围、外部依赖（xray-core 二进制路径/版本）、资源消耗、以及安全注意事项；提供最小可用示例与排障指引（例如 “找不到 xray-core 时仅导入 socks5/http 节点并告警”）。
10. 回滚/降级策略：所有新能力通过配置开关可关闭；当 Clash 解析失败或适配层不可用时，不影响现有 `proxy_list_urls` 逻辑；必要时可仅启用 `clash_yaml` 的解析但不启用协议适配（只导入 socks5/http）。

⚠️ 风险与注意事项
- 现有架构仅支持 SOCKS5 上游：要支持 SS/V2Ray/Hysteria 等，必须引入协议适配层（库集成或外部进程），否则无法“真正加入池并可用”。
- 外部进程（xray-core）带来的复杂度：节点数量上限、资源占用、崩溃重启、跨平台兼容与 Docker 镜像体积；MVP 通过“单入站 + observatory + 配置变更重启”降低实现难度。
- 许可与分发：xray-core 为 MPL-2.0；若将其二进制打包进镜像/发布物，需要一并包含许可文本与来源说明；若未来修改/二次分发其源码，需要遵守 MPL 的文件级开源义务。
- Clash 配置存在多变体：字段命名/加密方式/插件选项不一致，需要“尽力解析 + 明确不支持项 + 失败可观测”。
- 安全与隐私：节点信息含密码/uuid；日志、admin 状态输出与错误信息需要脱敏。
- 大规模节点的配置体积：SOCKS inbound 的 `accounts`（每节点一个 user）+ routing rules（每节点一个 user→outboundTag）会导致配置 O(N)；需要设置 `max_nodes`、输出清晰告警，并在后续迭代评估 xray API 动态更新或分片实例策略。
- observatory 的误判与探测特征：HTTPing 探测不等同于真实业务流量与 TLS 严格握手；需要可配置探测 URL、并在 status 中展示 last_error/last_try_time 等信息以便排障。

📝 修订记录
- 2025-12-23：确定协议适配层选用 xray-core（MPL-2.0），并将计划中适配实现/支持范围/降级策略细化到可执行级别。
- 2025-12-23：将适配方案从“一节点一端口”升级为“单 SOCKS 入站按 user 路由 + (burst)Observatory 测活 + metrics(expvar) 拉取结果”，以适配大规模节点并保持逐节点管理能力。
- 2025-12-24：确定只跑 RELAXED（兼容优先），收敛为单代理池与单套监听端口；xray 只启动一个实例。

📎 参考
- `internal/fetcher/fetcher.go:22`
- `internal/orchestrator/updater.go:86`
- `internal/config/config.go:10`
- `internal/health/health.go:30`
- `README.md:85`
