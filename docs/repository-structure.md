# 仓库结构

本文说明当前源码、部署文件、测试入口和远端 Git 分支的职责。部署命令见[首次部署指南](deployment/first-deployment.md)，运行系统边界见[当前架构](architecture/README.md)。

## 顶层结构

```text
Blog/
├── backend/                       # Go 模块：API、Worker、Migration
├── frontend/                      # React SPA、单元测试、Playwright E2E
├── deploy/                        # Compose 拓扑、overlays、Proxy
├── docs/                          # 部署、运维、当前架构、阶段记录、ADR
├── scripts/                       # 安全、测试、备份恢复自动化
├── .github/workflows/ci.yml       # GitHub Actions 质量门禁
├── .env.example                   # 无害配置模板
├── Makefile                       # 本地和 CI 的统一命令入口
├── README.md                      # 英文项目入口
└── README.zh-CN.md                # 中文项目入口
```

生成目录如 `bin/`、`frontend/dist/`、`node_modules/`、测试报告、备份、Secret 和 `.env` 不进入 Git。

## 后端

```text
backend/
├── cmd/
│   ├── api/                       # HTTP API 入口
│   ├── worker/                    # 后台任务处理入口
│   └── migrate/                   # 显式 SQL Migration CLI
├── internal/
│   ├── bootstrap/                 # Composition root、模块注册、生命周期
│   ├── config/                    # Env、*_FILE、类型和跨字段校验
│   ├── domain/                    # GORM 持久化领域模型
│   ├── modules/
│   │   ├── auth/                  # 注册、登录、JWT、Refresh rotation、RBAC
│   │   ├── posts/                 # 文章、分类、标签、作者工作区、索引任务
│   │   ├── comments/              # 评论、回复、权限和 moderation 入队
│   │   ├── ai/                    # 分块、索引、Milvus REST、RAG
│   │   └── operations/            # Health 与 Metrics 路由
│   ├── platform/
│   │   ├── cache/                 # Redis 客户端与 key builder
│   │   ├── database/              # MySQL/GORM 连接池
│   │   ├── httpserver/            # Gin、中间件和 HTTP 生命周期
│   │   ├── ids/                   # ULID
│   │   ├── jobs/                  # MySQL SKIP LOCKED 队列
│   │   ├── markdown/              # Markdown 渲染和 HTML 清洗
│   │   ├── migrations/            # golang-migrate 适配
│   │   ├── observability/         # 结构化日志与 Prometheus 指标
│   │   ├── openaicompat/          # Chat/Embedding HTTP 适配器
│   │   └── ratelimit/             # Redis 限流及降级策略
│   └── shared/apperr/             # 稳定 API 错误对象
├── migrations/                    # 0001 core、0002 AI；通过 embed.go 嵌入
├── Dockerfile                     # 构建 api/worker/migrate 同镜像
├── docker-entrypoint.sh           # 按 command 选择二进制
├── go.mod / go.sum                # Go 依赖锁定
└── .dockerignore                  # 后端构建上下文过滤
```

只有 `bootstrap` 同时接触配置、平台层与业务模块。生产数据库结构以 `backend/migrations/*.sql` 为准，不使用 `AutoMigrate`。

## 前端

```text
frontend/
├── src/
│   ├── main.tsx / App.tsx         # SPA 启动和路由
│   ├── api.ts                     # 同源 API client 与 refresh single-flight
│   ├── auth.tsx                   # 内存 Access Token 和会话恢复
│   ├── layout.tsx                 # 页面框架与导航
│   ├── components.tsx             # 共享 UI
│   └── pages/
│       ├── HomePage.tsx           # 文章列表
│       ├── PostPage.tsx           # 阅读与评论
│       ├── AuthPage.tsx           # 注册/登录
│       ├── EditorPage.tsx         # 写作和编辑
│       ├── MyPostsPage.tsx        # 作者工作区
│       ├── TaxonomyPage.tsx       # 管理员分类/标签
│       └── AskPage.tsx            # RAG 问答
├── e2e/
│   ├── smoke.spec.ts              # Mock API 浏览器 Smoke
│   └── real-api.spec.ts           # 真实 API/Worker 浏览器旅程
├── public/                        # 静态资源
├── Dockerfile / nginx.conf        # Production SPA 镜像和 fallback
├── playwright.config.ts           # E2E 套件选择
├── vite.config.ts                 # Vite/Vitest 与开发代理
└── package.json / package-lock.json
```

