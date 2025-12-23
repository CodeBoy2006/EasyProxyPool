# EasyProxyPool

[English](README.md) | 中文

EasyProxyPool 是一个本地运行的 **SOCKS5 + HTTP/HTTPS（CONNECT）** 动态代理程序，会将请求轮询/随机分发到一组上游 SOCKS5 代理（代理池）中。

程序会持续从多个来源拉取代理列表、并发测活，并维护两套代理池：

- **STRICT（严格）**：通过“带证书校验”的 TLS 握手检测
- **RELAXED（宽松）**：通过“跳过证书校验”的 TLS 握手检测（兼容性更好）

## 功能特性

- 多源代理列表拉取 + 去重
- 高并发测活 + 延迟阈值过滤
- 两套代理池（STRICT / RELAXED），分别提供 SOCKS5 + HTTP 四个监听端口
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

或：

```bash
docker-compose up -d
```

## 配置说明

编辑 `config.yaml`。

常用选项：

- `proxy_list_urls`：代理源列表（每行 `ip:port`；也支持 `socks5://ip:port`）
- `health_check.*`：测活超时、TLS 握手目标与阈值
- `ports.*`：本地四个代理监听地址
- `selection.*`：上游选择 + 重试/退避策略
- `auth.*`：开启代理认证（如果监听在非本地地址上，强烈建议开启）
- `admin.*`：管理接口开关与监听地址

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

