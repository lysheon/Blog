# 首次部署指南

本文用于把新的 Linux 主机从空环境部署到可验收的 Blog 实例。日常发布、回滚、备份与故障处理见[生产运维手册](../operations/runbook.md)。

## 1. 部署边界

当前仓库使用 Docker Compose 部署以下服务：

```text
public traffic → proxy → frontend / api
                           │      ├→ Redis
                           │      ├→ Milvus
                           │      └→ OpenAI-compatible Chat/Embedding
                           └──────→ MySQL ← worker
                                      ↑
                                   migrate (one shot)
```

- 只部署 `origin/main` 的明确 commit SHA；功能分支和开放 PR 不是发布基线。
- 仓库内 Proxy 默认提供 HTTP `:8080`。生产必须在可信 Ingress/负载均衡器终止 TLS，再转发到 Proxy。
- 只有 Proxy 默认发布宿主机端口。MySQL、Redis、Milvus、API 和 Worker 保持在 Compose 网络内。
- MySQL 是业务事实源；Milvus 是可重建派生索引；Redis 不保存持久任务。

## 2. 主机前置条件

- Docker Engine 与 Docker Compose v2；
- GNU Make、Git 和 `curl`；
- 足够运行 MySQL 8.4、Redis 7.4、Milvus 2.5、两个应用镜像和 Proxy 的 CPU/内存/磁盘；
- 受控的持久化磁盘、外部备份目录和日志/指标采集；
- 指向 TLS 入口的域名；
- 若启用 AI，准备 OpenAI-compatible Embedding/Chat Endpoint、模型名、API key 和固定 Embedding 维度。

确认版本：

```bash
docker version
docker compose version
make --version
git --version
```

## 3. 获取部署基线

```bash
git clone https://github.com/lysheon/Blog.git
cd Blog
git fetch origin --prune --tags
git switch main
git pull --ff-only origin main
export DEPLOY_SHA="$(git rev-parse HEAD)"
git status --short --branch
```

记录 `DEPLOY_SHA`。生产镜像 tag 应使用该 SHA 或发布版本号，不应复用可变 `latest`。

## 4. 准备非敏感配置

```bash
cp .env.example .env
chmod 0600 .env
```

至少检查并修改：

```dotenv
APP_ENV=production
PROXY_BIND_ADDRESS=127.0.0.1
PROXY_PORT=8080
BLOG_BACKEND_IMAGE_TAG=<DEPLOY_SHA-or-release>
BLOG_FRONTEND_IMAGE_TAG=<DEPLOY_SHA-or-release>
AUTH_COOKIE_SECURE=true
HTTP_TRUSTED_PROXIES=<实际可信入口 CIDR>
REDIS_KEY_PREFIX=blog:production:v1:
LOG_LEVEL=info
LOG_FORMAT=json
```

- TLS 入口与 Compose 同机时，推荐把 Proxy 绑定到 `127.0.0.1`；确需直接监听外网时，必须另行提供 TLS 和入口防护。
- 同源部署可将 `CORS_ALLOWED_ORIGINS` 留空。跨源时只列出完整 HTTPS Origin，启用 credentials 时禁止 `*`。
- `HTTP_TRUSTED_PROXIES` 必须匹配实际入口网络，不能为了省事信任任意地址。
- `.env` 中的 `MYSQL_DATABASE`、`MYSQL_USER`、模型 URL/名称、维度和限额可保留为普通配置；真实 Secret 使用下一节文件注入。
- 初次上线建议先保持 `AI_INDEXING_ENABLED=false`、`AI_RAG_ENABLED=false`，完成核心博客验收后再启用 AI。

## 5. 准备生产 Secret 文件

Secret 目录必须位于仓库外。以下 overlay 要求所有文件存在；即使暂时关闭 AI，也应为 AI/Milvus Secret 放置非生产占位文本，之后启用能力时再轮换为真实值。

```bash
export SECRETS_DIR=/srv/blog-secrets
sudo install -d -m 0700 -o "$(id -u)" -g "$(id -g)" "$SECRETS_DIR"
umask 077
```

创建文件：

```text
mysql_password          # Blog MySQL 用户密码
mysql_root_password     # MySQL root 初始化密码
mysql_dsn               # 完整 go-sql-driver/mysql DSN
redis_password          # Redis requirepass
jwt_secret              # 至少 32 bytes 的随机值
ai_chat_api_key         # AI 关闭时也需非空占位
ai_embedding_api_key    # AI 关闭时也需非空占位
milvus_password         # 当前本地 Milvus 无鉴权时使用非空占位
```

生成随机 Secret 的示例：

```bash
openssl rand -base64 48 > "$SECRETS_DIR/jwt_secret"
openssl rand -base64 36 > "$SECRETS_DIR/mysql_password"
openssl rand -base64 36 > "$SECRETS_DIR/mysql_root_password"
openssl rand -base64 36 > "$SECRETS_DIR/redis_password"
printf '%s\n' disabled-until-ai-is-configured > "$SECRETS_DIR/ai_chat_api_key"
printf '%s\n' disabled-until-ai-is-configured > "$SECRETS_DIR/ai_embedding_api_key"
printf '%s\n' unused-for-local-standalone > "$SECRETS_DIR/milvus_password"
chmod 0600 "$SECRETS_DIR"/*
```

`mysql_dsn` 必须与 `.env` 中的 `MYSQL_USER`、`MYSQL_DATABASE` 及 `mysql_password` 一致，并遵循 `go-sql-driver/mysql` DSN 转义规则：

