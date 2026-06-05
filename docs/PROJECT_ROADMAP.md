# TeamPulse 项目开发路线

更新日期：2026-06-04
当前仓库名：`redis-demo`
产品方向：公司内部团队协作与任务流转后端

## 1. 项目目标

TeamPulse 不是秒杀演示项目，而是一个更贴近公司内部系统的 Redis 实战项目。员工登录系统后，可以加入团队、看到在线同事、创建和处理任务、接收通知、浏览团队动态，并查看热门任务。

项目最终覆盖的业务场景：

- 员工注册、登录、鉴权与退出登录
- 团队创建、成员管理与组织数据缓存
- 团队成员在线状态
- 任务/工单创建、查看与流转
- 通知中心与消息已读/未读
- 团队操作动态流
- 热门任务排行
- 关键接口限流
- 后续增强：分布式锁、延迟任务

Redis 在本项目中的角色不是主数据库，而是支撑公司系统中最常见的高频能力：

```text
登录态失效、短期验证码、热点缓存、在线状态、未读数、
实时动态、排行榜、限流、锁与延迟任务
```

## 2. 技术与分层原则

### 技术栈

| 技术 | 用途 |
| --- | --- |
| GoFrame v2 | HTTP 服务、路由绑定、校验、DAO |
| MySQL | 用户、团队、成员、任务、通知等可靠主数据 |
| Redis | 缓存、TTL 状态、动态流、未读集合、排行、限流 |
| JWT | 登录后的 API 身份凭证 |

### 代码分层

| 层级 | 职责 |
| --- | --- |
| `api/` | 定义路由、请求结构、响应结构和参数校验 |
| `internal/controller/` | 获取 JWT 用户信息，调用 Logic，返回响应 |
| `internal/logic/` | 业务规则、权限校验、MySQL 与 Redis 操作 |
| `internal/dao/` | 数据库访问对象 |
| `internal/model/` | MySQL 对应的数据结构 |
| `internal/middleware/` | JWT 鉴权与请求上下文注入 |

### 数据原则

- MySQL 是业务真实数据源，任务和通知不能只存在 Redis。
- Redis 保存可重建、需要 TTL 或需要高性能访问的数据。
- 团队权限必须在 Logic 层校验，不能只依赖 Controller。
- 每个新功能都要验证成功路径和越权路径。

## 3. 当前开发进度总览

| 阶段 | 模块 | 状态 |
| --- | --- | --- |
| 第一阶段 | 用户与团队 | 基础功能与成员列表读取权限已完成验收 |
| 第二阶段 | 在线状态 | 基础功能与在线成员读取权限已完成验收 |
| 第三阶段 | 任务/工单流转 | 主线功能已完成手工验收，任务编辑关键规则已有可重复运行的自动测试 |
| 第四阶段 | 通知中心与未读数 | 指派通知、列表、已读、未读数链路已验收，状态通知后端接入已通过相关测试 |
| 第五阶段 | 团队动态流 | 基础功能已完成，正在完成权限验收 |
| 第六阶段 | 热门任务排行 | 基础功能已完成，控制台页面已与任务工作台联动 |
| 第七阶段 | 接口限流 | 创建任务限流与登录限流均已接入并完成 HTTP 联调 |
| 增强阶段 | 高级缓存、分布式锁、评论通知、延迟任务 | 任务详情缓存、通用锁、工单评论与提及通知均已完成后端验收 |

## 4. Redis Key 总设计

| 业务 | Key 格式 | 类型 | TTL / 说明 | 状态 |
| --- | --- | --- | --- | --- |
| 登录验证码 | `auth:captcha:{username}` | String | 5 分钟 | 已完成 |
| JWT 退出黑名单 | `jwt:blacklist:{token}` | String | 与 token 有效期配合 | 已完成 |
| 用户资料缓存 | `user:profile:{userId}` | String(JSON) | 30 分钟 | 已完成 |
| 团队成员缓存 | `team:members:{teamId}` | Set | 30 分钟 | 已完成 |
| 用户在线状态 | `presence:user:{userId}` | String | 60 秒 | 已完成 |
| 团队在线候选成员 | `presence:team:{teamId}` | Set | 1 小时 | 已完成 |
| 团队动态流 | `team:activities:{teamId}` | List | 7 天，只保留最近 100 条 | 已完成 |
| 通知未读集合 | `notification:unread:{userId}` | Set | 可由 MySQL 重建 | 已实现并验收 |
| 任务热门排行 | `team:task:hot:{teamId}` | Sorted Set | 按详情浏览加分 | 已完成基础功能 |
| 任务详情缓存 | `task:detail:{taskId}` | String(JSON / `__NULL__`) | 正常缓存 5-6 分钟，空值 60 秒 | 已实现并验收 |
| 任务详情互斥锁 | `lock:task:detail:{taskId}` | String NX | 10 秒 | 已迁移到通用锁工具 |
| 创建任务限流 | `rate:task:create:{userId}:{minute}` | String Counter | 1 分钟 | 已实现并验收 |
| 登录限流 | `rate:login:{ip}:{minute}` | String Counter | 1 分钟 | 已实现并验收 |
| 延迟提醒 | `task:reminder` | Sorted Set | score 为提醒时间戳，member 为 taskId | 设置提醒、扫描到期、后台 scanner 与 scanner 分布式锁已完成 |
| 分布式锁 | `lock:{business}:{id}` | String NX | 短 TTL，Lua 原子释放 | 已实现通用工具 |

说明：团队动态流在当前代码中使用的是 `team:activities:{teamId}`，后续延续这个已实现命名。

## 5. 第一阶段：用户与团队

### 目标

完成员工身份与团队关系的基础能力，为所有后续模块提供登录用户和团队权限边界。

### 已实现接口

| Method | Path | 用途 | 状态 |
| --- | --- | --- | --- |
| `POST` | `/auth/register` | 注册员工账号 | 已完成 |
| `POST` | `/auth/captcha` | 获取登录验证码 | 已完成 |
| `POST` | `/auth/login` | 登录并签发 JWT | 已完成 |
| `POST` | `/auth/logout` | 退出并拉黑当前 JWT | 已完成 |
| `GET` | `/user/profile` | 获取当前员工资料 | 已完成 |
| `PUT` | `/user/profile` | 更新当前员工资料 | 已完成 |
| `POST` | `/teams` | 创建团队 | 已完成 |
| `POST` | `/teams/{teamId}/members` | owner 添加成员 | 已完成 |
| `GET` | `/teams/{teamId}/members` | 查询团队成员 | 已完成基础功能 |

### Redis 学习点

#### 用户资料缓存

```text
GET /user/profile
-> 先读 user:profile:{userId}
-> 缓存未命中时查 MySQL
-> 将结果写回 Redis

PUT /user/profile
-> 更新 MySQL
-> 删除 user:profile:{userId}
```

这是典型 Cache Aside 模式。

#### 团队成员缓存

```text
team:members:{teamId} -> Set(userId)
```

团队创建和添加成员时同步维护 Set，查询成员时可以优先使用缓存中的成员 ID。

### 待收尾事项

`GET /teams/{teamId}/members` 已将当前用户传入 Logic，并在读取成员缓存前校验其属于目标团队；已验证成员可查询、非成员被拒绝。

## 6. 第二阶段：团队成员在线状态

### 目标

模拟公司 IM 或协作平台中的在线成员展示：用户停留在某个团队工作区时定时上报心跳，其他成员可以看到当前在线同事。

### 已实现接口

| Method | Path | 用途 | 状态 |
| --- | --- | --- | --- |
| `POST` | `/presence/heartbeat` | 上报当前用户在线心跳 | 已完成 |
| `GET` | `/teams/{teamId}/online-members` | 查询团队在线成员 | 已完成基础功能 |

### Redis 设计

前端每隔约 30 秒发送一次心跳：

```text
presence:user:{userId} -> teamId
TTL: 60 秒
```

只要该 key 存在，就认为用户在线。

同时用 Set 维护某个团队最近出现过的在线候选成员：

```text
presence:team:{teamId} -> Set(userId)
TTL: 1 小时
```

查询在线成员时：

```text
读取团队 Set
-> 逐个检查 presence:user:{userId} 是否存在
-> 返回仍在线的成员
-> 顺手从 Set 中清理已离线成员
```

### 待收尾事项

`GET /teams/{teamId}/online-members` 已将当前用户传入 Logic，并在读取在线集合前校验其属于目标团队；已验证成员可查询、非成员被拒绝。

## 7. 第三阶段：任务/工单流转

### 目标

完成协作系统的核心实体：团队成员可以创建任务、查看任务、推动任务状态变化，并为动态流、通知和排行提供业务事件。

### 当前已完成

