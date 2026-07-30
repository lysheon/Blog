# Blog 文档中心

本文档目录面向已经进入部署与运维阶段的 Blog 项目。根目录 README 用于项目概览；部署、运维、当前架构和历史设计在这里分别维护。

## 按目标阅读

| 目标 | 从这里开始 | 说明 |
|---|---|---|
| 首次部署 | [首次部署指南](deployment/first-deployment.md) | 从 `origin/main`、配置与 Secret 准备，到启动、管理员初始化和验收 |
| 发布、回滚、备份或故障处理 | [生产运维手册](operations/runbook.md) | 发布门禁、镜像回滚、MySQL 恢复、Milvus 重建、Redis/Worker 故障 |
| 理解当前运行系统 | [当前架构](architecture/README.md) | 运行拓扑、依赖边界、请求与后台任务链路 |
| 理解仓库与远端分支 | [仓库结构](repository-structure.md) | 源码职责、部署文件、CI 和 `origin/main` 基线 |
| 追溯技术决策 | [ADR-0001](adr/0001-modular-monolith.md) | 模块化单体、同镜像 API/Worker、MySQL 队列与 Kafka 后置 |
| 追溯阶段实现 | [阶段文档索引](architecture/README.md#阶段实现记录) | Stage 0–5.1 的交付边界和验收记录 |
| 使用自动化脚本 | [脚本说明](../scripts/README.md) | 隐私检查、真实 API E2E、备份与恢复脚本 |

## 文档结构

```text
docs/
├── README.md                         # 本入口
├── deployment/
│   └── first-deployment.md           # 新环境首次部署
├── operations/
│   └── runbook.md                    # 发布、回滚、备份、恢复与故障处理
├── architecture/
│   ├── README.md                     # 当前架构与阶段索引
│   └── stage-{0,1,2,3,4,5,5-1}.md   # 历史阶段交付记录
├── adr/
│   └── 0001-modular-monolith.md      # 架构决策记录
└── repository-structure.md           # 本地源码与远端代码结构
```

## 文档维护规则

1. 当前部署命令只写在 `deployment/`，生产操作只写在 `operations/`；其他文档链接过去，不复制成多份。
2. `architecture/README.md` 描述当前状态；`stage-*.md` 是演进记录，不应覆盖当前部署事实。
3. SQL migration 是数据库结构唯一事实源，Compose 文件是容器拓扑事实源，`.env.example` 是配置项模板。
4. 文档不得包含真实 DSN、密码、Token、API key、私有备份路径或生产主机信息。
5. 行为、路由、配置或目录发生变化时，在同一 PR 更新对应文档并执行 `make privacy-check`。
6. 部署始终以远端 `origin/main` 的明确 commit SHA 和不可变镜像 tag 为基线，不直接部署功能分支。
