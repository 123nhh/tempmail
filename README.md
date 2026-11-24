# TempMail

一个高并发自托管临时邮件平台，基于 **PostgreSQL + PgBouncer + Redis + Go + Postfix (LMTP) + Vue 3** 构建。支持多域名与多级通配子域名、自助提交域名与 MX 自动验证、SSE 实时收件推送、验证码自动识别、完整 API Key 编程接口、OAuth 登录及现代化 Web 管理后台。

- 部署教程参考：https://linux.do/t/topic/1667604

---

## 核心特性

| 模块 | 说明 |
|------|------|
| **临时邮箱管理** | 按需创建随机或自定义前缀邮箱，支持服务端分页、搜索与排序；可配置有效期（默认 30 分钟）、每用户活跃上限、默认域名及自动清理 |
| **一键续期与提醒** | 到期前自动弹出提醒并可一键续期；支持后台配置单次续期时长与单个邮箱最长存活上限（默认 24 小时） |
| **实时收件（SSE）** | 收件箱通过 SSE 实时推送 `ready` / `email` / `mailbox` / `expired` 事件，Nginx 独立关闭缓冲并配置 300s 读超时；断线自动退避重连并回退轮询 |
| **完整邮件解码与安全阅读** | Postfix 常驻 Go LMTP 守护进程支持 RFC 2047 邮件头、Base64 / Quoted-Printable、UTF-8 / GBK / GB18030 / GB2312 / HZ-GB-2312 / Big5 / ISO-8859-1 / Windows-1252 解码；HTML 正文通过受限 `iframe` + CSP 沙箱隔离，默认拦截远程追踪图片 |
| **验证码自动提取** | 自动从主题与正文中识别中英文验证码，一键复制 |
| **多域名池与多级子域** | 支持普通单域名与 `*.example.com` 多级通配子域名；自动独立检测单域/通配 MX 能力，后台每 30 秒验证待生效域名，每 6 小时健康巡检 |
| **账号与认证** | 支持独立 API Key（`Authorization: Bearer`、`X-API-Key`、`?api_key=` 三种方式均可）、注册/创建账户附带可选联系邮箱、Linux DO Connect 与 GitHub OAuth 登录 |
| **高并发与安全防护** | PgBouncer 事务连接池 + 内部单封投递微批 + 内存聚合计数；Redis Lua 原子固定窗口限流（账户级 + 公开入口 IP 防刷）；`/internal/*` 内部密钥鉴权 + 默认仅监听 `127.0.0.1` + 12 MiB 请求体上限 |
| **Web 控制台与文档** | Vue 3 + Vite 单页应用，内置数据看板、浅色/深色/跟随系统主题与交互式 API 文档（自动填入 Base URL 与当前 Key，提供 cURL / Python / JavaScript 示例） |
| **开箱即用镜像** | GitHub Actions 自动构建并推送 `tempmail-api`、`tempmail-frontend`、`tempmail-postfix` 三个 GHCR 镜像，低配服务器无需本地编译 |

---

## 架构概览

```
外部发件方 ──► Postfix (:25) ──(Unix LMTP)──► mail-receiver ──(HTTP + X-Internal-Key)──┐
                                                                                        ▼
用户浏览器 / API 客户端 ──► Nginx (:8888) ──(/api, /public, SSE)──► Go API (:8080) ──► PgBouncer (:6432) ──► PostgreSQL 16
                                                                         │
                                                                         └──► Redis 8（Lua 限流 & SSE Pub/Sub）
```

---

## 快速启动

### 前置条件

- Docker 20.10+ 与 Docker Compose v2+
- 拥有公网 IP 且放行 `25` 端口（用于外部 MX 收信）和 Web 端口（默认 `8888`）

### 1. 克隆仓库并准备环境变量

```bash
git clone https://github.com/123nhh/tempmail.git
cd tempmail
cp .env.example .env
```

编辑 `.env`，至少修改以下必填项：