| Method | Path | 用途 | 状态 |
| --- | --- | --- | --- |
| `POST` | `/teams/{teamId}/tasks` | 创建任务 | 已完成并手工验证 |
| `GET` | `/teams/{teamId}/tasks` | 查询团队任务列表 | 已完成并手工验证 |
| `GET` | `/tasks/{taskId}` | 查询任务详情并累计浏览热度 | 已完成并手工验证 |
| `PUT` | `/tasks/{taskId}` | 更新任务信息 | 已完成并手工验证 |
| `PATCH` | `/tasks/{taskId}/status` | 更新任务状态 | 状态流转与热度计分已验证 |
| `GET` | `/teams/{teamId}/tasks/hot` | 查询热门任务排行 | 已完成并手工验证 |

### 当前业务流程

```text
创建任务
-> MySQL 生成 task，初始 status=todo
-> Redis 团队动态新增 task_created

更新任务状态为 doing
-> MySQL 中 status 变化
-> Redis 团队动态新增 task_status_updated
-> Redis Sorted Set 为该任务增加一次热度

查看任务详情
-> 校验访问者属于任务所在团队
-> Redis Sorted Set 为该任务增加浏览热度

查询热门任务
-> Redis 按热度倒序返回任务 ID
-> MySQL 补充任务摘要信息
-> 前端控制台展示热度排行
```

此前已验证非团队成员不能：

- 查看团队任务列表
- 修改任务状态

### 主线接口验收结果

| Method | Path | 用途 | 验收要点 |
| --- | --- | --- | --- |
| `PUT` | `/tasks/{taskId}` | 更新任务信息 | 成员编辑、解除分配、负责人归属和非成员越权均已验证 |

### `GET /tasks/{taskId}` 的实现结果

1. API 已定义 `DetailReq` 与 `DetailRes`。
2. Logic 根据 `taskId` 查任务，并将 `sql.ErrNoRows` 转为“任务不存在”。
3. Logic 使用数据库中的 `task.TeamId` 校验当前用户属于团队。
4. 权限通过后，通过 `ZINCRBY team:task:hot:{teamId} 1 taskId` 累加热度。
5. 已验证成员可查看、非成员被拒绝、查看详情后排行分数增加。

### `PUT /tasks/{taskId}` 的实现规则

建议允许编辑：

```json
{
  "title": "新的任务标题",
  "description": "新的说明",
  "assigneeId": 11,
  "priority": 3
}
```

规则：

- 任务必须存在。
- 操作者必须属于任务所在团队。
- `assigneeId = 0` 表示暂不分配负责人。
- 新负责人非零时必须属于同一团队。
- 状态更新继续使用已经完成的 `PATCH /tasks/{taskId}/status`。
- 编辑成功后向团队动态流追加一条事件。

实现结果：

- API 已定义 `UpdateReq` 与 `UpdateRes`，只允许提交标题、描述、负责人和优先级。
- Logic 使用任务所属团队校验操作者与负责人，不允许跨团队编辑或指派。
- `assigneeId = 0` 时将数据库中的负责人字段写为 `NULL`。
- 提交的字段没有变化时保持幂等，不重复记录动态或增加热度。
- 编辑成功后写入 `task_updated` 团队动态并增加一次排行热度；成员编辑流程已手工验证。
- 控制台已加入任务创建、列表、详情编辑与状态切换入口，并在操作后刷新动态流和热门排行。

手工验收记录（2026-05-27）：

- 团队成员通过控制台成功编辑任务，并通过 `assigneeId = 0` 解除负责人。
- 对无效负责人提交编辑请求被拒绝，负责人必须属于任务所在团队的规则生效。
- 任务状态更新成功写入动态并增加热度；连续查看任务详情也成功增加热度。
- 非团队成员编辑任务被拒绝，编辑接口的团队成员权限校验已验证。

自动测试记录（2026-05-27）：

- `TestUpdateTaskRejectsNonMember` 已通过，覆盖非团队成员编辑失败。
- `TestUpdateTaskRejectsAssigneeOutsideTeam` 已通过，覆盖跨团队负责人失败。
- `TestUpdateTaskUnchangedDoesNotAddActivityOrHeat` 已通过，覆盖无变化提交不追加动态或热度。
- `TestUpdateTaskUpdatesAndClearsAssignee` 已通过，覆盖成功写入编辑字段与将负责人写为空值；页面验收已覆盖从已分配到解除负责人的操作流程。
- 写入测试已在清理阶段恢复任务字段、移除测试动态并回退热度；使用 `-count=2` 连续运行验证通过。

## 8. 第四阶段：通知中心与已读未读

### 目标

将任务流转产生的事件送达到相关员工，例如任务被分配、状态变化、有人提及自己等。

### 计划接口

| Method | Path | 用途 |
| --- | --- | --- |
| `GET` | `/notifications` | 查询当前用户通知列表 | 已实现并验收 |
| `PATCH` | `/notifications/{notificationId}/read` | 标记当前用户的一条通知已读 | 已实现并验收 |
| `GET` | `/notifications/unread-count` | 查询当前用户未读数 | 已实现并验收 |

第一期不提供 `POST /notifications`。通知不是用户随意发布的消息，而是由任务业务动作在 Logic 内部产生，避免伪造通知或绕过权限规则。

### 数据设计

MySQL 保存通知主数据，是通知是否存在、属于谁以及是否已读的真实数据源：

```text
notification
- id               bigint unsigned primary key
- receiver_id      bigint unsigned not null       接收通知的用户
- actor_id         bigint unsigned not null       触发动作的用户
- type             varchar(32) not null           通知类型
- content          varchar(255) not null          展示文案
- related_task_id  bigint unsigned null           关联任务
- is_read          tinyint not null default 0     0 未读，1 已读
- created_at       datetime not null
- read_at          datetime null
```

建议索引：

```text
index(receiver_id, created_at)
index(receiver_id, is_read)
```

第一期建表 SQL：

```sql
CREATE TABLE `notification` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '通知ID',
  `receiver_id` bigint unsigned NOT NULL COMMENT '接收人ID',
  `actor_id` bigint unsigned NOT NULL COMMENT '触发人ID',
  `type` varchar(32) NOT NULL COMMENT '通知类型',
  `content` varchar(255) NOT NULL COMMENT '通知内容',
  `related_task_id` bigint unsigned DEFAULT NULL COMMENT '关联任务ID',
  `is_read` tinyint unsigned NOT NULL DEFAULT '0' COMMENT '是否已读：0未读/1已读',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `read_at` datetime DEFAULT NULL COMMENT '已读时间',
  PRIMARY KEY (`id`),
  KEY `idx_notification_receiver_created` (`receiver_id`, `created_at`),
  KEY `idx_notification_receiver_read` (`receiver_id`, `is_read`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户通知';
```

已实现通知类型：

| Type | 触发场景 | 接收人 |
| --- | --- | --- |
| `task_assigned` | 任务负责人变更为某成员 | 新负责人 |
| `task_status_updated` | 任务状态发生变化 | 当前负责人 |
| `task_commented` | 任务被评论 | 当前负责人 |
| `task_mentioned` | 评论中提及成员 | 被提及成员 |

Redis 使用 Set 加速未读数量。Set 成员为 `notificationId`，可由 MySQL 中 `is_read = 0` 的数据重建：

```redis
SADD  notification:unread:1 1001
SREM  notification:unread:1 1001
SCARD notification:unread:1
```

### 业务规则

- 通知只能由业务 Logic 创建，不开放前端创建通知接口。
- 用户只能查询自己的通知、读取自己的未读数、将自己的通知标记为已读。
- 通知列表第一期按 `created_at` 倒序返回最近 20 条，避免无边界读取。
- `receiver_id = actor_id` 时不创建通知，避免自己通知自己。
- 任务重新分配时只通知新的非零负责人；解除负责人不创建指派通知。
- 状态更新只通知当前非零负责人，且状态实际变化时才触发。
- 重复标记同一条已读通知应幂等成功，不重复改变 MySQL 或 Redis 计数。
- MySQL 是真实数据源；如果 Redis 未读集合写入失败，已有通知仍以 MySQL 记录为准，并允许查询未读数时重建缓存。

### 业务流程

任务指派通知：

```text
UpdateTask 中 assigneeId 从旧值变为新值
-> 新 assigneeId 非零且不等于 operatorId
-> 插入 MySQL notification(type=task_assigned, receiver_id=新负责人)
-> SADD notification:unread:{新负责人} notificationId
```

任务状态通知：

```text
UpdateStatus 中 status 实际发生变化
-> 当前任务 assigneeId 非零且不等于 operatorId
-> 插入 MySQL notification(type=task_status_updated, receiver_id=负责人)
-> SADD notification:unread:{负责人} notificationId
```

查询通知：

```text
从 JWT 获取当前 userId
-> MySQL 按 receiver_id = userId 查询
-> created_at 倒序返回最近 20 条通知
```

标记已读：

```text
从 JWT 获取当前 userId
-> 只查询 receiver_id = userId 且 id = notificationId 的通知
-> 已经 is_read = 1 时不再更新 MySQL，但仍 SREM 修复可能残留的未读集合成员
-> 更新 MySQL is_read = 1、read_at = 当前时间
-> SREM notification:unread:{userId} notificationId
```

