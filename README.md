# simple-cloudflare-ddns

一个零第三方依赖的极简 Cloudflare IPv4 DDNS。程序按固定间隔查询公网 IPv4，仅在地址变化时更新 Cloudflare A 记录；记录不存在时会自动创建。

## Cloudflare 权限

推荐创建 API Token，并授予目标 Zone 以下权限：

- `Zone / Zone / Read`
- `Zone / DNS / Edit`

资源范围只选择需要更新的 Zone。

## JSON 配置

```bash
cp config.example.json config.json
chmod 600 config.json
```

编辑 `config.json`：

```json
{
  "api_token": "your-cloudflare-api-token",
  "domain": "home.example.com",
  "interval": "5m"
}
```

字段说明：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `api_token` | 是 | Cloudflare API Token |
| `zone` | 否 | Cloudflare Zone；省略时根据 `domain` 自动查询 |
| `domain` | 是 | 要更新的完整域名，例如 `home.example.com` |
| `interval` | 否 | 查询间隔，默认 `5m`，使用 Go duration 格式，如 `30s`、`10m`、`1h` |
| `ip_url` | 否 | 公网 IPv4 查询地址，默认 `https://cloudflare.com/cdn-cgi/trace`；也支持返回纯文本 IPv4 的地址 |
| `ttl` | 否 | 新建记录的 TTL，默认 `1`（自动），也可设置 `60` 到 `86400` |
| `proxied` | 否 | 新建记录时是否启用 Cloudflare Proxy，默认 `false` |

API Token 是唯一支持的鉴权方式。`domain` 是唯一必须提供的 DNS 字段；省略 `zone` 时，程序会通过 Cloudflare API 从完整域名向上查找最具体的可用 Zone。公网地址探测会强制使用 IPv4，并从 Cloudflare Trace 响应的 `ip=` 行读取地址。

省略 `ttl` 即使用 Cloudflare 的自动 TTL，对应 API 值 `1`。`ttl` 仅在程序创建新记录时使用；已有记录更新时只修改 IP，不会覆盖 Cloudflare 控制台中的 TTL、Proxy 等设置。

## 环境变量

配置文件中的所有字段都可以通过环境变量设置。环境变量优先级高于 JSON，因而也可以只覆盖 Token 等单个字段：

| 环境变量 | 对应 JSON 字段 | 示例 |
| --- | --- | --- |
| `CLOUDFLARE_API_TOKEN` | `api_token` | `your-cloudflare-api-token` |
| `DDNS_DOMAIN` | `domain` | `home.example.com` |
| `DDNS_ZONE` | `zone` | `example.com` |
| `DDNS_INTERVAL` | `interval` | `60s` |
| `DDNS_IP_URL` | `ip_url` | `https://cloudflare.com/cdn-cgi/trace` |
| `DDNS_TTL` | `ttl` | `1` |
| `DDNS_PROXIED` | `proxied` | `false` |

优先级为：程序默认值 < JSON 配置 < 环境变量。`DDNS_TTL` 必须是整数，其中 `1` 表示自动；`DDNS_PROXIED` 使用 `true` 或 `false`。

纯环境变量运行时不需要创建 `config.json`：

```bash
export CLOUDFLARE_API_TOKEN="your-cloudflare-api-token"
export DDNS_DOMAIN="home.example.com"
export DDNS_INTERVAL="60s"
go run . -once
```

默认的 `config.json` 不存在时，程序会继续读取环境变量。

## 运行

先执行一次，检查配置和权限：

```bash
go run . -config config.json -once
```

持续运行：

```bash
go run . -config config.json
```

编译后运行：

```bash
go build -o simple-cloudflare-ddns .
./simple-cloudflare-ddns -config config.json
```

## Docker

使用环境变量运行 GHCR 镜像：

```bash
docker run -d \
  --name simple-cloudflare-ddns \
  --restart unless-stopped \
  -e CLOUDFLARE_API_TOKEN="your-cloudflare-api-token" \
  -e DDNS_DOMAIN="home.example.com" \
  -e DDNS_INTERVAL="60s" \
  ghcr.io/mofelee/simple-cloudflare-ddns:latest
```

也可以挂载 JSON 配置：

```bash
docker run -d \
  --name simple-cloudflare-ddns \
  --restart unless-stopped \
  --user "$(id -u):$(id -g)" \
  -v "$PWD/config.json:/config.json:ro" \
  ghcr.io/mofelee/simple-cloudflare-ddns:latest \
  -config /config.json
```

镜像支持 `linux/amd64`、`linux/arm64` 和 `linux/arm/v7`。如果 GHCR Package 尚未公开，需要先登录 GHCR，或在 GitHub Packages 设置中将其可见性改为 Public。

## 自动构建

[GitHub Actions 工作流](.github/workflows/build.yml)在 Pull Request、`main` 分支推送、`v*` 标签以及手动触发时运行：

- 运行单元测试、竞态检测和 `go vet`。
- 构建 Linux `amd64`、`arm64`、`armv7`，macOS `amd64`、`arm64`，以及 Windows `amd64` 二进制。
- 每次运行将压缩后的二进制保存为 Actions Artifacts。
- 推送 `v*` 标签时创建 GitHub Release，并附加全部二进制和 `checksums.txt`。
- 构建多架构 GHCR 镜像；`main` 分支发布 `main` 和 `sha-*` 标签，`v*` 标签发布版本号和 `latest`。

创建正式版本：

```bash
git tag v1.0.0
git push origin v1.0.0
```

程序收到 `SIGINT` 或 `SIGTERM` 时会正常退出。同步失败后会从约 10 秒开始指数退避重试，并加入少量随机抖动；重试等待时间不会超过正常的 `interval`。同步成功后会恢复正常查询间隔。
