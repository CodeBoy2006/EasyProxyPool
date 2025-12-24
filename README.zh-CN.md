# EasyProxyPool

[English](README.md) | 中文

EasyProxyPool 是一个本地运行的 **SOCKS5 + HTTP/HTTPS（CONNECT）** 动态代理程序，会将请求轮询/随机分发到一组上游 SOCKS5 代理（代理池）中。

程序会持续从多个来源拉取代理列表、并发测活，并维护一套代理池（RELAXED 模式，兼容性优先）。

## 功能特性

- 多源代理列表拉取 + 去重
- 高并发测活 + 延迟阈值过滤
- 单套代理池，提供 SOCKS5 + HTTP 两个监听端口
- 上游选择策略（`round_robin` 或 `random`）
- 可选：基于会话 key 的粘性上游选择（仅 HTTP 代理路径）
- 请求失败自动重试（切换上游）+ 指数退避 + 临时禁用失败上游
- 可选认证：
  - HTTP：`Proxy-Authorization: Basic ...`
  - SOCKS5：用户名/密码
- 可选管理接口：`/healthz` 与 `/status`
- 结构化日志（Go `slog`）

## 快速开始

### 编译

```bash
go build -o easyproxypool ./cmd/easyproxypool
```

### 运行

```bash
./easyproxypool -config config.yaml
```

覆盖日志级别（环境变量优先生效）：

```bash
LOG_LEVEL=debug ./easyproxypool -config config.yaml
```

### 测试

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

或：

```bash
docker-compose up -d
```

## 配置说明

编辑 `config.yaml`。

常用选项：

- `proxy_list_urls`：代理源列表（每行 `ip:port`；也支持 `socks5://ip:port`）
- `sources`：支持按类型配置源（例如 `clash_yaml`）（可选，可替代 `proxy_list_urls`）
- `health_check.*`：测活超时、TLS 握手目标与阈值
- `ports.*`：本地代理监听地址
- `selection.*`：上游选择 + 重试/退避策略
- `selection.sticky.*`：基于会话 key 的粘性上游选择（可选）
- `auth.*`：开启代理认证（如果监听在非本地地址上，强烈建议开启）
- `admin.*`：管理接口 + Web 仪表盘（/ui/）+ SSE 实时日志
- `adapters.xray.*`：启用 xray-core 作为 Clash 节点协议适配层（可选，默认关闭）

### 认证

`auth.mode`：

- `disabled`：不启用认证
- `basic`：username/password 必须完全匹配
- `shared_password`：允许任意 username，仅校验 password（共享密钥；username 可用于会话/租户标识）

示例（共享密钥）：

```yaml
auth:
  mode: shared_password
  password: "shared-secret"
```

此时客户端可以使用任意 username：

```bash
curl -x http://127.0.0.1:17285 -U 'tenantA:shared-secret' https://api.ipify.org
curl -x http://127.0.0.1:17285 -U 'tenantB:shared-secret' https://api.ipify.org
```

### 基于会话 key 的“固定出口 IP”（仅 HTTP 代理路径）

如需把一个“会话”绑定到固定出口 IP（上游节点），启用 `selection.sticky`。
EasyProxyPool 会对当前存活节点集合使用 **Rendezvous（HRW）一致性哈希**，节点增删/存活变化时仅会影响少量会话映射。

会话 key 来源（优先级从高到低）：

1) `X-EasyProxyPool-Session`（当 `selection.sticky.header_override=true`）
2) `Proxy-Authorization` Basic 的 username（配合 `auth.mode: shared_password` 最适合）
3) W3C `traceparent` 的 trace-id（兜底）

可选的逐请求覆盖（由 `selection.sticky.header_override` 控制）：

- `X-EasyProxyPool-Sticky: on|off`
- `X-EasyProxyPool-Failover: soft|hard`
- `X-EasyProxyPool-Upstream: <entryKey>`（强制指定某个上游 key）

示例：

```bash
# HTTPS via CONNECT（使用 --proxy-header 把 header 发给代理）
curl -x http://127.0.0.1:17285 \
  --proxy-header 'X-EasyProxyPool-Session: s-123' \
  https://api.ipify.org

# 或：使用 traceparent 作为会话 key 的兜底来源
curl -x http://127.0.0.1:17285 \
  --proxy-header 'traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01' \
  https://api.ipify.org
```

### Clash YAML + xray-core 协议适配（可选）

如需使用 Clash 格式节点（vmess/vless/trojan/ss/socks5/http 等）且不想在 Go 中逐协议实现，
可以启用 xray 适配器，并添加 `clash_yaml` 源：

```yaml
sources:
  - type: clash_yaml
    path: "./clash.yaml"   # 或 url: "https://example.com/clash.yaml"

adapters:
  xray:
    enabled: true
    binary_path: "/usr/local/bin/xray"
    # 建议仅监听在本机回环。EasyProxyPool 会用 SOCKS username (= nodeID) 指定每条连接走哪个节点。
    socks_listen_relaxed: "127.0.0.1:17383"
    # 用于拉取 /debug/vars（observatory 的 alive/delay）
    metrics_listen_relaxed: "127.0.0.1:17387"
    fallback_to_legacy_on_error: true
```

说明：

- EasyProxyPool 只运行一个 xray 实例（RELAXED），并通过 SOCKS username (= nodeID) 指定每条连接走哪个节点。
- Observatory 采用 HTTPing 探测；可通过 `adapters.xray.observatory.*` 调整探测目标与间隔。
- 若 xray 启动失败或 metrics 不可用，程序会保留现有代理池；若开启 `fallback_to_legacy_on_error: true`，会尝试回退到 `proxy_list_urls` 的旧流程。

### 安全 / 许可提示

- 不要将本项目代理端口直接暴露到公网；至少开启认证并配合防火墙/访问控制。
- 建议将 xray 的 SOCKS/metrics 仅监听在回环地址（127.0.0.1）。
- xray-core 使用 MPL-2.0；若你在镜像/发行包中分发 xray 二进制，请同时包含其许可证与相关说明。

## 管理接口 / 仪表盘（可选）

在 `config.yaml` 中设置 `admin.enabled: true`（默认地址 `127.0.0.1:17287`）。

接口列表：

- 探活：`GET /healthz`（可配置允许免鉴权）
- 状态 JSON：`GET /status` 或 `GET /api/status`
- 构建/运行信息：`GET /api/info`
- 节点健康快照：`GET /api/nodes`
- 实时日志（SSE）：`GET /api/events/logs`
- Web 仪表盘：`GET /ui/`（当 `admin.ui_enabled: true` 时，访问 `/` 会跳转到 `/ui/`）

示例（无鉴权）：

```bash
curl http://127.0.0.1:17287/healthz
curl http://127.0.0.1:17287/status
curl http://127.0.0.1:17287/ui/
```

示例（推荐 `admin.auth.mode: shared_token`）：

```bash
curl -H 'Authorization: Bearer <token>' http://127.0.0.1:17287/api/info
curl -N 'http://127.0.0.1:17287/api/events/logs?token=<token>&since=0&level=info'
```

安全提示：不要将管理接口/仪表盘端口暴露到公网；建议仅监听回环地址，并配合鉴权与防火墙/访问控制。
降级/关闭：设置 `admin.enabled: false`（或设置 `admin.ui_enabled: false` 仅保留 API）。

## 安全提示

- 不要盲目信任“免费代理列表”，上游代理可能恶意或不稳定。
- 不要将本项目代理端口直接暴露到公网；至少开启认证并配合防火墙/访问控制。

## 许可证

MIT