查询未读数：

```text
从 JWT 获取当前 userId
-> SCARD notification:unread:{userId}
-> 数量大于 0 时直接返回
-> 数量为 0 时从 MySQL 查询未读通知 ID，存在未读则回填 Set 后返回数量
-> MySQL 同样无未读时返回 0
```

第一期采用上述简单重建方式，因此“确实没有未读”时会查询一次 MySQL；后续如需减少空结果查询，可增加缓存初始化标记 key。

### 与任务模块的联动

第一期在以下既有业务方法成功写入任务数据后创建通知：

| Logic 方法 | 触发通知 | 说明 |
| --- | --- | --- |
| `UpdateTask` | `task_assigned` | 已接入并验收：负责人发生变化且新负责人不是操作者本人 |
| `UpdateStatus` | `task_status_updated` | 已接入：状态发生变化、负责人非零且负责人不是操作者本人 |

评论通知与提及通知已在工单评论阶段接入。

### 联调验收记录（2026-05-27）

- A 将任务负责人改为 B 后，B 的 `GET /notifications` 返回未读 `task_assigned` 通知。
- 新通知 ID 已写入 Redis DB 1 的 `notification:unread:{B}` Set。
- A 尝试标记 B 的通知已读被拒绝，返回“你没有权限操作该通知”。
- B 标记自己的通知已读后，列表返回 `isRead = 1` 且 `readAt` 非空，Redis Set 已移除该通知 ID。
- 重复标记已读保持幂等，Redis 未读集合不会恢复脏数据。
- `GET /notifications/unread-count` 已验证无未读时返回 `0`，Redis 已有未读集合时直接返回集合数量。
- 已验证 Redis 集合为空但 MySQL 存在未读通知时，接口返回真实数量并将通知 ID 回填至 `notification:unread:{userId}`。
- 已验证未携带 JWT 时未读数接口返回 `401`，标记全部通知已读后接口和 Redis `SCARD` 均返回 `0`。

### 第一期实现顺序

1. 在 MySQL 创建 `notification` 表，并通过 `make dao` 生成 `dao`、`do`、`entity` 模型。已完成。
2. 定义通知列表与标记已读的 API 请求和响应结构。已完成。
3. 实现内部创建通知、查询列表与标记已读 Logic。已完成。
4. 实现列表、已读 Controller 并注册受 JWT 保护的 notification 模块路由。已完成。
5. 在 `UpdateTask` 中触发 `task_assigned`，并联调创建、读取、已读与 Redis 集合。已完成。
6. 实现并联调未读数接口，验证 Redis 命中、MySQL 重建、JWT 保护与已读清零链路。已完成。
7. 在 `UpdateStatus` 中接入状态通知。已完成后端接入并通过相关测试，待补 HTTP、MySQL 与 Redis 联调记录。

## 9. 第五阶段：团队操作动态流

### 目标

提供公司协作系统首页常见的“团队最近发生了什么”信息流。

示例：

```text
张三创建了任务 A
李四将任务 B 更新为完成
王五后续评论了工单 C
```

### 当前状态

| 能力 | 状态 |
| --- | --- |
| Redis List 写入与读取 | 已完成 |
| 创建团队写动态 | 已完成 |
| 添加成员写动态 | 已完成 |
| 创建任务写动态 | 已完成 |
| 更新任务状态写动态 | 已完成 |
| 查看动态的成员权限 | 代码已完成，需补一次手工越权验收 |

### 当前接口

```http
GET /teams/{teamId}/activities
```

### Redis 设计

```redis
LPUSH team:activities:{teamId} json
LTRIM team:activities:{teamId} 0 99
LRANGE team:activities:{teamId} 0 19
EXPIRE team:activities:{teamId} 604800
```

### 后续扩展事件

任务完整更新和通知完成后，继续增加：

- `task_updated`
- `task_assigned`
- `task_commented`

## 10. 第六阶段：热门任务排行

### 目标

展示某个团队最近最受关注的任务，这是 Redis Sorted Set 在公司业务中的典型用法。

### 已实现接口

```http
GET /teams/{teamId}/tasks/hot
```

### Redis 设计

```text
key:    team:task:hot:{teamId}
member: taskId
score:  热度分数
```

最初的加分规则建议保持简单：

| 行为 | 分数 |
| --- | --- |
| 有权限的成员查看任务详情 | `+1` |
| 更新任务基本信息 | `+1` |
| 更新任务状态 | `+1` |

实现示意：

```redis
ZINCRBY team:task:hot:7 1 1001
ZREVRANGE team:task:hot:7 0 9 WITHSCORES
```

### 开发依赖

热门排行应在 `GET /tasks/{taskId}` 完成后开发，因为详情浏览是最自然的热度来源。

### 验收场景

```text
查看任务 A 三次
查看任务 B 一次
GET /teams/{teamId}/tasks/hot
-> A 排在 B 之前，并返回对应分数
```

当前已经验证单任务热度自增流程：

```text
读取热门排行，任务 1 热度为 2
-> 请求 GET /tasks/1
-> 再次读取热门排行，任务 1 热度为 3
```

控制台首页已加入热门任务面板，可以输入团队 ID 刷新排行，并通过“查看详情”按钮触发一次热度增长。

## 11. 第七阶段：接口限流

### 目标

保护容易被频繁调用或滥用的接口，练习 Redis 计数器与过期时间的组合。

### 首批限流规则

| 场景 | 规则 | Key |
| --- | --- | --- |
| 创建任务 | 每个用户每分钟最多 10 次 | `rate:task:create:{userId}:{minute}` |
| 登录尝试 | 每个 IP 每分钟最多 5 次 | `rate:login:{ip}:{minute}` |

### Redis 实现思路

```text
INCR key
首次计数时 EXPIRE key 60
若计数超过阈值，则拒绝请求
```

### 放置位置

- 登录限流适合在认证业务入口处理，因为此时还没有 JWT 用户。
- 创建任务限流可以在认证后的 Controller 或 Logic 入口处理。
- 限流逻辑稳定后，可提炼为可配置的中间件或公共 Logic 工具。

### 验收场景

```text
一分钟内连续创建任务 10 次 -> 成功
第 11 次 -> 返回请求过于频繁
等待窗口结束 -> 再次允许创建

同一 IP 一分钟内连续登录尝试 5 次 -> 继续进入原登录逻辑
第 6 次 -> 返回登录过于频繁
```

### 已完成：创建任务限流

已在 `POST /teams/{teamId}/tasks` 入口接入 Redis 计数器限流。当前规则为同一用户每分钟最多创建 10 个任务，使用 `INCR rate:task:create:{userId}:{minute}` 计数，并在首次计数时设置 60 秒 TTL。

已通过 HTTP 联调验证：同一用户连续创建任务时，前 10 次成功，第 11 次返回“请求过于频繁，请稍后再试”。联调产生的测试任务、团队动态和限流 key 已清理。

### 已完成：登录限流

已在 `POST /auth/login` 接入 IP 维度 Redis 计数器限流。当前规则为同一 IP 每分钟最多尝试登录 5 次，使用 `INCR rate:login:{ip}:{minute}` 计数，并在首次计数时设置 60 秒 TTL。

已通过 HTTP 联调验证：同一 IP 前 5 次登录请求继续进入验证码校验逻辑，第 6 次返回“登录过于频繁，请稍后再试”。联调产生的限流 key 已清理。

### 已完成：接口限流自动测试

已补充 `internal/logic/ratelimit` 自动测试，覆盖创建任务第 11 次被拒绝、登录第 6 次被拒绝，并验证限流 key 带有 TTL。测试结束会清理 Redis 中产生的测试 key。

### 已完成：README 展示文档整理

README 已更新为当前 TeamPulse 项目说明，覆盖项目目标、核心功能、Redis key、启动方式、测试命令、API 演示流程和限流验证说明。

### 已完成：进入缓存高并发模式练习

限流阶段收口后，已进入 Redis 高级缓存与高并发模式练习；任务详情缓存穿透、缓存失效、TTL 随机抖动、缓存击穿与通用分布式锁工具均已完成验收。

## 12. 增强阶段：Redis 高级缓存与高并发模式

完成前七阶段后，继续围绕真实业务补充缓存穿透、缓存击穿、缓存雪崩、分布式锁、延迟任务、评论通知和点赞等能力。推荐顺序如下：

| 顺序 | 能力 | 业务场景 | Redis 重点 |
| --- | --- | --- | --- |
| 1 | 缓存穿透 | `GET /tasks/{taskId}` 查询不存在任务 | 空值缓存，短 TTL |
| 2 | 缓存雪崩 | 用户资料、团队成员、任务详情缓存 | TTL 随机抖动 |
| 3 | 缓存击穿 | 热门任务详情 key 过期 | 互斥锁重建缓存 |
| 4 | 分布式锁工具 | 防止重复重建缓存或重复处理业务 | `SET key value NX EX`，释放锁校验 value |
| 5 | 工单评论 | 给任务增加评论能力 | MySQL 主数据 + Redis 动态流 |
| 6 | 提及通知 | 评论中提醒负责人或被提及成员 | 复用 notification + 未读 Set |
| 7 | 延迟任务 | 任务到期前提醒负责人 | ZSet 延迟队列 |
| 8 | 队列/通知重试 | 通知或提醒处理失败后重试 | List / Stream |
| 9 | 点赞 | 任务点赞、取消点赞和点赞数 | Set 去重，计数统计 |