- `POSTGRES_PASSWORD` 与 `API_DB_DSN` 中的数据库密码（两者保持一致）
- `REDIS_PASSWORD` 与 `API_REDIS_PASSWORD`（两者保持一致）
- `INTERNAL_API_KEY`（建议填入高强度随机字符串，保护 `postfix` ↔ `api` 内部通信）
- `SMTP_SERVER_IP`（服务器公网 IP）与 `SMTP_HOSTNAME`（如 `mail.yourdomain.com`）

### 2. 启动服务

**方式 A：直接拉取预构建 GHCR 镜像（推荐，无需本地构建）**

```bash
docker compose pull
docker compose up -d
```

**方式 B：本地源码构建启动**

```bash
docker compose up -d --build
```

启动后将运行 6 个容器：`postgres`、`pgbouncer`、`redis`、`api`、`frontend`（Nginx）、`postfix`，核心服务均内置 Docker `healthcheck`。

### 3. 获取管理员 API Key

为避免密钥泄漏到日志系统，管理员 API Key **不会打印在容器日志中**，而是以 `0600` 权限写入宿主机 `data/admin.key`：

```bash
cat data/admin.key
# ADMIN_API_KEY=tm_admin_<随机密钥>
```

### 4. 访问 Web 控制台

打开 `http://<服务器IP>:8888`（端口由 `FRONTEND_HOST_PORT` 控制），使用上方管理员 API Key 登录即可配置域名与系统参数。

> 生产环境建议在前端再加一层 HTTPS 反向代理（如 Caddy / Nginx / Cloudflare），并透传 `X-Forwarded-Proto` 与 `X-Forwarded-Host`。

### 从旧版本升级

```bash
git pull
docker compose pull
docker compose up -d
# 若本地修改过代码，改为：docker compose up -d --build
```

- 新版新增的 `accounts.email` 字段会在 API 启动时自动幂等补齐（`EnsureSchema`），无需手动改表。
- 若从很早期的 v1–v10 数据库升级，请参考下方「数据库迁移」按顺序补齐历史脚本。

---

## 环境变量说明

完整模板见 [`.env.example`](./.env.example)。

### 基础与安全配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | `tempmail` / `tempmail` / *(必填)* | PostgreSQL 数据库凭证 |
| `REDIS_PASSWORD` / `API_REDIS_PASSWORD` | *(必填)* | Redis 密码（两项保持一致） |
| `INTERNAL_API_KEY` | *(推荐填写)* | `postfix` 与 `api` 之间 `/internal/*` 接口的共享密钥（`X-Internal-Key`）；留空时仅放行 Docker 内网与回环直连 IP |
| `API_HOST_BIND` | `127.0.0.1` | API 端口在宿主机的绑定地址；默认仅本机监听，防止外部直连 `/internal/*` |
| `API_HOST_PORT` | `8080` | API 宿主机映射端口（Web 流量已由 Nginx 反代，生产环境通常无需对外暴露） |
| `FRONTEND_HOST_PORT` | `8888` | Web 控制台宿主机对外端口（也可写成 `127.0.0.1:8888` 交由前置反代转发） |
| `SMTP_HOST_PORT` | `25` | SMTP 收信端口（公网收信必须为 `25`） |
| `SMTP_SERVER_IP` | *(必填)* | 服务器公网 IP，用于 MX 检测与 SPF 记录提示（管理后台设置优先） |
| `SMTP_HOSTNAME` | *(推荐填写)* | 邮件服务器主机名（如 `mail.yourdomain.com`），设置后添加域名只需一条 MX 记录 |
| `ADMIN_KEY_FILE` | `/data/admin.key` | 管理员 API Key 在容器内的落盘路径（对应宿主机 `./data/admin.key`） |
| `DOCKER_IMAGE_NAMESPACE` / `DOCKER_IMAGE_TAG` | `123nhh` / `latest` | GHCR 镜像命名空间与标签 |