```text
<user>:<encoded-password>@tcp(mysql:3306)/<database>?charset=utf8mb4&parseTime=true&loc=UTC
```

不要把未经处理、含 DSN 保留字符的密码直接拼接。生成后确认权限，不要打印文件内容：

```bash
find "$SECRETS_DIR" -maxdepth 1 -type f -printf '%m %f\n'
```

## 6. 验证配置并启动

基础 `.env` 验证：

```bash
make compose-config
```

生产 Secret overlay 验证：

```bash
SECRETS_DIR="$SECRETS_DIR" make compose-secrets-config
```

构建并启动：

```bash
SECRETS_DIR="$SECRETS_DIR" \
  docker compose --env-file .env \
  -f deploy/compose.yaml \
  -f deploy/compose.secrets.yaml \
  up -d --build
```

启动依赖顺序为 MySQL 健康 → 一次性 Migration 成功 → API/Worker → Frontend → Proxy。查看状态与日志：

```bash
SECRETS_DIR="$SECRETS_DIR" \
  docker compose --env-file .env \
  -f deploy/compose.yaml \
  -f deploy/compose.secrets.yaml \
  ps

SECRETS_DIR="$SECRETS_DIR" \
  docker compose --env-file .env \
  -f deploy/compose.yaml \
  -f deploy/compose.secrets.yaml \
  logs --no-color migrate api worker proxy
```

Migration 必须以退出码 0 完成；不要把一次性 `migrate` 容器显示为 exited 误判为故障。

## 7. 验收核心博客

在主机本地或经 TLS 域名检查：

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
curl -fsS http://127.0.0.1:8080/api/v1/posts
curl -I http://127.0.0.1:8080/
```

随后通过浏览器完成：

1. 打开首页和文章详情 deep link；
2. 注册、登录、刷新页面并确认会话恢复；
3. 创建草稿、发布文章；
4. 添加评论并等待 Worker 处理；
5. 检查 API/Worker 日志、heartbeat、队列统计和 `/metrics`；
6. 确认外部只暴露 TLS 入口，数据库和中间件端口不可公网访问。

## 8. 初始化首个管理员

开放注册只创建 `user` 角色，项目当前没有自动生成首个管理员的引导接口。先通过页面注册指定账户，再在受控维护窗口提升其角色：

```bash
export ADMIN_EMAIL='admin@example.com' # 必须是已注册的小写邮箱
case "$ADMIN_EMAIL" in
  ''|*[!a-z0-9._%+@-]*) printf 'ADMIN_EMAIL contains unsupported characters\n' >&2; exit 1 ;;
esac

SECRETS_DIR="$SECRETS_DIR" \
  docker compose --env-file .env \
  -f deploy/compose.yaml \
  -f deploy/compose.secrets.yaml \
  exec -T -e ADMIN_EMAIL="$ADMIN_EMAIL" mysql sh -eu -c '
    export MYSQL_PWD="$(cat "$MYSQL_PASSWORD_FILE")"
    exec mysql --protocol=TCP -h 127.0.0.1 \
      -u "$MYSQL_USER" "$MYSQL_DATABASE" \
      -e "UPDATE users SET role = '\''admin'\'', updated_at = UTC_TIMESTAMP(6) WHERE email_normalized = '\''$ADMIN_EMAIL'\'' AND deleted_at IS NULL; SELECT ROW_COUNT() AS promoted;"
  '
```

要求 `promoted` 为 `1`。如果为 `0`，停止并检查规范化邮箱；不要批量修改角色。完成后注销并重新登录，让新 JWT 携带当前角色，再验证 taxonomy 管理和 `/api/v1/ai/reindex` 权限。

## 9. 启用 AI

核心博客通过后再修改普通配置：

```dotenv
AI_INDEXING_ENABLED=true
AI_RAG_ENABLED=true
AI_EMBEDDING_BASE_URL=https://provider.example/v1
AI_EMBEDDING_MODEL=<embedding-model>
AI_EMBEDDING_DIMENSIONS=<provider-fixed-dimension>
AI_CHAT_BASE_URL=https://provider.example/v1
AI_CHAT_MODEL=<chat-model>
```

将两个 API key Secret 文件替换为真实值，保持 `0600`，然后重建 API/Worker：

```bash
SECRETS_DIR="$SECRETS_DIR" \
  docker compose --env-file .env \
  -f deploy/compose.yaml \
  -f deploy/compose.secrets.yaml \
  up -d --build api worker
```

由管理员调用 Reindex，观察 `post_index`、`ai_documents`、Embedding/Milvus 指标，再测试 `/api/v1/ai/ask` 的答案与来源。Embedding 模型或维度变化不能直接复用不兼容 Collection。

## 10. 上线前最终门禁

- CI 的 Quality、MySQL integration、Real API browser E2E、Milvus and AI integration 均通过；
- `make verify` 与 `make verify-integration` 在发布基线通过；
- TLS、Secure Cookie、CORS、trusted proxies 和入口限流已验证；
- Secret 未进入 Git、Compose 输出、日志或镜像；
- 已创建 MySQL 备份，并在隔离目标完成 `make verify-backup`；
- 已记录镜像 tag、commit SHA、migration version、RPO/RTO 和回滚版本；
- 已配置 API/Worker 指标抓取及最小告警。

完成后按[生产运维手册](../operations/runbook.md)执行后续发布和演练。生产禁止自动执行 `migrate down`，也禁止未经确认运行 `down --volumes`。
