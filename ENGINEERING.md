# TaskFlow 工程规范

本文定义 TaskFlow 后端的目标架构、目录职责、依赖规则和迁移要求，供开发、代码审查和课程实施使用。

架构参考 [GCFeed 工程规范](https://github.com/LeoninCS/GCFeed/blob/main/docs/engineering.md)，领域内按业务功能组织的方式参考 [LuckyDraw domain](https://github.com/pluto-han/LuckyDraw/tree/master/LuckyDraw-domain/src/main/java/org/example/domain)。保留 TaskFlow 的 Go、Gin、PostgreSQL、pgx、sqlc、Cookie Session 和现有部署方式。

**本文描述目标结构。当前源码仍主要采用 `internal/task`、`internal/project` 等业务包，尚未整体迁移。新增本文不代表重构已经完成，也不授权自动移动代码、修改 API、迁移数据库或部署。**

循环任务已作为首个试点迁移到四层：`domain/recurrence`、`application/recurrence`、`infra/persistence/recurrence`、`interfaces/http/recurrence`，调度入口位于 `interfaces/scheduler/recurrence`。共用身份类型和纯日历分别位于 `domain/account`、`domain/planning`。旧 `internal/recurrence` 已移除；其他模块仍按原结构运行。阅读说明见 [循环任务试点](docs/recurrence-ddd.md)。

## 1. 架构原则

采用模块化单体：一个 API 应用内按业务模块划分职责，通过清晰的接口协作。后台调度和外部投递沿用现有运行方式，不因分层而拆成微服务。

后端采用四层：Domain、Application、Infrastructure、Interfaces。目录统一使用 `domain`、`application`、`infra`、`interfaces`。每一层按需要包含对应的业务功能，不要求四层拥有完全相同的子目录。

规则复杂的地方使用领域对象、值对象和领域服务；简单查询可以直接通过应用服务调用仓储。新增抽象必须解决实际的规则、依赖或测试问题。

## 2. 目标目录

```text
cmd/
  api/main.go                       # 配置加载、依赖装配、启动和关闭
  dbsetup/main.go                   # 一次性迁移及 runtime GRANT

internal/
  domain/
    account/                        # 账号、密码策略、邮箱能力相关规则
    session/                        # 会话、撤销、凭据版本相关模型和约束
    task/                           # 任务、优先级、状态和计划日期规则
      entity.go
      errors.go
      repository.go
    project/                        # 项目、成员角色和邀请规则
    recurrence/                     # 循环规则、暂停恢复、发生日期
      entity.go
      errors.go
      repository.go
      calendar.go
    planning/                       # Amsterdam 自然日与跨日计算
    integration/                    # 账号与外部频道的绑定规则

  application/
    account/                        # 注册、改密、邮箱验证和找回用例
    session/                        # 登录、恢复、退出、会话管理
    task/                           # 任务用例、Today 查询、缓存协调
    project/                        # 项目与协作用例
    recurrence/                     # 规则管理、生成实例的用例
    integration/                    # 绑定、解绑及其授权流程
    notification/                   # 提醒领取、投递、确认和重试用例
    mail/                           # 邮件 Outbox 投递与清理用例

  infra/
    config/                         # 环境变量、Secret 文件、启动配置校验
    database/                       # pgx 连接池和数据库连接配置
    persistence/
      account/                      # PostgreSQL 仓储与事务实现
      session/
      task/
      project/
      recurrence/
      integration/
      notification/
      mail/
    cache/                          # go-redis 与缓存存储实现
    discord/                        # Discord HTTP 客户端、签名验证适配
    mail/                           # Resend HTTP 客户端、邮件内容编码
    telemetry/                      # 日志、指标的具体实现

  interfaces/
    http/
      router/                       # 路由、依赖接入、共享中间件配置
      middleware/                   # Cookie、Origin、CSRF、鉴权和限流
      account/                      # Handler、DTO 和错误映射
      session/
      task/
      project/
      recurrence/
      integration/
      notification/
      mail/
    scheduler/                      # 定时触发应用用例、停止和等待

  db/
    queries/                        # sqlc 的 SQL 源文件，保留现有路径
    sqlc/                           # sqlc 生成代码，属于持久化实现细节
  testdb/                           # 隔离数据库 fixture 的安全保护

migrations/                         # 追加式版本迁移
frontend/                           # React 应用，继续使用现有组织规范
test/                               # 跨模块集成、端到端、负载和 Python 测试
scripts/                            # 本地测试与部署工具，使用 Python 编排
deploy/                             # 容器、网关、监控配置
docs/                               # 设计说明和课程总结
devdoc/                             # 版本和课程计划
```

这是目录地图，不是待创建的空目录清单。迁移某个模块时才创建所需文件；不为每个模块机械补齐 entity、service、event 等文件。

## 3. 依赖方向

| 层 | 可以依赖 | 不能依赖 |
| --- | --- | --- |
| Domain | 标准库；确有需要且无环的其他领域类型 | Application、Infra、Interfaces、Gin、pgx、sqlc、go-redis、供应商 SDK |
| Application | Domain、标准库、应用层定义的小接口 | Infra 的具体实现、Interfaces、HTTP Context、数据库驱动类型 |
| Infra | Domain/Application 的接口、数据库与供应商库 | HTTP Handler、路由；通过反向依赖把实现细节传回业务层 |
| Interfaces | Application、必要的领域类型、入口协议库 | 直接调用 SQL、Redis、供应商客户端完成业务操作 |
| 启动装配 | 上述各层 | 在装配代码中实现业务规则 |

运行时会经过仓储实现，但业务源码只依赖仓储接口。例如 Application 调用 `domain/task.Repository`，由启动代码注入 `infra/persistence/task.Repository`。

模块间共享少量 ID 或不可变类型时保持依赖单向。跨业务流程通过应用层协调，不让两个领域服务互相调用。不要创建一个容纳全部模型的通用 `common` 包。

## 4. Domain：按业务功能组织

一个领域功能对应一个包，例如 `domain/task`、`domain/project`。领域目录表达业务概念，避免按数据库表、第三方供应商或前端标签逐个建领域。

Today 是任务的一个日期视图，Discord 和 Resend 是投递技术；它们不自动对应独立领域。领域拆分要看规则和一致性要求。

### 4.1 标准文件职责

| 文件 | 何时使用 | 内容 |
| --- | --- | --- |
| `entity.go` | 有需要保持身份和状态的业务对象 | 实体、构造函数、状态变化方法 |
| `errors.go` | 有稳定的业务失败语义 | 业务错误，不包含 HTTP 状态码或 SQL 错误文本 |
| `repository.go` | 需要通过存储接口读写该领域对象 | 领域所需的仓储能力，参数和结果使用领域类型 |
| `value.go` | 某个值有需要统一维护的规则 | 优先级、角色、日期等值对象，按实际需要增加 |
| `service.go` | 规则涉及多个领域对象，无法自然放进单个实体 | 领域服务，避免成为通用业务杂物箱 |
| `event.go` | 存在实际消费者和一致性设计 | 已发生的领域事实，不直接发送网络请求 |
| 业务名称文件 | 功能进一步增长 | 如 `calendar.go`、`invitation.go`、`policy.go` |

这些优先是同一个 Go 包内的文件。先使用 `domain/task/entity.go`，不要直接照搬 Java 的 `model/entity`、`model/valobj`、`repository`、`service/impl` 多级子包。

### 4.2 领域规则

实体负责合法状态及其变化，例如循环规则的暂停、恢复和周期校验。规则需要业务时间时显式接收时间或日期，方便固定时钟测试。不能依赖服务器默认时区。

领域对象不负责解析 JSON、读取 Cookie、发送邮件或操作 Redis。HTTP DTO 和 sqlc 生成模型都不作为领域对象使用。读取数据库时做必要转换，不通过“创建新对象”的路径意外重设历史时间、状态或版本。

`Task.CanMove`、循环预览、分页游标等面向当前用户或当前请求的结果应由应用返回模型表达。领域数据与返回结果分离时保持既有 JSON 合同。

业务关联不等于必须合并为一个聚合。不要为了项目与任务有关联，就把项目的全部任务加载到一个对象中。

### 4.3 仓储接口

仓储表达业务所需的读写操作，使用 `context.Context`、领域 ID 和领域对象。不要泄露 `pgx.Tx`、`pgtype.UUID`、Redis key 或 sqlc Row。

领域实体的仓储接口放在对应 `domain/<module>/repository.go`。缓存、发送器、用例专属查询和事务协调等接口通常由使用它们的 Application 包定义；不把所有 interface 都塞进 Domain。

不创建泛型 CRUD 基类或空的接口继承层。简单功能可以只有一个仓储和一个应用服务。

## 5. Application：用例编排

应用服务描述用户或后台要完成的一件事：登录、修改密码、接受邀请、生成当天循环实例等。

常见文件为 `service.go`、`command.go`、`query.go`，按体量选择。应用服务使用的小接口及相关应用错误放在使用它们的文件中，例如 `service.go`；不单独创建 `ports.go`。入参和返回模型不带 HTTP 绑定标签；协议转换由 Interfaces 完成。

应用层负责读取必要数据、调用领域规则、协调原子写入，并决定提交后的缓存操作。依赖通过构造函数传入。简单用例可以直接使用仓储，不要求额外建立一个只转发调用的领域服务。

身份来自经过认证的调用方上下文，通过显式参数传入。不要从请求 body 中的 owner/user ID 推导操作身份；Discord 的签名和频道权限规则也必须保留。

缓存是可选优化。数据库写入成功后的缓存失效失败应按既有合同处理，不能把成功写入伪装成未发生。认证、权限、执行账本和 Outbox 不依赖缓存判断正确性。

## 6. Infra：实现外部依赖

### 6.1 PostgreSQL 与 sqlc

`infra/persistence/<module>` 实现仓储和具体事务，转换数据库错误与领域错误。不得把含 SQL、DSN 或凭据的原始错误作为公开响应。

继续使用 pgx 和 sqlc。SQL 源位于 `internal/db/queries`，生成文件位于 `internal/db/sqlc`。这些路径是保留的技术实现目录，领域层和应用层不得导入生成包。

修改查询后运行 `sqlc generate`。只有明确调整生成配置、工具和 CI 的独立变更，才迁移生成路径；不得手工修改生成代码。历史迁移不因目录重构而改写。

### 6.2 缓存与投递

Redis、Discord、Resend 的实现留在 Infra。应用层需要的发送结果使用自己的小型结果类型，避免像现有通知接口那样将具体 Discord 返回类型带入通用投递流程。

供应商客户端固定合法目标、验证 TLS、限制响应大小、遵守 context 和超时，避免在驱动与应用层同时无限重试。错误与指标不包含邮箱、token、Cookie、原始链接或供应商正文。

Resend 的稳定幂等键和 24 小时去重窗口、Discord 的 mentions 限制、已有重试上限都是迁移时要保持的合同。

### 6.3 配置与资源生命周期

配置实现负责环境变量、Secret 文件读取及校验；启用开关关闭时不读取对应凭据。Production 禁止开发假邮箱，公开 origin、可信代理和数据库 TLS 校验不得因重构放宽。

启动代码负责创建和关闭连接池、客户端与后台 worker；router 接收已经组装好的应用依赖，不在 HTTP 请求期间创建这些长期资源。

## 7. Interfaces：HTTP 与后台入口

HTTP 模块采用 `handler.go`、`dto.go` 和必要的 `errors.go`。Handler 负责解析输入、取得已验证身份、调用应用服务、转换 DTO 和写入既有错误响应。

按本地 GCFeed 的结构，模块必须有显式 `Handler` 结构体，通过 `New(service)` 注入应用服务；每个 HTTP 操作是独立 receiver 方法，例如 Create、List、Get、Update、Delete、Preview。不能把整组处理逻辑放进路由注册函数的匿名闭包。共享中间件仍可以使用闭包。

路由在 `interfaces/http/router` 集中注册，模块 Handler 不创建连接池或应用服务。`dto.go` 只放 Request/Response 与响应转换，`errors.go` 放固定的错误映射。响应名统一以 Response 结尾；协议不输出的字段不放进响应 DTO。

JSON 字段名、缺失/null/值的 PATCH 三态、时间格式、游标与错误码保持兼容。不要把领域错误名称直接当作新的公开错误码。

Cookie、Origin、CSRF、限流、请求 ID 和 HTTP 观察放在共享中间件。公开能力链接与环境身份保持分离；GET 不消费验证或重置 token。Discord 专用签名验证不能通过扩大通用豁免来实现。

Scheduler 负责定时触发、批次调度和停止等待。周期业务计算属于 Domain，数据库领取和租约属于持久化实现，重试决策属于应用用例。现有循环扫描、邮件和提醒 worker 按这个职责逐步拆分。

## 8. 事务与并发合同

分层后仍必须保留当前的原子性：

| 操作 | 必须保持的保证 |
| --- | --- |
| 改密或密码重置 | 密码哈希更新、凭据版本变化、会话撤销在同一事务完成；签发会话前重新检查凭据版本 |
| 验证或找回申请 | token 与 email Outbox 一起提交 |
| 消费能力链接 | 用途、到期、消费状态和凭据版本按既有规则检查，并发最多一次成功 |
| 循环生成 | 任务、执行账本、提醒 Outbox 按既有事务一起提交；唯一约束防止同日重复 |
| 任务与规则更新 | 保留 version 乐观锁及冲突响应 |
| 项目操作 | 授权、任务归属和删除限制由 SQL/锁与业务规则共同保证 |
| Outbox 投递 | 保留有界领取、租约和 fencing、重试上限、过期清理及敏感字段擦除 |

应用层定义“哪些工作一起成功”，Infra 实现具体事务。可以使用一个表达原子业务操作的仓储方法；确有多仓储协调需求时再引入用例所需的事务接口。不要为了分层把一笔事务拆成多个独立提交，也不要把 pgx 事务暴露给领域对象。

网络投递和 Argon2 哈希不放进长数据库事务。将校验移到领域对象之后，数据库中的 owner 条件、角色检查、行锁和唯一约束仍然保留，避免先查权限再写入的竞态。

## 9. Go 命名与错误处理

包名及导入别名按 GCFeed 使用“层级前缀 + 业务名”，不再在不同层统一使用 `package recurrence`，也不使用 app、domain、store 等泛化别名。循环试点固定如下：

| 路径 | package 与导入别名 |
| --- | --- |
| `domain/recurrence` | `domainrecurrence` |
| `application/recurrence` | `applicationrecurrence` |
| `infra/persistence/recurrence` | `infrarecurrence` |
| `interfaces/http/recurrence` | `interfaceshttprecurrence` |
| `interfaces/http/router` | `interfaceshttprouter` |
| `interfaces/scheduler/recurrence` | `interfacesschedulerrecurrence` |

例如后续 task 模块迁移时使用：

```go
import (
    domaintask "github.com/pluto-han/taskflow/internal/domain/task"
    applicationtask "github.com/pluto-han/taskflow/internal/application/task"
    infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
)
```

类型使用 `Task`、`Repository`、`Service`、`Handler` 等名称。避免引入 `ITaskRepository`、`TaskServiceImpl` 等 Java 风格命名。新迁移包参照 GCFeed 使用 `New` 构造 Service、Repository、Handler，实体使用 `NewRule` 等业务名称；显式的时钟或调度配置可使用 `NewWithClock`、`NewWithScheduler`。未迁移模块的旧构造函数不随本次试点批量改名。

Handler 方法使用动作和资源名称，例如 CreateRule、ListRules、GetRule、UpdateRule、DeleteRule、PreviewDates。转换函数使用 recurrenceResponseFromResult、recurrenceListResponseFromResult；错误映射使用 writeRecurrenceError。应用返回类型使用 RuleResult、ListResult、PreviewResult，HTTP 返回类型使用 RuleResponse、ListResponse、PreviewResponse。

参照 GCFeed，dto.go 定义协议数据，响应转换放在 handler.go；错误映射可以按已约定拆为 errors.go。当前循环 DTO 为保持跨包 OpenAPI 合同检查而保留导出名称；这不影响 HTTP 数据格式。请求解码、鉴权传输和错误 JSON 保留 TaskFlow 既有安全合同，不照搬参考项目的 JWT 或宽松解码。

使用 `errors.Is`、`errors.As` 保留有意义的错误分类。内部可以包装安全的上下文信息，公开响应仍由入口层统一映射。所有 I/O 传递 context，取消不是撤销已经完成的事务。

## 10. 测试与验证

测试统一以仓库根目录 `test/` 为目标存放位置，核心源码目录只保存实现。当前先完成 account 试点及其密码适配测试；其他模块的既有包内测试、前端测试本轮不移动，不把目标写成已完成。

| 范围 | 目标位置 |
| --- | --- |
| 领域规则 | `test/domain/<module>/` |
| 应用用例 | `test/application/<module>/` |
| 存储、缓存、密码及供应商适配 | `test/infra/<adapter>/`；需要真实数据库的用例放 `test/integration/<module>/` |
| HTTP/调度入口 | `test/interfaces/http/<module>/`、`test/interfaces/scheduler/<module>/` |
| 跨模块/浏览器/架构 | `test/integration/`、`test/e2e/`、`test/architecture/` |
| 前端/Python/负载 | 最终归 `test/frontend/`、`test/python/`、`test/load/`；本轮不移动非 account 文件 |

Go 测试使用显式导入被测包的外部测试包。目录不同，即使 package 名相同也不能访问原包私有成员；不通过导出业务私有方法、复制生产实现、unsafe、链接指令或 overlay 绕过边界。测试原本观察私有实现时，改用公开调用验证相同业务行为；并发、错误分类、取消、数据隔离和敏感字段断言必须保留。

account 的实际落点为 `test/application/account/service_test.go`、`test/interfaces/http/account/handler_test.go`、`test/infra/password/password_test.go` 和 `test/integration/account/{integration,postgres}_test.go`。数据库错误映射通过真实 PostgreSQL 约束验证，必须运行隔离 fixture，不能以无数据库时的 skip 作为通过证据。

使用现有 `make check`、`make test-unit`、`make test-integration`；相关链路需要时执行 `make test-e2e`、`make test-cache`。这些入口已包含 `./test/...`，account 测试无需专门运行器或新增依赖。后续迁移其他模块时同步更新 CI、覆盖率、相对路径和测试发现范围。测试数据库始终使用现有安全 guard 与运行租约，不连接开发库或生产库。

`test/architecture/layers_test.go` 检查 account 已迁移链路的依赖方向、包名与别名，并防止该链路重新出现包内测试。它不能证明全仓库分层或事务行为正确。

## 11. 渐进迁移流程

先选择一个模块，记录现有行为、调用链、依赖与测试基线，再确定领域模型和用例接口。循环规则可以作为候选试点，但不是本文自动批准的实施任务。

每次只迁移一条可验证的调用链：领域规则 → 应用用例 → 持久化适配 → HTTP/调度入口 → 启动装配。保持同一个用例只有一套真实规则实现；过渡适配如有必要，应说明移除条件。

迁移时核对以下当前目录的实际职责：

| 当前代码 | 目标去向 |
| --- | --- |
| `internal/task`、`project`、`recurrence` 等 | 按内容拆到 Domain、Application、Persistence 和 HTTP |
| `internal/session`、`user`、`auth` | 区分账号/会话规则、用例、哈希适配和认证中间件 |
| `internal/mail`、`notification` | 区分投递用例、队列持久化、供应商适配和 HTTP |
| `internal/discord`、`integration` | 区分绑定规则、绑定用例、签名/发送适配和回调入口 |
| `internal/planning` | 纯业务日历迁往 Domain，返回 DTO 独立审查 |
| `internal/cache`、`config`、`telemetry` | 外部实现迁往 Infra；必要接口归使用方 |
| `internal/httpapi`、`server` | 中间件和 HTTP router，依赖改为应用服务 |

不执行一对一整目录搬运。Service 中可能同时存在业务规则、SQL 和协议转换，必须先理解再拆分。

迁移本身原则上不改变数据库结构、公开 API 或产品行为。确需改变时另列变更，不混入“目录整理”。完成后运行相关测试、审查 diff，并写清迁移前后的阅读路径。

前端重构、后端架构迁移和稳定化分别按已批准的版本计划推进。本文不自动调整课程顺序，不新增框架、消息中间件或部署服务。

## 12. 提交与交付要求

提交说明要表达具体迁移的模块、保持的合同和验证结果，不以“采用 DDD”代替实际内容。移除本次产生的旧实现和无用代码，保留不相关用户修改。

不自动提交、推送、打标签、部署或发送真实邮件/Discord。生产保持四服务 CD 白名单、SQL 哈希门禁和独立管理迁移。禁止强制降级、删除卷或用放宽安全检查换取测试通过。不生成 `.sh`；自动编排继续使用 Python。

交付时区分已实现结构、待迁移结构、实际执行的检查和未验证项。工程规范随着已接受的架构决策更新，不能把目标目录写成当前源码的完成声明。