### API、限流与连接池调优

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `API_PORT` | `8080` | Go API 容器内监听端口 |
| `API_DB_DSN` | `postgres://...@pgbouncer:6432/tempmail?sslmode=disable` | API 数据库连接串（经 PgBouncer） |
| `API_DB_MAX_CONNS` / `API_DB_MIN_CONNS` | `128` / `16` | API 到 PgBouncer 的连接池大小 |
| `API_DELIVERY_BATCH_ENABLED` | `true` | 是否开启并发单封投递内部微批（不改变 HTTP 接口语义） |
| `API_DELIVERY_BATCH_MAX` / `API_DELIVERY_BATCH_WAIT` | `64` / `1ms` | 微批上限（1–256）与最长攒批等待（100µs–100ms） |
| `API_RATE_LIMIT` / `API_RATE_WINDOW` | `5000` / `60` | Redis Lua 固定窗口限流：每账户每窗口最大请求数与窗口秒数 |
| `PGBOUNCER_MAX_CLIENT_CONN` | `2000` | PgBouncer 最大客户端连接数 |
| `PGBOUNCER_DEFAULT_POOL_SIZE` / `MIN` / `RESERVE` | `64` / `16` / `16` | PgBouncer 到 PostgreSQL 的连接池参数 |
| `POSTFIX_DEFAULT_PROCESS_LIMIT` / `SMTPD_PROCESS_LIMIT` | `500` / `200` | Postfix 默认进程上限与入站 `smtpd` 独立上限 |
| `POSTFIX_LMTP_DESTINATION_CONCURRENCY_LIMIT` | `128` | Postfix → LMTP daemon 并发度 |
| `LMTP_HTTP_MAX_IDLE_CONNS` / `PER_HOST` / `MAX_CONNS_PER_HOST` | `512` / `256` / `512` | LMTP daemon 到 API 的 HTTP 连接池参数 |

### OAuth 配置（可选，亦可在管理后台动态填写）

| 变量 | 说明 |
|------|------|
| `LINUXDO_CLIENT_ID` / `LINUXDO_CLIENT_SECRET` / `LINUXDO_REDIRECT_URL` | Linux DO Connect 应用配置，回调地址示例：`https://your-domain.com/public/auth/linuxdo/callback` |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` / `GITHUB_REDIRECT_URL` | GitHub OAuth App 配置，回调地址示例：`https://your-domain.com/public/auth/github/callback` |

> 系统动态设置（`mailbox_ttl_minutes`、`mailbox_extend_minutes`、`mailbox_max_lifetime_minutes`、`max_mailboxes_per_user`、`default_domain`、`site_title`、`site_logo_url`、登录/注册开关等）均可在管理后台「系统设置」页实时修改并生效。

---

## 添加邮件域名与 DNS 配置

任意已登录用户均可自助提交域名；管理员也可以在后台直接添加或强制导入。

### 推荐配置（已设置 `SMTP_HOSTNAME`，例如 `mail.yourdomain.com`）

确保 `mail.yourdomain.com` 已有指向服务器公网 IP 的 `A` 记录，然后为业务域名添加：

```text
# 仅单域名邮箱（user@example.com）
MX   @   mail.yourdomain.com   优先级 10
TXT  @   v=spf1 ip4:<服务器IP> ~all

# 如需支持多级通配子域邮箱（user@a.b.c.example.com），额外添加：
MX   *   mail.yourdomain.com   优先级 10
```

> 若未配置 `SMTP_HOSTNAME`，还需为每个域名单独添加 `A` 记录 `mail -> <服务器公网IP>` 并把 MX 指向 `mail.example.com`。

### 创建邮箱的域名与模式选择规则

- **显式指定 `mode`**（`single` 或 `multi`）：按指定模式选取支持对应 MX 能力的活跃域名。
- **指定了 `domain` 或 `domain_id` 但未传 `mode`**：优先使用该域名的单域名能力，仅支持通配时自动使用 `multi`。
- **未指定域名且未传 `mode`**：优先尝试后台配置的 `default_domain`；否则在单域名与多级子域之间 50/50 随机选择，并在缺少对应能力域名时自动回退到可用类型。

---

## API 使用指南

