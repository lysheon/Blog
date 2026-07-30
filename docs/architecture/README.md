# 当前架构

Blog 当前是一个通过 Docker Compose 部署的前后端分离模块化单体。API、Worker 和 Migration 来自同一个 Go 后端镜像；React SPA 由独立 Nginx 容器提供；外部流量统一经过 Proxy。

> 本文描述当前 `origin/main`。同目录的 `stage-*.md` 保留各阶段的设计和验收记录，不是独立部署版本。

## 运行拓扑

```text
                         public HTTP / HTTPS
                                  │
                                  ▼
                         ┌─────────────────┐
                         │  Nginx Proxy    │
                         │  :8080 / TLS*   │
                         └───────┬─────────┘
                      /api,/health│   │/,/assets
                                  │   ▼
                                  │ ┌──────────────┐
                                  │ │ React SPA    │
                                  │ │ frontend:80  │
                                  │ └──────────────┘
                                  ▼
                         ┌─────────────────┐
                         │ Go API :8080    │
                         │ Gin + GORM      │
                         └───┬─────┬───────┘
                             │     │
                    ┌────────┘     └──────────────┐
                    ▼                             ▼
             ┌─────────────┐               ┌─────────────┐
             │ MySQL 8.4   │               │ Redis 7.4   │
             │ data + jobs │               │ rate limits │
             └──────┬──────┘               └─────────────┘
                    │
                    ▼
             ┌─────────────┐    Embedding/Chat    ┌──────────────────┐
             │ Go Worker   │─────────────────────▶│ OpenAI-compatible│
             │ job polling │                      │ model providers  │
             └──────┬──────┘                      └──────────────────┘
                    │
                    ▼
             ┌─────────────┐
             │ Milvus 2.5  │
             │ vector data │
             └─────────────┘

* TLS 在生产可信入口终止；仓库内 Nginx 默认监听 HTTP :8080。
```

`migrate` 是一次性容器，在 API 与 Worker 启动前执行向前 migration。MySQL、Redis 和 Milvus 使用命名卷；只有 Proxy 默认向宿主机发布端口。

## 组件职责

| 组件 | 职责 | 数据性质 |
|---|---|---|
| Proxy | 同源路由、安全响应头、请求 ID 转发、SPA 与 API 分流 | 无状态 |
| Frontend | 阅读、认证、作者工作区、编辑、评论、taxonomy 和 AI 问答界面 | 无状态 |
| API | 认证、RBAC、文章/评论、健康检查、指标、AI Ask/Reindex API | 无状态进程 |
| Worker | MySQL 任务领取、重试、死信、评论处理、文章向量索引 | 无状态进程；状态在 MySQL |
| Migrate | 执行嵌入式显式 SQL migration | 一次性任务 |
| MySQL | 用户、Token、文章、评论、taxonomy、任务和 AI 文档状态 | 唯一业务事实源 |
| Redis | 分布式限流和短期协调 | 可丢失；不承载持久任务 |
| Milvus | 已发布公开文章的可重建向量索引 | 派生数据，可从 MySQL 重建 |
| AI Provider | OpenAI-compatible Embedding 与 Chat | 外部依赖，按开关启用 |

## 后端边界

```text
cmd/{api,worker,migrate}
          │
          ▼
internal/bootstrap        # 唯一依赖装配点
  ├── modules/            # auth, posts, comments, ai, operations
  ├── platform/           # MySQL, Redis, HTTP, jobs, metrics, model adapter...
  ├── domain/             # 持久化领域模型
  ├── config/             # 环境变量读取、类型与跨字段校验、*_FILE
  └── shared/apperr       # 稳定 API 错误契约
```

业务模块不自行创建基础设施连接。`bootstrap` 注入 GORM、Redis、指标和 OpenAI-compatible/Milvus 适配器。生产 schema 不使用 GORM `AutoMigrate`。

## 核心链路

### HTTP 请求

1. Proxy 生成或转发 Request ID。
2. Gin 中间件处理恢复、访问日志、CORS、请求体限制和统一错误。
3. Auth 中间件验证短期 JWT；Refresh Token 只通过 HttpOnly Cookie 轮换。
4. Handler 调用模块 Service/Repository。
5. API 返回稳定错误对象：`code`、`message`、`details`、`request_id`。

### 文章索引

1. 已发布文章变更与 `post_index` 任务在 MySQL 中持久化。
2. Worker 使用 `FOR UPDATE SKIP LOCKED` 领取任务。
3. Worker 读取当前公开版本、分块并调用 Embedding 接口。
4. 向 Milvus 幂等写入向量后更新 `ai_documents`。
5. 失败任务按预算重试，超过次数进入 dead；旧索引不会成为 MySQL 权限事实源。

### RAG 问答

1. API 对问题做长度和严格限流校验；Redis 不可用时 AI fail-closed。
2. Query Embedding 在 Milvus 召回候选。
3. MySQL 重新验证文章仍为 `published + public` 且版本一致。
4. 按文章去重、限制 chunk 数量并组装有界上下文。
5. Chat 接口生成回答，API 只返回经验证文章对应的来源。

## 依赖与降级

- **MySQL 是硬依赖**：启动连接失败则 API/Worker 不启动；运行期故障使 readiness 返回 503。
- **Redis 是博客软依赖**：连接失败时 API 可降级启动；认证/评论限流 fail-open，但必须由入口基础防护兜底。
- **Redis 是 AI 成本保护硬依赖**：AI 限流 fail-closed。
- **Milvus 和模型服务不是核心博客启动依赖**：AI 关闭或不可用不应阻断公开文章功能。
- **Milvus 不是权限源**：检索候选必须回查 MySQL。

## 架构事实源

| 事实 | 文件 |
|---|---|
| 依赖装配与模块注册 | [`backend/internal/bootstrap/app.go`](../../backend/internal/bootstrap/app.go) |
| 环境配置与校验 | [`backend/internal/config/config.go`](../../backend/internal/config/config.go) |
| 数据库 schema | [`backend/migrations/`](../../backend/migrations/) |
| 基础部署拓扑 | [`deploy/compose.yaml`](../../deploy/compose.yaml) |
| 生产 Secret overlay | [`deploy/compose.secrets.yaml`](../../deploy/compose.secrets.yaml) |
| 入口路由 | [`deploy/proxy/nginx.conf`](../../deploy/proxy/nginx.conf) |
| CI 门禁 | [`.github/workflows/ci.yml`](../../.github/workflows/ci.yml) |

## 阶段实现记录

| 阶段 | 文档 | 已交付重点 |
|---|---|---|
| 0 | [stage-0.md](stage-0.md) | 配置、日志、MySQL/Redis、Migration、健康检查、Compose |
| 1 | [stage-1.md](stage-1.md) | Auth、博客领域、限流、MySQL Worker |
| 2 | [stage-2.md](stage-2.md) | React SPA 与同源交付 |
| 3 | [stage-3.md](stage-3.md) | Embedding、分块和 Milvus 索引 |
| 4 | [stage-4.md](stage-4.md) | 受约束检索与 RAG 问答 |
| 5 | [stage-5.md](stage-5.md) | 生产门禁、可观测性和恢复设计 |
| 5.1 | [stage-5-1.md](stage-5-1.md) | 真实依赖、浏览器 E2E、Secrets、备份恢复闭环 |

进一步阅读：[仓库结构](../repository-structure.md) · [首次部署](../deployment/first-deployment.md) · [生产运维](../operations/runbook.md) · [ADR-0001](../adr/0001-modular-monolith.md)