### 已完成：缓存穿透任务详情空值缓存

目标：防止大量请求查询不存在的任务 ID 时反复打到 MySQL。

计划 key：

```text
task:detail:{taskId}
```

已实现流程：

```text
GET /tasks/{taskId}
-> 先读 Redis task:detail:{taskId}
-> 命中 "__NULL__"：直接返回任务不存在
-> 命中任务 JSON：继续校验当前用户是否属于任务团队
-> 未命中：查询 MySQL
-> MySQL 不存在：写入 "__NULL__"，TTL 60 秒
-> MySQL 存在：写入任务 JSON，TTL 带随机抖动
```

当前已完成：不存在任务写入 `__NULL__` 短 TTL 空值缓存；存在任务写入任务 JSON 缓存；缓存命中任务详情时仍会校验团队成员权限，避免绕过 Logic 层权限边界。

已补充自动测试：

- 不存在任务会写入 `task:detail:{taskId}` 空值缓存并带 TTL。
- 已缓存的不存在任务再次查询仍返回“任务不存在”。
- 存在任务会写入任务 JSON 缓存。
- 非团队成员即使命中任务详情缓存，仍会被拒绝。

已验证：`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1` 通过。

### 已完成：任务详情缓存失效

已在 `UpdateTask` 和 `UpdateStatus` 成功修改任务后删除 `task:detail:{taskId}`，避免任务详情缓存短时间返回旧数据。

已补充自动测试：

- `UpdateTask` 成功编辑任务后会删除任务详情缓存。
- `UpdateStatus` 成功修改任务状态后会删除任务详情缓存。

已验证：`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1` 通过。

### 已完成：任务详情缓存 TTL 随机抖动

已给正常任务详情 JSON 缓存增加随机 TTL 抖动：基础 TTL 为 5 分钟，随机增加 0 到 60 秒，避免大量任务详情缓存同时过期。

已补充自动测试：验证 `taskDetailCacheTTL()` 返回值始终位于 5 到 6 分钟之间，并能产生大于基础 TTL 的随机抖动。

已验证：`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1` 通过。

### 已完成：缓存击穿与任务详情互斥重建

热点任务详情缓存未命中时，已使用 `lock:task:detail:{taskId}` 做互斥重建：拿到锁的请求负责查询 MySQL 并写回 `task:detail:{taskId}`，未拿到锁的请求短暂等待后重读缓存。

已实现要点：

- 加锁使用 `SET lock:task:detail:{taskId} token NX EX 10`。
- token 使用随机值，避免释放锁时误删别的请求持有的锁。
- 释放锁前会读取当前 value，只有 value 等于自己的 token 才删除。
- 如果锁已过期或 value 已不匹配，释放动作直接返回，不删除当前锁。

已补充自动测试：

- 首次加锁成功，锁 value 等于随机 token，并带 TTL。
- 同一个任务详情锁未释放前，第二次加锁失败。
- 错误 token 释放锁不会删除锁。
- 正确 token 可以释放锁。
- `GetTask` 缓存未命中并完成回源重建后，会释放 `lock:task:detail:{taskId}`。

已验证：`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1` 通过。

### 已完成：分布式锁工具封装与 Lua 原子释放

已把任务详情里的锁能力沉淀为通用工具，例如 `lock:{business}:{id}`，并升级释放锁逻辑为 Lua 脚本，保证“判断 token + 删除 key”在 Redis 内原子完成。

已实现：

- 新增 `internal/logic/lock` 通用分布式锁包。
- `TryLock(ctx, key, ttl)` 使用 `SET key token NX EX seconds` 尝试获取锁。
- 锁 value 使用随机 token，避免释放锁时误删其他请求持有的锁。
- `Unlock(ctx, lock)` 支持空锁幂等返回。
- `UnlockWithToken(ctx, key, token)` 使用 Lua 脚本原子执行“判断 token + 删除 key”。
- `GetTask` 已迁移为调用通用 `lock` 包，任务模块只保留 `lock:task:detail:{taskId}` 的业务 key 生成逻辑。

释放锁 Lua：

```lua
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
```

已补充自动测试：

- 第一次 `TryLock` 成功并写入 Redis。
- 同一个 key 第二次 `TryLock` 会失败。
- 锁 value 等于随机 token。
- 锁 key 带 TTL，且 TTL 不超过传入秒数。
- 错误 token 释放锁不会删除 key。
- 正确 token 释放锁会删除 key。
- `Unlock(nil)` 幂等返回 nil。
- 任务详情缓存未命中并回源重建后，会释放任务详情锁。

已验证：

```bash
go test ./internal/logic/lock ./internal/logic/task -count=1
go test ./... -count=1
```

测试结果：全部通过。

### 已完成：工单评论与提及通知

已完成任务评论能力，并复用团队动态流和通知中心形成闭环：评论写入 MySQL，评论成功后写入 `team:activities:{teamId}`；评论负责人或提及成员时创建通知，并写入接收人的 Redis 未读集合。

### 工单评论与提及通知

目标：让任务具备真实协作讨论能力。

已实现数据：

```text
task_comment
- id
- task_id
- team_id
- user_id
- content
- created_at
```

已实现流程：

```text
POST /tasks/{taskId}/comments
-> 校验评论内容非空
-> 校验任务存在
-> 校验当前用户属于任务团队
-> 校验被提及用户属于同一团队
-> 写入 MySQL task_comment
-> 写入 Redis 团队动态 team:activities:{teamId}
-> 创建 task_mentioned / task_commented 通知
-> 通知 ID 写入 notification:unread:{receiverId}
```

已实现通知类型：

| Type | 场景 | 接收人 |
| --- | --- | --- |
| `task_commented` | 任务被评论 | 当前负责人 |
| `task_mentioned` | 评论中提及成员 | 被提及成员 |

已实现接口：

| Method | Path | 用途 | 状态 |
| --- | --- | --- | --- |
| `POST` | `/tasks/{taskId}/comments` | 创建任务评论 | 已完成并验收 |
| `GET` | `/tasks/{taskId}/comments` | 查询任务评论列表 | 已完成并验收 |

已完成验收：

- 成员可以创建评论，MySQL `task_comment` 正确写入。
- 成员可以查询任务评论列表，按评论创建顺序返回。
- 空白评论会被拒绝，返回“请输入评论内容”。
- 非团队成员不能创建或查看目标任务评论。
- 任务不存在时返回“任务不存在”。
- 评论成功后写入 `team:activities:{teamId}` 动态流。
- 提及成员时创建 `task_mentioned` 通知，并写入 `notification:unread:{receiverId}`。
- 评论任务负责人时创建 `task_commented` 通知。
- 同一条评论里如果负责人已经通过提及收到通知，不再重复创建 `task_commented`。
- 评论者本人不会收到自己触发的评论或提及通知。

已补充自动测试并通过：

```bash
go test ./internal/logic/task -count=1
go test ./... -count=1
```

### 延迟任务：任务提醒

目标：练习 Redis ZSet 延迟队列，并让任务系统具备提醒能力。

计划 key：

```text
task:reminder
```

结构：

```text
ZADD task:reminder remindAt taskId
```

后台扫描：

```text
ZRANGEBYSCORE task:reminder -inf now
-> 对到期 taskId 创建提醒通知
-> 处理成功后 ZREM task:reminder taskId
```

当前已完成：设置任务提醒、扫描到期提醒、后台定时扫描。

已实现接口：

| Method | Path | 用途 | 状态 |
| --- | --- | --- | --- |
| `POST` | `/tasks/{taskId}/reminder` | 设置任务提醒时间 | 已完成并验收 |

已实现规则：

- 任务必须存在。
- 已完成任务 `done` 不允许设置提醒。
- 当前用户必须属于任务所在团队。
- `remindAt` 必须晚于当前时间。
- 写入 Redis ZSet：`ZADD task:reminder remindAt taskId`。

已完成验收：

- 成员给未完成任务设置未来提醒成功。
- Redis `ZSCORE task:reminder {taskId}` 能查到对应 `remindAt`。
- 过去时间被拒绝，返回“提醒时间必须晚于当前时间”。
- 不存在任务被拒绝，返回“任务不存在”。
- 非团队成员被拒绝，返回“你没有权限设置该任务提醒”。
- 已完成任务被拒绝，返回“已完成任务不允许设置提醒”。
- 联调产生的临时任务、动态、热度和 Redis reminder 已清理。

已验证：