所有 `/api/*` 接口均需认证，支持以下**三种方式**（长期保持兼容，只增不减）：

```text
Authorization: Bearer tm_xxxxxxxxxxxx
X-API-Key: tm_xxxxxxxxxxxx
?api_key=tm_xxxxxxxxxxxx
```

错误响应统一返回 `{"error": "..."}`。登录后在 Web 控制台「API 文档」页可查看全部接口参数、错误码与一键复制的 cURL / Python / JavaScript 代码。

### 常用接口示例

```bash
BASE="http://<服务器IP>:8888"
KEY="your_api_key"

# 1. 公开注册（需后台开启注册；email 为可选联系邮箱）
curl -X POST "$BASE/public/register" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","email":"alice@example.org"}'

# 2. 获取公开站点配置 / 平台统计
curl "$BASE/public/settings"
curl "$BASE/public/stats"

# 3. 获取当前账户信息 & 可用域名
curl "$BASE/api/me" -H "Authorization: Bearer $KEY"
curl "$BASE/api/domains" -H "Authorization: Bearer $KEY"

# 4. 创建临时邮箱（字段全可选）
curl -X POST "$BASE/api/mailboxes" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"mode":"single","address":"mytest","domain":"example.com"}'

# 5. 邮箱列表（支持服务端分页、关键词搜索 q、排序 sort=expiry|new）
curl "$BASE/api/mailboxes?page=1&size=20&q=mytest&sort=expiry" \
  -H "Authorization: Bearer $KEY"

# 6. 查询单个活跃邮箱详情 / 续期 / 删除
curl "$BASE/api/mailboxes/<mailbox-id>" -H "Authorization: Bearer $KEY"
curl -X POST "$BASE/api/mailboxes/<mailbox-id>/extend" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"minutes":30}'
curl -X DELETE "$BASE/api/mailboxes/<mailbox-id>" -H "Authorization: Bearer $KEY"

# 7. 跨邮箱最近邮件 / 单邮箱邮件列表 / 读取完整邮件
curl "$BASE/api/emails/recent?limit=8" -H "Authorization: Bearer $KEY"
curl "$BASE/api/mailboxes/<mailbox-id>/emails" -H "Authorization: Bearer $KEY"
curl "$BASE/api/mailboxes/<mailbox-id>/emails/<email-id>" -H "Authorization: Bearer $KEY"

# 8. SSE 实时监听新邮件（事件：ready / email / mailbox / expired）
curl -N "$BASE/api/mailboxes/<mailbox-id>/events" -H "Authorization: Bearer $KEY"

# 9. 自助提交域名做 MX 自动验证 / 查询验证状态
curl -X POST "$BASE/api/domains/submit" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com"}'
curl "$BASE/api/domains/<domain-id>/status" -H "Authorization: Bearer $KEY"
```

### 限流响应头

```http
X-RateLimit-Limit: 5000
X-RateLimit-Remaining: 4999
X-RateLimit-Reset: 1735000000
Retry-After: 42        # 仅在触发 429 时返回
```

---

## 吞吐压测

仓库内置两个基于现有真实链路的压测工具（位于 `api/cmd/`）：

```bash
cd api

# 1. 直接压测 API → PgBouncer → PostgreSQL（配置了 INTERNAL_API_KEY 时传入 -internal-key）
go run ./cmd/deliverbench \
  -url http://127.0.0.1:8080 \
  -internal-key "$INTERNAL_API_KEY" \
  -recipient mailbox@example.com \
  -requests 30000 -concurrency 128 -raw-bytes 1024

# 2. 端到端压测 SMTP → Postfix → LMTP → API → PostgreSQL
go run ./cmd/smtpbench \
  -addr 127.0.0.1:25 \
  -recipient mailbox@example.com \
  -messages 10000 -concurrency 200 -messages-per-connection 10 \
  -database-dsn "$BENCH_DATABASE_DSN"
```

---

## 数据库迁移