前端 Access Token 只保存在内存；Refresh Token 是浏览器管理的 HttpOnly Cookie。

## 部署文件

| 路径 | 职责 |
|---|---|
| `deploy/compose.yaml` | 基础全栈：Proxy、Frontend、API、Worker、Migrate、MySQL、Redis、Milvus |
| `deploy/compose.dev.yaml` | 显式选择时仅向 loopback 暴露 API/MySQL/Redis 调试端口 |
| `deploy/compose.integration.yaml` | 临时真实 MySQL/Redis/Milvus 集成测试依赖 |
| `deploy/compose.secrets.yaml` | 生产 `*_FILE` 和 Docker Compose Secret overlay |
| `deploy/proxy/nginx.conf` | `/api`、`/health` 与 SPA/静态资源同源路由 |

后端和前端分别是 Docker build context，因此只有它们各自的 `.dockerignore` 生效；仓库根目录不需要 `.dockerignore`。

## 文档结构

```text
docs/
├── README.md                       # 文档入口与阅读路径
├── deployment/first-deployment.md # 新环境上线
├── operations/runbook.md          # 发布、回滚、恢复、故障处理
├── architecture/
│   ├── README.md                   # 当前架构
│   └── stage-*.md                  # Stage 0–5.1 历史交付记录
├── adr/                            # 架构决策记录
└── repository-structure.md         # 本文
```

`stage-*.md` 是阶段快照，可能描述当时尚未实现的边界；当前部署事实以 `deployment/`、`operations/`、`architecture/README.md` 和代码为准。

## 自动化与 CI

```text
scripts/
├── operations/                    # MySQL backup/restore/verify
├── security/                      # Secret、隐私文件和权限门禁
└── testing/                       # 真实 API 浏览器 E2E 启动器
```

`.github/workflows/ci.yml` 当前包含：

1. **Quality**：隐私、格式、vet、Go test/build、前端 lint/test/build、Playwright Smoke；
2. **MySQL integration**：真实 MySQL/Redis 基础集成；
3. **Real API browser E2E**：API、Worker、MySQL、Redis 和浏览器完整旅程；
4. **Milvus and AI integration**：真实 Milvus 与 AI 索引/检索边界。

本地统一从 Makefile 运行：

```bash
make check
make verify
make verify-integration
make e2e-real-api
```

## 远端分支与部署基线

- `origin/main` 是唯一部署基线和默认分支。
- 功能、修复、文档和测试分支只能通过 PR 进入 `main`，不能直接作为生产版本。
- 发布记录必须固定完整 commit SHA 与不可变 backend/frontend 镜像 tag。
- 已合并且没有开放 PR 或独有提交的远端分支应删除，避免把历史分支误认成仍维护的版本。
- 开放 PR 对应分支必须保留；是否冗余不能只看分支名，应同时检查 PR 状态、祖先关系和 `git cherry` 的补丁等价性。

获取实时远端结构：

```bash
git fetch origin --prune --tags
git remote -v
git branch -r --sort=refname
git log --oneline --decorate --graph --all -20
git branch -r --merged origin/main
git branch -r --no-merged origin/main
```

本地 `main` 使用快进同步：

```bash
git switch main
git pull --ff-only origin main
```

如果工作区存在未提交变更，应先切到对应工作分支，不要通过 reset/checkout 覆盖。远端删除、历史重写、强制推送和生产发布都需要单独确认。

## 事实源优先级

| 问题 | 事实源 |
|---|---|
| 当前远端发布代码 | `origin/main` 的明确 SHA |
| 容器和依赖拓扑 | `deploy/compose.yaml` 及显式 overlays |
| 配置项和默认示例 | `.env.example` + `internal/config` 校验 |
| 数据库结构 | `backend/migrations/*.sql` |
| API 路由和权限 | `internal/bootstrap` + 各模块 Register/Handler |
| CI 实际门禁 | `.github/workflows/ci.yml` + `Makefile` |
| 生产操作 | `docs/deployment/` + `docs/operations/` |
| 历史设计动机 | `docs/architecture/stage-*.md` + `docs/adr/` |
