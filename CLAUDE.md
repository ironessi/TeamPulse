# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目简介

**TeamPulse** -- 基于 GoFrame v2 + MySQL + Redis 的团队协作后端练习项目，演示 JWT 认证、Token 黑名单、验证码 TTL、用户资料缓存（Cache Aside）、团队成员集合、在线状态心跳、团队动态流、任务管理、通知中心、热门排行榜和接口限流等功能。

模块名：`redis-demo`，Go 1.25.0，服务端口 `:8000`。静态前端资源从 `resource/public` 目录提供，浏览器访问 `/` 即可加载。

## 常用命令

```bash
# 首次配置（复制示例配置并填写数据库/Redis 连接信息）
cp manifest/config/config.example.yaml manifest/config/config.yaml
cp hack/config.example.yaml hack/config.yaml

# 运行
go run main.go

# 构建（需要 GoFrame CLI）
make build            # 使用 gf build -ew

# 代码生成（需要 GoFrame CLI + MySQL 连接）
make ctrl             # 根据 API 定义重新生成 controller
make dao              # 根据 MySQL 表结构重新生成 DAO/DO/Entity

# 测试
go test ./... -count=1                              # 运行所有测试
go test ./internal/logic/jwt/... -count=1           # 运行单个包的测试
go test ./internal/logic/task/... -run TestUpdateTask -count=1  # 运行单个测试函数

# 部署
make image            # 构建 Docker 镜像
make deploy           # 通过 Kustomize 部署到 Kubernetes
```

## 架构

标准 GoFrame v2 分层架构：