新部署直接由 PostgreSQL 自动执行 [`sql/init.sql`](./sql/init.sql)。老库升级时，`accounts.email` 会由 API 启动时自动幂等添加；若跳过较早版本，按序执行对应历史迁移脚本即可：

| 脚本 | 变更内容 |
|------|----------|
| `sql/init.sql` | 全量初始化（新库自动执行） |
| `sql/migrate_v2.sql` | 邮箱 `expires_at` 字段 |
| `sql/migrate_v3.sql` | 域名 `status`、`mx_checked_at` 与系统配置项（含 `smtp_hostname`） |
| `sql/migrate_v4.sql` | Linux DO Connect 登录与登录方式开关 |
| `sql/migrate_v5.sql` | 平台累计收件统计 |
| `sql/migrate_v6.sql` | 账户收件统计 |
| `sql/migrate_v7.sql` | 自定义站点 Logo |
| `sql/migrate_v8.sql` | 多级/通配域名支持 |
| `sql/migrate_v9.sql` | GitHub OAuth 登录 |
| `sql/migrate_v10.sql` | 独立跟踪单域名与通配子域名能力 |
| `sql/migrate_v11.sql` | 邮箱累计收件数扩展为 `BIGINT` |

执行示例：

```bash
docker exec -i $(docker compose ps -q postgres) \
  psql -U tempmail -d tempmail < sql/migrate_v11.sql
```

---

## 本地开发与项目结构

```text
tempmail/
├── api/                  # Go API 服务（Gin + pgx/v5 + go-redis）
│   ├── main.go           # 路由、内部鉴权、限流、后台任务与优雅停机
│   ├── config/           # 环境变量配置加载
│   ├── handler/          # HTTP 接口处理器与单元测试
│   ├── middleware/       # API Key 认证与 Redis Lua 固定窗口限流
│   ├── realtime/         # Redis Pub/Sub + 有界异步 SSE Hub
│   ├── model/            # 数据模型与请求/响应结构
│   ├── store/            # PostgreSQL 存取、微批投递与统计聚合
│   └── cmd/              # deliverbench / smtpbench 压测工具
├── frontend/             # Vue 3 + Vite + Pinia 前端
│   ├── src/              # 页面、组件、API 客户端与内置文档数据源
│   ├── test/             # 前端单元测试（node --test）
│   └── Dockerfile        # 多阶段构建（Node 22 → Nginx Alpine）
├── postfix/              # Postfix 配置、入口脚本与 Go LMTP 守护进程（支持中文编码）
├── nginx/                # Nginx 反向代理与 SSE 专用配置
├── pgbouncer/            # PgBouncer 事务模式连接池配置
├── sql/                  # 初始化 DDL 与历史迁移脚本
├── .github/workflows/    # GitHub Actions 多镜像 GHCR 自动构建
├── docker-compose.yml    # 完整编排（含 healthcheck 与安全端口绑定）
└── .env.example          # 环境变量模板
```

### 本地开发与测试命令

```bash
# 后端测试
cd api && go vet ./... && go test -race ./...

# Postfix LMTP 守护进程测试
cd postfix && go vet ./... && go test -race ./...

# 前端开发、测试与构建（Node.js 20.19+）
cd frontend
npm ci
npm run dev          # http://localhost:5173（/api 与 /public 代理到 localhost:8080）
node --test test/otp.test.js
npm run build
```

---

## 后台任务

| 任务 | 周期 | 职责 |
|------|------|------|
| 邮箱过期清理 | 每 1 分钟 | 删除 `expires_at` 已过期的邮箱及级联邮件 |
| 统计计数落盘 | 每 1 秒 | 将内存聚合的收件计数批量写回 PostgreSQL |
| 待验证域名 MX 检测 | 每 30 秒 | 轮询 `status='pending'` 域名，通过即自动激活 |
| 活跃域名健康巡检 | 每 6 小时 | 重检已激活域名，MX 失效自动暂停（`disabled`） |
| Admin Key 落盘 | 启动后 1 秒 | 写入 `ADMIN_KEY_FILE`（`0600` 权限，不打印至日志） |

---

## 许可证

MIT