```bash
go test ./api/task/v1 ./internal/controller/task ./internal/logic/task -count=1
go test ./... -count=1
```

已完成第二小步：扫描到期提醒。

已实现流程：

```text
ScanDueReminders(ctx, now)
-> ZRANGEBYSCORE task:reminder -inf now
-> 解析到期 taskId
-> 查询 MySQL 任务
-> 任务不存在、已完成或无负责人时清理 ZSet
-> 正常任务给负责人创建 task_reminder 通知
-> 通知成功后 ZREM task:reminder taskId
```

已完成第三小步：后台定时扫描。

```text
服务启动
-> StartReminderScanner(ctx)
-> goroutine 每 5 秒触发一次
-> 调用 ScanDueReminders(ctx, time.Now().Unix())
-> 扫描失败只记录日志，不影响 HTTP 服务
```

已完成验收：

- 未到期任务不会创建通知，仍保留在 `task:reminder`。
- 到期任务会创建 `task_reminder` 通知，并进入 `notification:unread:{assigneeId}`。
- 通知成功后会从 `task:reminder` 移除 taskId。
- 重复扫描不会重复创建通知。
- 脏 member、不存在任务、已完成任务和无负责人任务会被清理。
- 服务启动后，后台 scanner 能自动把到期 reminder 转换成通知。

已验证：

```bash
go test ./internal/logic/task -run 'TestScanDueReminders' -count=1
go test ./internal/logic/task -count=1
go test ./... -count=1
```

服务级联调记录：

- 启动当前代码版本服务后，手动写入已到期 `task:reminder` member。
- scanner 自动创建 MySQL `task_reminder` 通知。
- 通知 ID 写入负责人未读集合。
- `task:reminder` 中对应 taskId 自动移除。
- 联调产生的通知、未读集合成员和 reminder 已清理。

已完成第四小步：scanner 分布式锁。

已实现流程：

```text
ticker 触发
-> TryLock lock:task:reminder:scanner
-> 拿不到锁：跳过本轮
-> 拿到锁：ScanDueReminders(ctx, now)
-> defer Unlock
```

已实现规则：

- scanner 锁 key 为 `lock:task:reminder:scanner`。
- scanner 锁 TTL 为 4 秒，扫描间隔为 5 秒。
- 拿不到锁不是错误，说明其他实例正在扫描。
- 拿到锁后通过 `defer` 释放，避免后续扫描逻辑增加分支时漏释放。

已完成验收：

- 已有 scanner 锁时，本轮扫描跳过，不创建通知，ZSet 保持不变。
- 无 scanner 锁时，当前实例能拿锁、扫描到期提醒、创建通知、移除 ZSet。
- 扫描结束后 scanner 锁被释放。
- 服务级联调验证加锁后的后台 scanner 仍能自动创建通知。

已验证：

```bash
go test ./internal/logic/task -run 'TestScanDueRemindersWithLock' -count=1
go test ./internal/logic/task ./internal/logic/lock -count=1
go test ./... -count=1
```

### 点赞

目标：练习 Set 去重与状态切换。

计划 key：

```text
task:likes:{taskId}
```

规则：

```text
点赞：SADD task:likes:{taskId} userId
取消点赞：SREM task:likes:{taskId} userId
点赞数：SCARD task:likes:{taskId}
是否点赞：SISMEMBER task:likes:{taskId} userId
```

当前已完成基础闭环：

- 新增 `POST /tasks/{taskId}/like` 点赞任务。
- 新增 `DELETE /tasks/{taskId}/like` 取消点赞。
- 新增 `GET /tasks/{taskId}/like-status` 查询当前用户是否点赞和当前点赞数。
- 点赞前会校验任务存在，并校验当前用户属于任务所在团队。
- 使用 Redis Set 去重，同一用户重复点赞不会重复计数。
- 取消点赞使用 `SREM`，未点赞时取消仍返回成功，保持幂等。

已补充自动测试：

- 重复点赞不增加点赞数。
- 取消点赞后点赞数归零，重复取消仍成功。
- 点赞状态能正确反映当前用户是否点赞和当前点赞数。
- 非团队成员不能点赞、取消点赞或查询点赞状态。
- 任务不存在时返回“任务不存在”。

已验证：

```bash
go test ./internal/logic/task -run 'TestLikeTask|TestUnlikeTask|TestGetLikeStatus|TestTaskLike' -count=1
go test ./internal/logic/task -count=1
go test ./... -count=1
```

已完成 HTTP 联调：

- 初始 `GET /tasks/{taskId}/like-status` 返回 `isLiked=false`、`likeCount=0`。
- 首次 `POST /tasks/{taskId}/like` 返回 `likeCount=1`。
- 重复点赞仍返回 `likeCount=1`，Redis `task:likes:{taskId}` 中只保留当前用户一份。
- 点赞后查询状态返回 `isLiked=true`、`likeCount=1`。
- `DELETE /tasks/{taskId}/like` 返回 `likeCount=0`。
- 重复取消点赞仍返回 `likeCount=0`。
- 非团队成员点赞被拒绝，任务不存在时返回“任务不存在”。
- 联调产生的临时用户、团队、成员关系、任务和 Redis key 已清理。

这些能力不应一次性并行开发；先从缓存穿透开始，每完成一个小阶段都补测试、联调和文档记录。

### 已完成：通知重试队列工具

目标：练习 Redis List 的队列语义，为后续“通知创建失败后重试”预留基础工具。

计划 key：

```text
notification:retry:queue
```

当前已完成基础工具：

- 新增 `RetryNotificationPayload`，保存接收人、触发人、通知类型、内容、关联任务和重试次数。
- 新增 `EnqueueNotificationRetry(ctx, payload)`，将 payload 序列化为 JSON 后通过 `RPUSH` 写入 Redis 队列。
- 新增 `DequeueNotificationRetry(ctx)`，通过 `LPOP` 从 Redis 队列取出一条 payload。
- 队列为空时返回 `nil, nil`，表示没有待重试通知，不作为错误。
- 使用 `RPUSH + LPOP` 保持 FIFO：先入队的通知先被取出。

已补充自动测试：

- 连续入队两条通知后，队列长度为 2。
- 出队顺序符合 FIFO。
- payload JSON 反序列化后字段保持一致。
- 空队列出队返回 `nil, nil`。

已验证：

```bash
go test ./internal/logic/notification -run 'TestNotificationRetryQueue' -count=1
go test ./internal/logic/notification -count=1
go test ./... -count=1
```

## 13. 后续协作增强备选

后续还可以继续扩展：

| 能力 | 使用 Redis 的方式 | 业务例子 |
| --- | --- | --- |
| 组织/团队详情缓存 | String(JSON) Cache Aside | 高频查看组织页 |
| 分布式锁 | `SET key value NX EX` | 防止重复处理任务或重复发送通知 |
| 延迟任务 | Sorted Set / 队列 | 任务到期提醒、通知重试 |
| 工单评论 | 动态流 + 通知 | 评论任务并提醒负责人 |
| 过期任务提醒 | 延迟任务 + 通知 | 截止时间前提醒成员 |

这些功能不应抢在核心流程之前实现；先让任务、通知、动态、排行和限流形成完整闭环。

## 14. 推荐实施顺序

| 顺序 | 功能 | 原因 |
| --- | --- | --- |
| 1 | 实现通知中心与未读数 | 让任务事件真正触达员工 |
| 2 | 完善通知与动态自动测试 | 固化消息触达和信息流规则 |
| 3 | 完善任务与动态自动测试 | 继续固化已实现的业务规则 |
| 4 | 完善动态事件与手工验收 | 让工作台信息流完整 |
| 5 | 接口限流 | 为关键写接口和登录增加保护 |
| 6 | 自动测试、README 与演示脚本 | 形成可展示项目 |
| 7 | 锁与延迟任务 | 作为进阶能力 |

## 15. 里程碑与验收标准

### M1：团队协作基础

状态：已完成

- 员工可注册、登录、退出。
- 可创建团队并添加成员。
- 用户资料与团队成员使用 Redis 缓存。
- 在线心跳和动态查询基本可用。
- 成员、在线成员读取权限已补齐并完成成员/非成员手工验收。

### M2：任务流转与热度

状态：已完成

- 创建、列表、详情、状态流转、热门排行和基本编辑均已实现并完成主线手工验收，任务编辑关键规则已有可重复运行的自动测试覆盖。
- 所有任务读取和修改都有团队成员权限保护。
- 查看与操作任务可以累计热度。
- 团队可查看热门任务排行。

### M3：通知与工作台

状态：已完成

- 任务事件产生通知（指派、状态变更、评论、提及、提醒）。
- 用户可查看通知并标记已读。
- Redis 快速返回未读消息数。
- 动态流展示关键协作动作。
- 评论、提及、负责人评论通知均已完成后端验收。
- 前端已接入评论、点赞、提醒和通知类型图标。

### M4：稳定性与工程化

状态：已完成