- **api/** -- 请求/响应结构体，通过 `g.Meta` 标签定义路由，`v:` 标签定义校验规则
- **internal/controller/** -- 薄层 HTTP 处理器，从 JWT 上下文中提取用户信息后委托给 logic。每个操作一个文件：`{name}_v1_{action}.go`。模块包括 auth、user、team、presence、task、notification
- **internal/logic/** -- 业务逻辑层，所有 Redis 和 MySQL 交互都在这里。Controller 不直接操作数据库。模块包括 auth、user、team、presence、task、notification、jwt、ratelimit
- **internal/dao/** -- 由 `gf gen dao` 自动生成，使用 `.Ctx(ctx)` 进行查询构建
- **internal/model/entity/** -- 自动生成的数据库实体结构体
- **internal/model/do/** -- 自动生成的插入/更新数据对象
- **internal/middleware/auth.go** -- JWT 中间件：解析 Bearer Token、检查 Redis 黑名单、将 userId/username 注入请求上下文
- **internal/cmd/cmd.go** -- 路由注册和中间件绑定

## Redis Key 规范

| 模式 | Key 格式 | 数据类型 | TTL |
|------|----------|----------|-----|
| 验证码 | `auth:captcha:{username}` | String | 5 分钟 |
| JWT 黑名单 | `jwt:blacklist:{token}` | String | 2 小时 |
| 用户资料缓存 | `user:profile:{userId}` | String (JSON) | 30 分钟 |
| 任务详情缓存 | `task:detail:{taskId}` | String (JSON) | 5 分钟 |
| 任务详情空值 | `task:detail:{taskId}` | String (`__NULL__`) | 60 秒 |
| 团队成员 | `team:members:{teamId}` | Set | 30 分钟 |
| 团队动态 | `team:activities:{teamId}` | List | 7 天 |
| 用户在线状态 | `presence:user:{userId}` | String | 60 秒 |
| 团队在线成员 | `presence:team:{teamId}` | Set | 1 小时 |
| 热门任务排行 | `team:task:hot:{teamId}` | Sorted Set | 无 |
| 任务点赞 | `task:likes:{taskId}` | Set | 无 |
| 通知未读集合 | `notification:unread:{userId}` | Set | 无 |
| 延迟提醒 | `task:reminder` | Sorted Set | score 为提醒时间戳 |
| 通知重试队列 | `notification:retry:queue` | List | 无 |
| 通知重试处理中 | `notification:retry:processing` | List | 无 |
| 创建任务限流 | `rate:task:create:{userId}:{minute}` | String Counter | 60 秒 |
| 登录限流 | `rate:login:{ip}:{minute}` | String Counter | 60 秒 |
| 任务详情锁 | `lock:task:detail:{taskId}` | String NX | 10 秒 |
| 提醒扫描锁 | `lock:task:reminder:scanner` | String NX | 4 秒 |
| 重试 worker 锁 | `lock:notification:retry:worker` | String NX | 4 秒 |

所有 Redis 操作集中在 `internal/logic/` 层。

## 路由结构

`internal/cmd/cmd.go` 注册两组路由：
- 公开路由（`/`）：auth 模块（注册、登录、验证码），无需认证
- 受保护路由（`/`，带 `middleware.Auth`）：user、team、presence、task、notification 模块，需要 JWT

## 配置结构

配置文件 `manifest/config/config.yaml`（从 `config.example.yaml` 复制）：
- `server.address` -- 监听地址（默认 `:8000`）
- `database.default.link` -- MySQL 连接串
- `redis.default.address` / `redis.default.db` -- Redis 连接
- `jwt.secret` / `jwt.issuer` -- JWT 签发配置

## 核心设计模式

- **Cache Aside（旁路缓存）**：用户资料和任务详情读取时先查 Redis，未命中则查 MySQL 并回写 Redis；更新时删除缓存。任务详情额外使用空值缓存（`__NULL__`）防止缓存穿透
- **Token 黑名单**：登出时将 Token 写入 Redis，TTL 与 JWT 过期时间一致；中间件每次请求检查
- **双键在线状态**：用户级 key（60 秒 TTL）+ 团队级 Set，惰性清理过期成员
- **热门任务排行**：使用 Redis Sorted Set，成员为任务 ID，分数为热度值（每次查看/编辑/状态变更累加），按分数降序获取热门任务
- **通知未读集合**：Redis Set 存储未读 notificationId，创建通知时 SAdd，标记已读时 SRem，计数用 SCard；集合为空时从 MySQL 重建
- **滑动窗口限流**：Redis INCR + EXPIRE 实现每分钟计数器，创建任务限 10 次/用户/分钟，登录限 5 次/IP/分钟

## 后台 Worker

服务启动时在 `internal/cmd/cmd.go` 启动两个后台 goroutine：

- **提醒扫描**（`task.StartReminderScanner`）：每 5 秒扫描 Redis ZSet `task:reminder` 中到期的任务，给负责人创建通知。使用分布式锁 `lock:task:reminder:scanner` 防止多实例重复扫描。
- **通知重试**（`notification.StartNotificationRetryWorker`）：每 5 秒从 `notification:retry:queue` 取出失败通知重试创建。使用 `LMOVE` 保证消息不丢失，分布式锁 `lock:notification:retry:worker` 防止多实例重复处理。

## 测试与验证

```bash
go test ./... -count=1              # 所有单元测试（需要本地 MySQL + Redis）
bash hack/smoke-test.sh             # 全链路 smoke test（需要服务运行中）
```

测试文件位于 `internal/logic/` 各子包中，使用 `t.Cleanup` 清理 Redis key 和 MySQL 数据。

## 代码约定

- 配置读取：`g.Cfg().MustGet(ctx, "key.path", defaultValue)`，带硬编码兜底默认值
- JWT 密钥：配置项 `jwt.secret`，签发者：`jwt.issuer`
- 获取当前用户 ID：`g.RequestFromCtx(ctx).GetCtxVar("userId").Uint()`，用户名同理用 `"username"`
- Controller 使用结构体 + `NewV1()` 构造函数；Logic 使用独立函数（无接收者）
- API 结构体通过 `g.Meta` 标签定义路由（path、method、summary、tags）
- Redis key 生成函数定义在各 logic 文件中（如 `presenceUserKey`、`taskHotKey`），不要硬编码 key 字符串
- 部分测试会访问本地 MySQL 和 Redis，测试结束时清理产生的 Redis key、通知、动态或任务副作用
