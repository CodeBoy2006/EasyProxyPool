# EasyProxyPool

[English](README.md) | 中文

EasyProxyPool 是一个本地运行的 **SOCKS5 + HTTP/HTTPS（CONNECT）** 动态代理程序，会将请求轮询/随机分发到一组上游 SOCKS5 代理（代理池）中。

程序会持续从多个来源拉取代理列表、并发测活，并维护一套代理池（RELAXED 模式，兼容性优先）。

## 功能特性

- 多源代理列表拉取 + 去重
- 高并发测活 + 延迟阈值过滤
- 单套代理池，提供 SOCKS5 + HTTP 两个监听端口
- 上游选择策略（`round_robin` 或 `random`）
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
- `ports.*`：本地四个代理监听地址
- `selection.*`：上游选择 + 重试/退避策略
- `auth.*`：开启代理认证（如果监听在非本地地址上，强烈建议开启）
- `admin.*`：管理接口开关与监听地址
- `adapters.xray.*`：启用 xray-core 作为 Clash 节点协议适配层（可选，默认关闭）

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

## 管理接口（可选）

在 `config.yaml` 中设置 `admin.enabled: true`（默认端口 `:17287`），然后：

```bash
curl http://127.0.0.1:17287/healthz
curl http://127.0.0.1:17287/status
```

## 安全提示

- 不要盲目信任“免费代理列表”，上游代理可能恶意或不稳定。
- 不要将本项目代理端口直接暴露到公网；至少开启认证并配合防火墙/访问控制。

## 许可证

MIT