- 创建任务与登录接口已具有限流。
- 任务、通知、限流、缓存、分布式锁和评论通知已有测试或可重复联调记录。
- 明确 MySQL 与 Redis 双写失败时的处理策略（通知重试队列）。
- 分布式锁工具已完成（通用锁 + Lua 原子释放）。
- 延迟提醒已完成（ZSet 队列、到期扫描、scanner 分布式锁）。
- 通知重试队列已完成（List 入队、FIFO 出队、worker、worker 分布式锁）。

## 16. 每个功能的学习与开发流程

后续保持“你实现、Codex 辅导”的节奏：

```text
1. Codex 先详细说明当前小阶段的目标、实现思路、数据流、权限规则、异常处理理由与验收点
2. Codex 先提供仅保留步骤注释的空白代码框架，便于你看清实现顺序
3. 空白框架后紧接着给出带必要注释的完整参考代码，由你直接对照完成实现，不需要再次追问
4. 完整参考代码后，Codex 解释代码中容易出错、涉及数据一致性或值得重点理解的地方
5. 按 api -> logic -> controller -> 联调/测试 的顺序完成该小阶段
6. 用 curl 验证成功路径、权限边界和幂等行为
7. 观察 MySQL 记录与 Redis key/返回结果，确认缓存、状态或排行符合预期
8. 每完成并验收一个小阶段，Codex 同步完善本路线图的状态、验收记录和下一步
```

### 每日收尾约定

当你说“今天就到这里”时，Codex 自动完成当天工作的收尾：

1. 只针对当天已经实现并完成验收的功能，补齐 `resource/public` 中相应的前端展示、操作入口和交互反馈；尚未实现或未验收的接口不提前接入页面。
2. 运行与当天改动相关的格式化、编译、测试或可执行联调验证，并记录未能验证的部分。
3. 更新本路线图中的完成状态与验收结果，并新增当天完成事项记录，说明完成的接口、规则、测试和前端收尾内容。
4. 在本路线图中规划第二天任务，写明下一小阶段的目标、首个实施步骤和需要验证的关键行为。
5. 创建一次仅包含本阶段相关代码与文档文件的 Git 提交，提交范围不包含工作区中与本阶段无关的已有改动。

## 17. 当前工作面板

本节只保留“现在做到哪、接下来做什么”。历史验收细节统一放在前文阶段章节和每日进展记录中，避免路线图越写越散。

### 当前状态

| 模块 | 当前状态 | 最近验证 |
| --- | --- | --- |
| 任务主线 | 创建、列表、详情、编辑、状态流转、热门排行均已完成 | `go test ./internal/logic/task -count=1` |
| 通知中心 | 指派通知、状态通知、列表、未读数、已读均已完成 | `go test ./internal/logic/task ./internal/logic/notification -count=1` |
| 限流 | 创建任务限流、登录限流已完成 | `go test ./internal/logic/ratelimit -count=1` |
| 高级缓存 | 任务详情空值缓存、缓存失效、TTL 抖动、互斥重建已完成 | `go test ./internal/logic/task -count=1` |
| 分布式锁 | 通用 `internal/logic/lock`、Lua 原子释放已完成 | `go test ./internal/logic/lock -count=1` |
| 工单评论 | 创建评论、评论列表、团队动态、评论通知、提及通知均已完成后端验收 | `go test ./internal/logic/task -count=1` 与 HTTP 联调 |
| 延迟提醒 | 设置提醒、取消提醒、扫描到期提醒、后台 scanner、scanner 分布式锁均已完成 | `go test ./... -count=1` 与服务级联调 |
| 任务点赞 | 点赞、取消点赞、点赞状态查询已完成后端测试与 HTTP 联调 | `go test ./... -count=1` 与 HTTP 联调 |
| 通知重试队列 | Redis List 入队、出队、FIFO、后台 worker、worker 分布式锁和失败路径入队均已完成 | `go test ./... -count=1` 与服务级联调 |

### 项目最终状态

全部后端 Redis 实战能力已完成，项目进入毕业状态：

| 收尾项 | 状态 |
| --- | --- |
| 全链路 Smoke Test 脚本 | ✅ 已完成 `hack/smoke-test.sh`，覆盖 28 个端点 |
| 前端评论/点赞/提醒入口 | ✅ 已接入任务详情抽屉 |
| 通知类型图标扩展 | ✅ 已支持 commented/mentioned/reminder |
| 路线图里程碑清理 | ✅ M1-M4 全部标记完成 |
| README 演示路径 | ✅ 已补全推荐演示顺序 |
| 全量自动测试 | ✅ `go test ./... -count=1` 通过 |

## 18. 毕业后 Redis 进阶学习路线

当前项目已经覆盖 Redis 在业务系统里的主线用法：验证码、缓存、在线状态、未读集合、动态流、排行、限流、分布式锁、延迟提醒和通知重试队列。后续不再以“补项目功能”为主，而是以“小切片练工程能力”为主。

### 推荐学习顺序

| 顺序 | 主题 | 适合落点 | 学习目标 |
| --- | --- | --- | --- |
| 1 | Pipeline / MGET 批量优化 | 团队成员、任务列表、通知列表 | 减少循环 Redis 请求，提高批量读取效率 |
| 2 | Lua 原子点赞 | `task:likes:{taskId}` | 将 `SADD + SCARD` 合成一次原子操作，返回是否新增与最新点赞数 |
| 3 | Redis 事务对比练习 | `task:likes:{taskId}` 或计数类场景 | 学习 `WATCH`、`MULTI`、`EXEC`，对比 Lua 原子脚本的使用边界 |
| 4 | MySQL 事务 + Redis 最终一致性 | 评论、通知、动态流 | 理解数据库事务能保证什么，Redis 写入失败时如何补偿或重试 |
| 5 | 可靠队列 | `notification:retry:queue` | 从普通 List 重试队列升级为 pending 队列，避免 worker 取出后宕机导致消息丢失 |
| 6 | Redis Stream | 通知事件或团队动态 | 学习 `XADD`、`XREADGROUP`、`XACK`、pending message 和消费组 |
| 7 | 排行榜时间衰减 | `team:task:hot:{teamId}` | 练习日榜、周榜、热度衰减和榜单清理 |
| 8 | Bitmap / HyperLogLog | 任务访问统计 | 练习签到、是否浏览、UV 估算等统计型场景 |
| 9 | 布隆过滤器思想 | 任务详情缓存穿透 | 在空值缓存之外，学习“请求到 MySQL 前先判断 ID 是否可能存在” |

当前事务练习进展：已完成 Redis 事务对比学习函数 `LikeTaskWithRedisTransaction`，并将评论创建中的 `task_comment`、`task_mentioned`、`task_commented` MySQL 写入纳入同一个事务；事务提交后再写 Redis 团队动态和通知未读集合，保持 MySQL 为真实数据源、Redis 最终一致。验证通过：`go test ./internal/logic/task -run 'TestCreateComment' -count=1`、`go test ./internal/logic/notification -count=1` 与 `go test ./... -count=1`。

当前可靠队列进展：通知重试队列已从普通 `LPOP` 消费升级为 `LMOVE notification:retry:queue notification:retry:processing LEFT RIGHT`，worker 处理成功或失败后都会从 processing 队列 ACK 旧消息；失败且未超过最大重试次数时，再将 `retryCount+1` 的新 payload 重新入队，避免 worker 取出消息后宕机或失败重试时丢失、残留或重复入队。验证通过：`go test ./internal/logic/notification -run 'TestNotificationRetryQueue|TestProcessOneNotificationRetryAcksProcessingMessage' -count=1`、`go test ./internal/logic/notification -count=1` 与 `go test ./... -count=1`。

### 下一次最推荐小切片：Lua 原子点赞

当前点赞逻辑已经使用 Redis Set 保证同一用户不会重复点赞，但通常会分成多步：

```text
SADD task:likes:{taskId} userId
SCARD task:likes:{taskId}
```

下一步可以用 Lua 把这两步合成一次 Redis 原子脚本：

```text
输入：taskId、userId
执行：SADD + SCARD
返回：是否本次新增点赞、当前点赞总数
```

这样可以练到三个重点：

- Redis 多命令原子化。
- 高并发下避免中间状态被其他请求插入。
- 后端接口直接拿到最终点赞数，减少额外 Redis 往返。

当前完成状态：已将 `LikeTask` 的点赞写入升级为 Lua 原子脚本，使用一次 `EVAL` 完成 `SADD + SCARD`，并保持原有权限校验、重复点赞幂等和点赞数返回行为。验证通过：`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1`。

### 事务练习说明

后续会练两类事务，但它们解决的问题不同：

| 类型 | 适合场景 | 重点 |
| --- | --- | --- |
| Redis 事务 | 点赞、计数、库存这类 Redis 内部并发更新 | `WATCH` 乐观锁、`MULTI/EXEC` 批量提交、失败后重试 |
| MySQL 事务 | 多张数据库表必须一起成功或一起失败 | 例如评论写入、通知写入、业务记录状态更新 |

注意：MySQL 事务不能把 Redis 写入一起回滚。涉及 MySQL + Redis 的场景，要用“先保证 MySQL 真实数据，再通过删除缓存、重试队列或补偿任务保证 Redis 最终一致”。

### 进阶阶段仍然遵守的节奏

```text
1. 先讲清楚为什么要做这个 Redis 技术点
2. 给空白代码框架
3. 再给完整参考代码
4. 你实现后 Codex 检查
5. 小范围测试或联调
6. 验收后更新本路线图
```

## 19. 每日进展记录

### 2026-05-27：今天完成了什么

- 完成成员列表与在线成员列表的读取权限收口，非团队成员不能读取目标团队信息。
- 完成任务编辑主线验收与关键自动测试，并修正测试中固定用户 ID 随团队成员变化而失真的问题，改为动态选择真实成员与非成员。
- 创建通知表及模型，完成通知列表、标记已读、内部创建通知和 `UpdateTask -> task_assigned` 联动。
- 完成 `GET /notifications/unread-count`，验证 Redis 命中、Redis 为空时从 MySQL 重建、JWT 保护与已读清零流程。
- 完成通知中心前端收尾：页面展示未读数量、通知列表与标记已读操作，并在登录后自动刷新当前用户通知。
- 验证通过：`node --check resource/public/resource/js/app.js` 与 `go test ./... -count=1`；通知接口已完成 HTTP、MySQL 和 Redis 联调。当前内置浏览器没有可用会话，页面布局与实际点击的可视化复验留到下一次启动可用浏览器后补做。

### 2026-05-28：今天完成了什么

- 完成 `task_status_updated` 通知类型常量。
- 在 `UpdateStatus` 中接入状态变化通知：仅当状态实际变化、当前负责人非零且负责人不是操作者本人时，通过内部 `CreateNotification` 创建通知。
- 保持原有状态更新顺序：先更新 MySQL 任务状态，再写团队动态与任务热度，最后创建通知。
- 补充状态通知自动测试，覆盖发给负责人、不重复发送、自己不通知自己，并清理测试产生的 MySQL 与 Redis 副作用。
- 完成状态通知 HTTP 联调，验证负责人通知列表、未读数、标记已读、MySQL 状态和 Redis 未读集合清理。
- 验证通过：`gofmt` 已整理测试文件，`go test ./internal/logic/task ./internal/logic/notification -count=1` 通过。

### 2026-05-29：今天完成了什么

- 完成任务状态通知 HTTP 联调：`PATCH /tasks/{taskId}/status` 触发通知，负责人可查询通知和未读数，标记已读后 MySQL 与 Redis 均正确更新。
- 接入创建任务限流：`POST /teams/{teamId}/tasks` 进入创建逻辑前，按用户维度调用 Redis `INCR` 计数。
- 完成创建任务限流 HTTP 联调：同一用户一分钟内前 10 次创建成功，第 11 次返回“请求过于频繁，请稍后再试”。
- 接入并完成登录限流 HTTP 联调：同一 IP 一分钟内前 5 次登录请求继续进入验证码校验，第 6 次返回“登录过于频繁，请稍后再试”。
- 补充限流自动测试，覆盖创建任务限流、登录限流和 Redis TTL。
- 验收后已清理测试任务、团队动态、任务热度、通知记录和限流 key。
- 验证通过：`go test ./... -count=1`。

### 2026-05-30：今天完成了什么

- 完成 README 展示文档整理，串联登录、团队、任务、通知、动态、排行和限流主流程。
- 完成任务详情缓存穿透练习：不存在任务写入 `__NULL__` 短 TTL，避免反复查询 MySQL。
- 完成任务详情缓存失效：`UpdateTask` 和 `UpdateStatus` 成功修改后删除 `task:detail:{taskId}`。
- 完成任务详情缓存雪崩练习：任务详情缓存 TTL 加入 0 到 60 秒随机抖动。
- 完成任务详情缓存击穿练习：缓存未命中时使用 `lock:task:detail:{taskId}` 互斥重建缓存。
- 补充锁相关自动测试，覆盖加锁、重复加锁失败、错误 token 不释放、正确 token 释放，以及 `GetTask` 回源后释放锁。
- 验证通过：`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1`。

### 2026-05-31：今天完成了什么

- 新增 `internal/logic/lock` 通用分布式锁包，封装 `TryLock`、`Unlock`、随机 token 和锁 TTL。
- 使用 Redis `SET key token NX EX seconds` 获取锁，`locked=false` 表示锁被其他请求持有，不作为系统错误。
- 使用 Lua 脚本释放锁，保证“判断 token + 删除 key”在 Redis 内原子执行，避免误删其他请求持有的锁。
- 将 `GetTask` 当前的任务详情锁迁移到通用锁工具，任务模块只保留 `lock:task:detail:{taskId}` 的业务 key 生成逻辑。
- 补充锁工具自动测试，覆盖加锁、重复加锁失败、TTL、错误 token 不删除、正确 token 删除和 `Unlock(nil)` 幂等。
- 回归任务详情缓存击穿测试，确认缓存未命中并回源重建后会释放任务详情锁。
- 验证通过：`go test ./internal/logic/lock ./internal/logic/task -count=1` 与 `go test ./... -count=1`。
- 启动工单评论阶段：明确 `task_comment` 表结构、评论 API 设计与后续评论通知流程；当前已有评论相关草稿文件，尚未完成 Logic / Controller / 联调验收。
- 整理路线图结构：压缩重复的“当前下一步”和历史完成项，新增“当前工作面板”，让后续开发只看当前状态、当前第一步和明日优先级。

### 2026-06-01：今天完成了什么

- 完成工单评论 API 与 Controller：`POST /tasks/{taskId}/comments` 创建评论，`GET /tasks/{taskId}/comments` 查询评论列表。
- 完成评论 Logic：校验任务存在、评论内容非空、当前用户属于任务团队、被提及用户属于同一团队。
- 评论成功后写入 MySQL `task_comment`，并写入 Redis 团队动态流 `team:activities:{teamId}`。
- 接入评论通知：提及成员创建 `task_mentioned`，评论负责人创建 `task_commented`，通知进入 `notification:unread:{receiverId}` 未读集合。
- 补齐通知去重规则：同一条评论中负责人已被提及时，不再重复创建负责人评论通知；评论者本人不接收自己触发的通知。
- 完成自动测试，覆盖创建评论、查询评论、非法输入、非团队成员拒绝、提及通知、负责人评论通知、重复通知抑制和自己不通知自己。
- 完成 HTTP 联调，验证评论创建、评论列表、空白内容拒绝、非成员拒绝、任务不存在、MySQL 写入、Redis 动态流和 Redis 未读通知集合。
- 验证通过：`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1`。
- 完成延迟提醒第一小步：新增 `POST /tasks/{taskId}/reminder`，将未来提醒时间写入 Redis ZSet `task:reminder`。
- 设置提醒已校验任务存在、任务状态不能为 `done`、当前用户必须属于任务团队、`remindAt` 必须晚于当前时间。
- 完成设置提醒 HTTP/Redis 联调，验证成功写入 ZSet、过去时间拒绝、任务不存在拒绝、非团队成员拒绝、已完成任务拒绝；联调临时数据已清理。
- 验证通过：`go test ./api/task/v1 ./internal/controller/task ./internal/logic/task -count=1` 与 `go test ./... -count=1`。

### 2026-06-02：今天完成了什么

- 完成 `task_reminder` 通知类型。
- 完成 `ScanDueReminders(ctx, now)`：使用 `ZRANGEBYSCORE task:reminder -inf now` 扫描到期 taskId。
- 扫描时以 MySQL 任务为真实数据源；任务不存在、已完成或无负责人时清理 ZSet 并跳过。
- 正常到期任务会给负责人创建 `task_reminder` 通知，并写入 `notification:unread:{assigneeId}`。
- 通知创建成功后再 `ZREM task:reminder taskId`，避免先删除 reminder 后通知创建失败导致提醒丢失。
- 补充提醒扫描自动测试，覆盖未到期不触发、到期触发、重复扫描不重复、脏 member/无效任务清理。
- 完成 `StartReminderScanner(ctx)`，服务启动后用 goroutine 和 ticker 每 5 秒扫描一次到期提醒。
- 在 `internal/cmd/cmd.go` 接入后台 scanner。
- 完成服务级联调：启动当前代码服务，写入到期 reminder，scanner 自动创建通知、写入未读集合并移除 ZSet member；联调数据已清理。
- 验证通过：`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1`。

### 2026-06-03：计划已完成

目标：延迟提醒增强收口，优先练习“多实例防重复扫描”。

1. 先给 scanner 加一把短 TTL 分布式锁：`lock:task:reminder:scanner`。
2. `StartReminderScanner` 每轮触发时先尝试获取锁。
3. 拿到锁才调用 `ScanDueReminders`，拿不到锁直接跳过本轮。
4. 使用现有 `internal/logic/lock` 工具释放锁，保持 token 校验。
5. 补充测试或最小联调，验证拿不到锁时不会扫描，拿到锁时正常处理。
6. 这一阶段不做取消提醒、不做前端，保持小切片。

### 2026-06-03：今天完成了什么

- 给延迟提醒后台 scanner 接入分布式锁 `lock:task:reminder:scanner`。
- 每轮 ticker 触发时先尝试获取锁，拿不到锁直接跳过本轮，避免多实例重复扫描同一批 reminder。
- 拿到锁后调用 `ScanDueReminders(ctx, now)`，扫描结束通过 `defer Unlock` 释放锁。
- 补充 `scanDueRemindersWithLock`，让 scanner 加锁扫描逻辑可单独测试。
- 补充自动测试，覆盖已有锁时跳过扫描、无锁时扫描并释放锁。
- 完成服务级联调：写入到期 reminder 后，scanner 自动拿锁、创建 `task_reminder` 通知、写入未读集合、移除 ZSet member，并最终释放锁。
- 联调产生的通知、未读集合成员、scanner 锁和 reminder 已清理。
- 验证通过：`go test ./internal/logic/task -run 'TestScanDueRemindersWithLock' -count=1`、`go test ./internal/logic/task ./internal/logic/lock -count=1` 与 `go test ./... -count=1`。

### 2026-06-03：取消提醒接口小阶段

目标：延迟提醒体验收口，补齐取消提醒接口。

已完成：

- 新增 `DELETE /tasks/{taskId}/reminder` API 与 Controller。
- 新增 `CancelReminder(ctx, userId, taskId)` Logic，先查 MySQL 任务，再校验当前用户属于任务团队。
- 权限通过后执行 `ZREM task:reminder taskId`，不存在 reminder 时仍返回成功，保证重复取消幂等。
- 非团队成员取消会返回“你没有权限取消该任务提醒”，任务不存在会返回“任务不存在”。
- 补充自动测试，覆盖成员取消成功、重复取消幂等、非成员拒绝、任务不存在拒绝。
- 验证通过：`go test ./internal/logic/task -run 'TestCancelReminder' -count=1`、`go test ./internal/logic/task -count=1` 与 `go test ./... -count=1`。
- 完成 HTTP 联调：临时用户创建团队、添加成员、创建任务、设置提醒后，成员取消成功并从 `task:reminder` 删除；重复取消继续成功；非团队成员取消被拒绝且 Redis reminder 保留；不存在任务返回“任务不存在”。
- 联调产生的临时用户、团队、成员关系、任务和 Redis key 已清理。

下一步：

1. 决定是否补前端任务详情里的设置/取消提醒入口。
2. 如果暂不做前端，则进入下一个 Redis 练习小切片。

### 2026-06-03：点赞与通知重试收尾

目标：完成最后几个后端 Redis 小切片，并形成可测试、可联调、可讲解的项目闭环。

已完成：

- 新增任务点赞接口：`POST /tasks/{taskId}/like`。
- 新增取消点赞接口：`DELETE /tasks/{taskId}/like`。
- 新增点赞状态接口：`GET /tasks/{taskId}/like-status`，返回当前用户是否点赞和当前点赞数。
- 点赞使用 Redis Set `task:likes:{taskId}`，同一用户重复点赞不会重复计数；取消点赞保持幂等。
- 完成点赞自动测试，覆盖重复点赞、重复取消、状态查询、非团队成员拒绝和任务不存在。
- 完成点赞 HTTP 联调，验证 Redis Set 中只保留一份 userId，联调产生的临时用户、团队、任务和 Redis key 已清理。
- 新增通知重试队列 `notification:retry:queue`，使用 `RPUSH + LPOP` 实现 FIFO。
- 新增 `RetryNotificationPayload`、`EnqueueNotificationRetry`、`DequeueNotificationRetry` 和 `ProcessOneNotificationRetry`。
- 新增通知重试后台 worker：服务启动时每 5 秒尝试处理一条重试通知。
- 给通知重试 worker 接入分布式锁 `lock:notification:retry:worker`，避免多实例重复处理。
- `CreateNotification` 在 MySQL 插入失败时会将通知 payload 写入重试队列，等待 worker 后续重试创建。
- 完成通知重试服务级联调：手工 `RPUSH notification:retry:queue` 后，worker 自动创建 MySQL 通知、写入 `notification:unread:{receiverId}`，并清空队列。
- 更新 README，补充点赞接口、通知重试队列、Redis key、测试覆盖和手工验证命令。

验证通过：

```bash
go test ./internal/logic/notification -count=1
go test ./internal/cmd ./internal/logic/task -count=1
go test ./... -count=1
```

前端收尾说明：

- 通知重试属于后台稳定性能力，没有前端入口。
- 点赞接口已完成后端测试和 HTTP 联调，但今天不额外扩展前端 UI，避免在收尾阶段引入新的页面交互风险。

### 2026-06-04：下一步任务规划

目标：项目进入最终收口阶段，优先做文档、演示和可展示性整理。

建议顺序：

1. 整理 README 的项目亮点：Redis String、Set、List、ZSet、锁、限流、延迟队列分别对应哪些业务。
2. 整理一组完整演示 curl：登录、团队、任务、评论、通知、提醒、点赞、重试队列。
3. 可选补前端点赞入口；如果做前端，只接 `like-status`、`POST /like`、`DELETE /like` 三个已验收接口。
4. 最后做一次全量测试和服务级 smoke test，准备项目封版提交。

### 2026-06-04：今天完成了什么

- 整理 README 项目亮点，将 Redis String、Cache Aside、空值缓存、TTL 抖动、Set、List、Sorted Set、Counter、分布式锁和后台 goroutine 映射到具体业务场景。
- 补充工程取舍说明：MySQL 作为真实数据源，Redis 负责可重建缓存、临时状态、高频计数、队列和排行。
- 补充推荐演示顺序，从验证码、登录、团队、在线心跳、任务、评论通知、点赞、延迟提醒到通知重试队列，形成完整展示链路。
- 补充常用 Redis 检查命令，便于演示时观察 `task:*`、`notification:*`、动态流、热门排行、点赞集合和提醒队列。
- 编写全链路 Smoke Test 脚本 `hack/smoke-test.sh`，覆盖全部 28 个 API 端点。
- 前端任务详情抽屉补齐评论（创建/列表）、点赞（切换/状态/计数）、提醒（设置/取消）入口。
- 通知类型图标扩展：新增 `task_commented`、`task_mentioned`、`task_reminder` 图标映射。
- 侧边栏 Redis 图例更新：补充点赞（Set）、通知重试队列（List）、延迟提醒（ZSet）。
- 路线图里程碑 M1-M4 全部标记完成，项目进入毕业状态。
- 验证通过：`go test ./... -count=1`。

### 2026-06-05：今天完成了什么

- 完成 Redis Lua 原子点赞练习：`LikeTask` 使用一次 `EVAL` 完成 `SADD + SCARD`，保持重复点赞幂等和点赞数返回。
- 新增 Redis 事务对比学习函数 `LikeTaskWithRedisTransaction`，练习 `WATCH`、`MULTI`、`EXEC`、`DISCARD` 和重试流程。
- 拆分通知创建职责：`CreateNotificationRecord` 只写 MySQL，`AddUnreadNotificationToRedis` 只维护 Redis 未读集合，为事务组合和最终一致性做准备。
- 完成评论创建的 MySQL 事务改造：`task_comment`、`task_mentioned`、`task_commented` 通知记录在同一事务内提交，事务成功后再写 Redis 团队动态和未读集合。
- 完成通知重试队列可靠队列第一阶段：使用 `LMOVE notification:retry:queue notification:retry:processing LEFT RIGHT` 将消息移动到 processing 队列，避免取出即丢。
- 完成通知重试 ACK 机制：成功和失败路径都会从 processing 删除旧 raw；失败且未超过最大重试次数时，将 `retryCount+1` 的新 payload 重新入队。
- 修复 retry worker 重复入队风险：worker 不再调用带失败入队逻辑的 `CreateNotification`，改为直接调用 `CreateNotificationRecord` 与 `AddUnreadNotificationToRedis`。
- 补充可靠队列测试，覆盖 FIFO、processing 移动、成功 ACK、失败 ACK 后重入队；验证通过：`go test ./internal/logic/notification -count=1` 与 `go test ./... -count=1`。

### 2026-06-06：下一步任务规划

目标：继续围绕可靠队列收口，练习“处理中消息异常恢复”。

建议顺序：

1. 给 `notification:retry:processing` 增加补偿扫描设计：识别 worker 宕机后遗留的 processing 消息。
2. 先实现一个手动恢复函数，将 processing 中的消息搬回 `notification:retry:queue`。
3. 再考虑是否增加死信队列 `notification:retry:dead`，用于超过最大重试次数的 payload。
4. 最后评估是否进入 Redis Stream 消费组，作为 List 可靠队列之后的进阶对比。
