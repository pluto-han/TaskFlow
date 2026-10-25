# TaskFlow — Codex 驱动开发与 Go 后端学习总计划

> 起点：Lesson 12（2026-10-25）  
> 项目：TaskFlow — 面向个人 VPS 自托管、支持朋友使用的多用户任务管理平台  
> 开发方式：**Vibe Coding with Codex**（Codex 实现；学习者负责需求、审查、测试、决策）  
> 语言：沟通和教学用中文；代码、标识符、提交信息、技术文档可使用英文。  
> 本文是**实施计划**，不是现有代码已完成状态的声明。Codex 必须先检查实际仓库，再调整实现细节。

---

## 0. 项目现状、边界与路线

### 0.1 既有课程背景（Lesson 1–11）

此前课程已讲解/规划：Go module、Gin、`context.Context`、Task CRUD、Handler → Service → Repository、interface 与依赖注入、统一错误处理、优雅关闭、PostgreSQL、Docker Compose、`pgxpool`、数据库迁移、`sqlc`、`pgtype.Timestamptz` 类型映射、Task `version` 乐观锁。

**重要：以上是课程进度，不等于仓库全部实现并通过测试。** Codex 应检查 `go.mod`、`cmd/api`、`internal/`、`migrations/`、`sqlc.yaml`、`compose.yaml` 和测试，建立真实基线。特别检查：

- Repository 接口的实际名称和方法签名（可能是 `Repository` 或 `TaskRepository`）。
- `Task.CreatedAt` 在 sqlc 生成类型中可能是 `pgtype.Timestamptz`；不得假设一定是 `time.Time`。
- `due_date` 的可空映射与 PATCH 缺失/null/值三态处理。
- `version` 乐观锁是否覆盖 PostgreSQL 与内存实现，以及客户端是否提供预期版本。
- 数据库是否已有 000001–000003 Migration、是否存在 dirty 状态；不得擅自 `force`、`down`、`drop` 或删除 volume。
- `go test ./...` 和 `go test -race ./...` 的当前结果。

### 0.2 最终目标

- 多用户注册、登录、退出、会话撤销、用户数据隔离。
- 个人任务、项目、标签、截止日期、筛选、分页、共享项目与成员角色。
- React + TypeScript 响应式 Web UI，手机浏览器可用。
- PostgreSQL 持久化、迁移、测试、备份与恢复。
- Docker Compose + Caddy + HTTPS 部署到 Ubuntu/Debian VPS。
- GitHub Actions CI/CD、结构化日志、Prometheus/Grafana、基础安全与运维。
- Kubernetes/kind/Helm 作为后期**独立学习环境**，不为了学习而强制替换单机生产部署。

### 0.3 技术基线

Go、Gin、PostgreSQL、pgx/v5、sqlc、golang-migrate、Go testing、Docker Compose、React、TypeScript、Caddy、GitHub Actions；测试优先用标准库与现有依赖，确有必要再引入 Testcontainers。认证优先选择**服务端不透明 Session**，不为“看起来现代”而默认引入 JWT。Session 存储 PostgreSQL；Redis 后期按需要引入。

---

## 1. Codex 通用执行协议（每一节必须遵守）

### 1.1 角色分工

**学习者负责：** 选择小节、确认设计与安全敏感变更、阅读代码解释、运行验收、决定是否合并。  
**Codex 负责：** 仓库检查、提出实施方案、分阶段实现、编写测试与文档、运行可执行的检查、报告结果与风险。  
**ChatGPT 负责：** 解释本节概念、审查 Codex 方案和 diff、讲解不理解的代码、提供复盘题。

### 1.2 每节统一工作流

1. **Inspect**：读取 `plan.md`、`AGENTS.md`（若存在）、`go.mod`、相关代码、迁移和测试；汇报真实现状与本节差距。
2. **Plan**：列出文件级改动、API/Schema 变更、兼容性与安全风险、测试计划。对破坏性变更先征求确认。
3. **Implement**：只实现当前小节；遵循现有模块划分；优先小 diff；不要重写不相关模块。
4. **Verify**：运行 `gofmt`、`go test ./...`、适用时 `go test -race ./...`、`sqlc generate`、Migration 与集成测试、前端 lint/build。若环境不支持，明确说明未执行原因。
5. **Explain**：用中文说明请求链路、核心代码、关键 Go/SQL 知识、架构取舍和安全边界。
6. **Deliver**：列出变更文件、运行命令、测试结果、剩余风险、手工验收步骤；给出一个小型思考题；等待用户确认再进入下一节。

### 1.3 不可违反的工程规则

- 不要假定之前的教学示例与实际仓库完全一致；以现有代码为准。
- 不要在未确认的情况下执行破坏性 Migration、删除生产数据、重建卷、覆盖配置或推送部署。
- **不得提交密码、Token、`.env`、私钥**；新增 `.env.example`，生产密钥从环境或秘密管理系统读取。
- API 错误使用稳定错误码，不向客户端泄露 SQL、堆栈或密码哈希。
- 所有数据库访问参数化；跨用户查询必须在 SQL 条件中限定主体/授权范围，而不是仅在前端隐藏。
- 所有 HTTP 请求与数据库操作传递 `context.Context`；共享资源正确关闭；避免 goroutine 泄漏。
- 保持迁移文件向前兼容；已应用的 Migration 原则上不可编辑，新增 Migration 修正。
- SQL/Schema 变化后运行 `sqlc generate` 并审查生成类型；生成文件不得手工改。
- 新增功能必须有成功、错误、边界和权限/并发测试；不能只凭编译通过判定完成。
- 遵循现有代码风格；避免不必要的微服务、消息队列、Redis 或 Kubernetes 复杂度。
- 禁止未经许可自动提交、推送、修改远程资源或生产部署；涉及外部系统/花费先征求确认。

### 1.4 每节固定的 Codex 提示词模板

将下方模板复制给 Codex，替换 `LESSON_ID`；然后附上对应小节的“专用指导”。

```text
你正在开发 TaskFlow（Go + Gin + PostgreSQL + pgx/v5 + sqlc），并担任我的编程实现者和中文技术导师。
请先阅读仓库根目录 plan.md，执行 Lesson LESSON_ID，严格遵循“Codex 通用执行协议”。

工作要求：
1. 先检查真实仓库，不要假设之前的课程代码已全部落地；汇报当前状态。
2. 先给出文件级实施计划、Schema/API 变化、测试方案和风险，再进行实现。
3. 只完成本节范围，尽量不改无关模块。涉及破坏性迁移、生产数据、密钥、部署和远程推送必须先问我。
4. 保留 Handler → Service → Repository 架构；遵循接口契约、context、sqlc、迁移和统一错误响应。
5. 补充测试；运行可执行的检查，不得编造测试结果。
6. 完成后用中文输出：改动摘要、核心调用链、每个关键设计的原因、Go/SQL/安全知识点、测试结果、遗留问题、手动验收步骤和 3 道理解题。
7. 不要自动开始下一节；等待我的确认。
```

### 1.5 完成标准（Definition of Done）

- [ ] 本节范围明确且实现完整；没有偷偷添加未讨论的大功能。
- [ ] 核心成功路径和失败路径测试通过；必要时包含集成测试。
- [ ] 代码格式化，静态检查/构建没有新增错误。
- [ ] 迁移可在**全新数据库**上重放；已有数据库升级路径清晰。
- [ ] API/数据库变化有文档或示例请求。
- [ ] 安全和权限边界已测试；没有敏感信息泄露。
- [ ] Codex 已解释关键实现；学习者能回答至少两道复盘题。

---

## 2. 分阶段里程碑

| 阶段 | 小节 | 交付结果 | 上线条件 |
|---|---|---|---|
| A. 身份与隔离 | 12–15 | 注册、登录、Session、用户私有任务 | 必须完成权限测试 |
| B. 核心产品 | 16–19 | 项目、任务体验、API 文档、前端 MVP | 可供内部试用 |
| C. 协作与安全 | 20–22 | 成员权限、集成测试、安全加固 | 公网开放前必须验收 |
| D. 部署与运维 | 23–26 | Docker、HTTPS、备份、CI/CD、监控 | 正式 VPS 上线 |
| E. 云原生拓展 | 27–28 | Kubernetes、Helm、发布演练 | 可选学习，不阻塞上线 |

> 每一节有依赖关系。若 Codex 检查发现前置功能缺失，先报告差距；经确认可补最小前置修复，不应静默跨节实现。

---

## Lesson 12 — 用户注册、密码哈希与 User 模块

**目标：** 新用户可以安全注册；密码只以强密码哈希保存；重复邮箱得到可预期响应。**本节不实现登录，也不开放公网注册。**

**给 Codex 的专用指导（可直接粘贴）：**

```text
执行 Lesson 12：实现用户注册。先审查现有数据库版本、sqlc 类型和 API 错误结构。
1. 新增 users 表 Migration（UUID 主键、规范化 email 唯一约束、password_hash、created_at）；不要修改已应用迁移。使用实际下一个迁移序号。
2. 在 internal/user 中建立 User domain model、RegisterRequest、UserResponse、Repository、Service、Handler；不要把 password_hash 暴露给 JSON 响应。
3. 在 internal/auth 或独立密码模块实现 Argon2id：每次随机 salt，编码算法/参数/salt/hash；VerifyPassword 必须限制可解析参数的内存/时间上限，并采用常量时间比较。测试正确密码、错误密码、不同 salt、格式损坏、异常参数。
4. 添加 users.sql 的 CreateUser、GetUserByEmail、GetUserByID；sqlc generate；根据实际 pgtype.UUID / Timestamptz 类型实现映射，不得臆测类型。
5. 实现 POST /auth/register：校验请求大小、邮箱、密码字节长度与基本策略；规范化邮箱；生成安全 UUID；数据库 UNIQUE 约束处理并发重复注册；返回 201 和不含敏感字段的 DTO。
6. 测试注册成功、无效输入、重复邮箱、并发重复注册、密码不明文、数据库异常；对外错误不泄露 SQL。
7. 解释密码 Hash 与 Encryption、Salt、Argon2id 参数、唯一约束为何优于先查再插。
```

**知识点与重点：** Authentication vs Authorization；密码哈希与加密；Argon2id/salt/参数调优；`crypto/rand`；UUID；DTO 与数据泄露；PostgreSQL `UNIQUE`、SQLSTATE `23505`；并发注册竞争；`errors.Is/As`；`pgtype.UUID` 与 `time.Time` 类型映射。  
**验收：** 注册返回 201；数据库只存密码哈希；重复邮箱被拒绝；响应不含哈希；`go test ./...` 通过。  
**复盘问题：** 为什么不使用 SHA-256 直接保存密码？为什么不能只靠 `SELECT` 检查邮箱重复？为什么要单独定义 `UserResponse`？

---

## Lesson 13 — 登录、服务端 Session、登出与认证 Middleware

**目标：** 通过邮箱和密码登录；使用可撤销的不透明随机 Token；受保护路由能获取当前用户；登出立即失效。

**给 Codex 的专用指导：**

```text
执行 Lesson 13：在 Lesson 12 基础上实现登录和 Session。
1. 新增 sessions 表 Migration：session id、user_id FK、token_hash UNIQUE、created_at、expires_at、revoked_at（或等价撤销策略），建立必要索引；不要存储明文 Token。
2. 实现 POST /auth/login：规范化邮箱、查用户、验证 Argon2id；错误邮箱和错误密码对外使用统一错误语义；考虑时间差与账户枚举风险；加入登录速率限制的最小实现或明确的上线阻断项。
3. 使用 crypto/rand 生成至少 256-bit 随机不透明 Token，返回一次给客户端；数据库只保存 Token 的 SHA-256 摘要。注意 Token 是高熵随机值，与密码哈希的威胁模型不同。
4. Gin Middleware 从 Authorization: Bearer <token> 解析 Token，计算摘要查询 Session，检查到期/撤销和用户有效性，把强类型 user ID 放入请求上下文；不要让客户端通过自填 user_id 冒充身份。
5. 实现 GET /auth/me、POST /auth/logout；登出撤销当前 Session；缺失、无效、过期、撤销 Token 均返回 401；严格区分 401/403。
6. 明确当前是 Bearer API 方案，未来浏览器 Cookie 方案须补 SameSite、Secure、HttpOnly、CSRF；不要无说明混用。
7. 编写完整 Handler/Service/Repository 测试和真实数据库集成测试，验证 Token 不明文入库、登出立即生效、过期拒绝、不同用户 Session 隔离。
```

**知识点与重点：** Authentication、Session vs JWT、Bearer Token、`crypto/rand`、高熵 Token 的 SHA-256 摘要、Gin Middleware、请求上下文、401 vs 403、Session 生命周期、Token 轮换、过期与撤销、Cookie/CSRF 取舍。  
**验收：** 登录拿到 Token；`/auth/me` 返回当前用户；登出后同一 Token 返回 401；数据库中不出现明文 Token。  
**复盘：** 为什么 Session Token 可以用 SHA-256 存储，而用户密码不行？为什么 Token 不应放在 URL 查询参数？

---

## Lesson 14 — Task 所有权与跨用户数据隔离（上线阻断项）

**目标：** 每条 Task 都有明确所有者；用户只能操作自己的任务；旧任务安全迁移。

**给 Codex 的专用指导：**

```text
执行 Lesson 14：为 tasks 引入 owner_id/user_id，并封闭所有跨用户访问路径。
1. 检查现有任务数据量与归属。提出旧任务迁移方案：测试数据可经批准清理；真实数据必须先确定归属或采用分阶段迁移，禁止擅自把所有旧任务归到某个用户或删除。
2. 设计向后兼容 Migration：先添加可空 owner_id + FK + 索引；明确回填策略后再设置 NOT NULL；若环境为空可采用简化迁移，但说明选择。
3. 修改 sqlc SQL：List/Get/Update/Delete 均将 authenticated user ID 作为查询条件；Create 从认证上下文赋予 owner_id，禁止接受客户端传入的 owner_id。
4. Service/Repository API 显式传递当前用户身份，不能依赖前端过滤；对无权访问的任务返回不泄露存在性的 404（或统一的明确策略）。
5. 所有 /tasks 路由加认证 Middleware；修复任何可绕过的旧路由；保持乐观锁 version 校验。
6. 测试两个用户：A 不能 list/get/update/delete B 的任务；创建时不能伪造 owner_id；旧任务迁移无数据丢失；未认证请求返回 401。
```

**知识点与重点：** 多租户数据隔离、IDOR/BOLA、外键、索引、数据回填、Expand–Migrate–Contract、认证主体传播、资源授权、404/403 取舍、SQL 条件级防越权。  
**验收：** A/B 两个用户交叉访问均失败；任务列表只包含自己的任务；未登录无法调用 Task API。  
**复盘：** 为什么不能先查 Task 再只在前端隐藏？为什么 Migration 要考虑历史数据？

---

## Lesson 15 — 任务 API 完善：PATCH 三态、版本冲突、分页与过滤

**目标：** Task API 满足真实前端使用要求，更新语义清晰，错误可预测。

**给 Codex 的专用指导：**

```text
执行 Lesson 15：完善已有 Task API，不要重写整个模块。
1. 审查现有 PATCH DTO；为 due_date 实现缺失/显式 null/具体时间的三态解析（可自定义 OptionalNullable 类型或明确的 JSON 字段检测），确保 null 清除而缺失不修改。
2. 验证空 PATCH、非法 priority、非法日期、过去日期的产品策略；不凭空禁止过去日期，先记录并选择策略。
3. 审查乐观锁 version：客户端提供 expected version，SQL 原子 WHERE version=? + version=version+1；冲突返回 409；禁止在 Service 中重读最新版本后静默覆盖。
4. GET /tasks 增加有界 limit、稳定 cursor/keyset pagination（created_at + id）、completed/priority/due_date 过滤；每个 SQL 查询必须包含当前 user_id。
5. 添加必要索引；解释 EXPLAIN 和索引权衡；为分页稳定性、跨用户隔离、并发版本冲突、null 清除写测试。
6. 更新 API 文档与 curl 示例；保持旧 API 兼容或明确迁移方案。
```

**知识点与重点：** PATCH 语义、JSON 三态、指针与零值、乐观锁、原子 SQL、409、稳定排序、Cursor Pagination、索引、查询计划。  
**验收：** `due_date:null` 可清除；缺失不修改；旧版本 409；分页无重复/遗漏（固定数据集）；不同用户结果隔离。  
**复盘：** 为什么 `*time.Time` 无法区分 JSON 缺失与 null？为什么 OFFSET 大分页可能有问题？

---

## Lesson 16 — 项目（Project）与任务归类

**目标：** 用户可以创建 Project，把任务组织到项目中，为协作做准备。

**给 Codex 的专用指导：**

```text
执行 Lesson 16：增加 Project domain。
1. 设计 projects 表（UUID、owner_id、name、description、created_at、updated_at 等）与 tasks.project_id 的可空 FK；迁移旧任务为未归类状态，禁止丢失。
2. 增加 Project Repository/Service/Handler，CRUD API；项目列表按当前用户过滤；任务创建/移动时验证目标 Project 的访问权限。
3. 先仅允许项目所有者访问，不在本节引入成员角色；删除项目时明确任务处理策略（拒绝、解绑或级联），必须先询问我，默认不做级联删除。
4. sqlc generate；测试项目归属、非法 project_id、项目删除边界、跨用户移动任务、乐观锁兼容。
```

**知识点与重点：** 一对多关系、FK、`ON DELETE` 策略、聚合边界、Repository 组织方式、事务必要性、API 资源建模。  
**验收：** 项目 CRUD 正常；任务可以归类；不能将任务移动到别人的项目。  
**复盘：** `ON DELETE CASCADE` 有哪些风险？项目所有权和任务所有权如何协调？

---

## Lesson 17 — API 规范、OpenAPI、错误码与开发体验

**目标：** 前后端可以依据稳定 API 契约协作。

**给 Codex 的专用指导：**

```text
执行 Lesson 17：建立 API 契约与文档。
1. 审查所有路由、请求 DTO、响应 DTO、状态码；统一错误结构和稳定 code；梳理 400/401/403/404/409/422/429/500 的使用。
2. 生成或手写可维护的 OpenAPI 3.x 文档，覆盖 auth/tasks/projects；增加示例请求、认证说明、分页和版本冲突语义。
3. 增加 /health/live 与 /health/ready（ready 检查关键依赖但不泄露配置）；避免健康检查执行昂贵查询。
4. 加入基础请求日志、request_id、panic recovery；敏感头、密码、Token 不进入日志。
5. 增加 API 合约测试或文档一致性检查，避免只生成漂亮但过期的文档。
```

**知识点与重点：** REST、HTTP 状态码、OpenAPI、契约优先、DTO、错误码、request ID、健康探针、结构化日志与敏感信息脱敏。  
**验收：** 文档覆盖现有接口；示例可执行；错误结构一致；日志无凭据。  
**复盘：** readiness 与 liveness 有什么区别？为什么错误码需要稳定？

---

## Lesson 18 — React + TypeScript 前端基础与登录流程

**目标：** 建立可运行的前端，用户可以注册、登录、查看个人资料并退出。

**给 Codex 的专用指导：**

```text
执行 Lesson 18：新增 frontend/，使用 React + TypeScript + Vite；先检查项目是否已有前端，避免重复初始化。
1. 实现路由、基础 Layout、API Client、类型定义、环境配置、错误/加载状态；尽量根据 OpenAPI 契约组织类型。
2. 实现注册、登录、/me、退出；优先选择安全的同源 Cookie Session 方案时须先协调后端 HttpOnly/Secure/SameSite/CSRF 设计；如果暂用 Bearer Token，不要把长期 Token 放 localStorage，明确开发阶段限制与迁移路径。
3. 实现受保护路由、会话失效后的 UI 状态、表单验证、可访问性标签、移动端布局。
4. 为关键交互写前端测试；运行 lint、typecheck、build；提供本地启动命令和代理/CORS 说明。
5. 不在本节实现完整任务 UI，保持范围可控。
```

**知识点与重点：** React 组件、Hooks、TypeScript 类型、SPA 路由、API Client、CORS、Cookie、CSRF、表单、客户端状态、可访问性。  
**验收：** 浏览器完成注册/登录/退出；刷新后认证状态正确；无 Token 泄露到前端持久存储。  
**复盘：** 为什么 HttpOnly Cookie 不能被 JS 读取？为什么 Cookie 认证需要考虑 CSRF？

---

## Lesson 19 — 日常可用的任务前端 MVP

**目标：** 可以在浏览器中日常管理任务和项目。

**给 Codex 的专用指导：**

```text
执行 Lesson 19：在现有前端实现任务管理 MVP。
1. 任务列表、创建/编辑/删除、完成状态、优先级、截止日期、项目筛选、分页/加载更多。
2. 展示 loading/empty/error/optimistic state；发生 409 冲突时提示刷新或比较版本，不静默覆盖。
3. UI 对 null due_date 的清除操作应调用正确 PATCH 三态协议；表单日期需处理本地时区与 UTC 转换。
4. 添加响应式布局、键盘可访问性、确认删除、必要的交互测试。
5. 不做未经要求的大型设计系统或复杂拖拽看板；先确保功能可靠。
```

**知识点与重点：** React 状态管理、数据获取、乐观 UI、错误恢复、时间与时区、响应式布局、交互测试。  
**验收：** 不用 curl 就能完成日常任务 CRUD；409 有明确反馈；手机浏览器可用。  
**复盘：** 乐观 UI 与数据库乐观锁有何区别？为什么时间要区分本地显示与 UTC 存储？

---

## Lesson 20 — 共享项目、成员角色与邀请

**目标：** 允许朋友协作，但严格控制成员权限。

**给 Codex 的专用指导：**

```text
执行 Lesson 20：实现共享 Project 与 RBAC。
1. 先提交权限矩阵供我确认：Owner、Member（必要时 Viewer）；定义查看、编辑任务、邀请、移除、删除项目等权限。
2. 设计 project_memberships（project_id、user_id、role、唯一约束）与邀请流程；邀请令牌需高熵、有限期、可撤销；避免公开用户邮箱。
3. 在 Service 与 SQL 中落实权限，不只靠 UI；跨项目、跨用户、已移除成员、最后一名 Owner 等边界必须处理。
4. 必要时用事务保证邀请接受、成员变更、角色变更的原子性；并发操作要测试。
5. 测试权限矩阵的允许/拒绝路径，尤其 IDOR、越权邀请、移除后旧 Session 的权限变化。
```

**知识点与重点：** RBAC、ACL、Membership、邀请 Token、唯一约束、事务、TOCTOU、权限矩阵、最小权限原则。  
**验收：** Owner/Member 权限正确；非成员不能访问项目；被移除成员立即失去权限。  
**复盘：** 认证通过为什么仍不能访问项目？如何避免权限检查与写入之间的竞态？

---

## Lesson 21 — 集成测试、并发测试与可靠性基线

**目标：** 从“本机能运行”升级为“自动验证关键行为”。

**给 Codex 的专用指导：**

```text
执行 Lesson 21：建立测试金字塔与真实 PostgreSQL 集成测试。
1. 保留单元测试，新增使用独立测试 DB 或 Testcontainers 的集成测试；禁止测试连接生产数据库。
2. 测试从全新数据库应用 Migration、sqlc 查询、Session 过期/撤销、权限隔离、版本冲突、事务回滚。
3. 并发测试必须避免测试代码自身 data race；使用 go test -race；重复运行关键并发场景。
4. 添加 HTTP API 端到端测试（注册→登录→创建任务→跨用户拒绝→退出）。
5. 建立稳定测试数据清理策略，避免测试之间共享脏状态；输出覆盖率但不以百分比代替质量。
```

**知识点与重点：** 单元/集成/E2E 测试、Testcontainers、测试隔离、race detector、事务回滚、fixture、测试金字塔、可重复性。  
**验收：** CI 可在空数据库重复跑通关键流程；并发和权限测试稳定。  
**复盘：** Mock 测试为什么无法替代真实数据库测试？`-race` 能否检测数据库层 Lost Update？

---

## Lesson 22 — 公网前安全加固与威胁建模（上线阻断项）

**目标：** 找出并修复上线前高风险问题。

**给 Codex 的专用指导：**

```text
执行 Lesson 22：对当前 API 和前端做上线前安全审查。
1. 输出威胁模型：资产、入口、攻击者、信任边界、滥用场景；重点认证、授权、会话、注册滥用、文件/日志/密钥。
2. 检查并落实：HTTPS 策略、Cookie/CSRF 或 Bearer 方案、CORS 白名单、请求体大小限制、超时、限流、登录防暴力破解、密码策略、错误脱敏、安全响应头。
3. 检查 SQL 参数化、跨用户授权、IDOR、XSS、依赖漏洞、Secrets 泄露、日志中的 PII、数据库最小权限。
4. 为高风险发现写回归测试；无法在本节修复的高风险项必须列为阻断上线。
5. 不要进行未授权的外部扫描；所有测试仅针对本地或明确授权环境。
```

**知识点与重点：** OWASP 风险、威胁建模、XSS/CSRF/CORS、暴力破解、限流、最小权限、Secret 管理、依赖审计。  
**验收：** 无已知高危未处理项；跨用户隔离和会话撤销可自动测试；安全配置有文档。  
**复盘：** CORS 是否等于授权？为什么限流不能只依赖前端？

---

## Lesson 23 — Docker 多阶段构建与本地生产模拟

**目标：** 把 API、前端和 PostgreSQL 组织成可复现部署单元。

**给 Codex 的专用指导：**

```text
执行 Lesson 23：容器化并做本地生产模拟。
1. Go API 多阶段 Dockerfile（固定基础镜像版本、非 root 用户、最小运行时）；前端独立构建/静态服务。
2. 编写开发与生产 Compose 配置，使用 healthcheck、持久卷、内部网络、restart policy、资源限制；数据库端口不得默认暴露公网。
3. Migration 采用独立的一次性部署步骤/Job，避免多个 API 实例同时迁移；失败时阻止新版本上线。
4. 环境变量示例和 Secrets 注入；不把数据库密码写进镜像或 Git。
5. 测试 clean build、容器启动、健康检查、重启后持久化、优雅关闭；记录镜像大小和构建缓存策略。
```

**知识点与重点：** Dockerfile、多阶段构建、镜像层、Compose 网络、volume、容器健康检查、非 root、不可变镜像、迁移顺序。  
**验收：** 新环境可按 README 启动；数据库数据在容器重启后保留；API 不直接公开 DB。  
**复盘：** 为什么 Migration 不应由每个 API 副本自动并发执行？为什么容器内 root 有风险？

---

## Lesson 24 — VPS、Caddy HTTPS 与首次上线

**目标：** 在用户的 Ubuntu/Debian VPS 安全运行 TaskFlow。

**给 Codex 的专用指导：**

```text
执行 Lesson 24：准备 VPS 部署方案；任何远程执行前先请求明确授权。
1. 先收集/确认域名、DNS、服务器资源、操作系统版本、SSH 访问方式、是否已有服务占用 80/443、备份需求；不要猜测 IP/域名。
2. 给出最小权限部署用户、SSH key、系统更新、防火墙（22/80/443 按需）、Docker/Compose 安装方案；防止误锁 SSH。
3. 配置 Caddy 自动 HTTPS、反向代理、前端静态资源、API 路由；只公开必要端口，PostgreSQL 仅内网可访问。
4. 部署前确认环境密钥、Migration 备份、回滚步骤；部署后验证 TLS、健康、注册/登录、用户隔离、持久化。
5. 输出部署 Runbook、故障排查和回滚命令；不在未授权情况下修改真实 VPS。
```

**知识点与重点：** DNS、TLS/HTTPS、Caddy、Reverse Proxy、Linux 服务、UFW/nftables、SSH、最小权限、部署 Runbook、回滚。  
**验收：** 域名 HTTPS 可访问；数据库不暴露；核心流程通过；有明确回滚方法。  
**复盘：** 反向代理与应用服务器分别负责什么？为什么不能直接把 5432 暴露到公网？

---

## Lesson 25 — PostgreSQL 备份、恢复与运维

**目标：** 发生误删、升级失败或 VPS 故障时有可验证的恢复方案。

**给 Codex 的专用指导：**

```text
执行 Lesson 25：设计并验证备份与恢复。
1. 使用 pg_dump/pg_restore 设计定期逻辑备份；区分数据库备份、Compose volume、应用配置和 Secrets；说明 RPO/RTO 目标。
2. 备份文件需加密、限制权限并异地保存；不要把备份上传到公开仓库；先询问目标存储和保留策略。
3. 建立自动化备份脚本、保留策略、失败告警；不要假定 cron/存储凭据已配置。
4. 在独立测试数据库执行完整恢复演练，验证用户、任务、项目、Session 处理和 Schema 版本；禁止在生产库上直接覆盖恢复。
5. 输出恢复 Runbook，记录恢复时间与已知限制。
```

**知识点与重点：** RPO/RTO、`pg_dump`、`pg_restore`、一致性快照、备份加密、异地备份、恢复演练、灾难恢复。  
**验收：** 能从备份恢复到隔离环境；验证数据完整性；备份失败有可见信号。  
**复盘：** 为什么“备份成功”不等于“可以恢复”？逻辑备份与磁盘快照有什么不同？

---

## Lesson 26 — GitHub Actions CI/CD、可观测性与告警

**目标：** 让发布更可靠、问题更容易定位。

**给 Codex 的专用指导：**

```text
执行 Lesson 26：增加自动化验证、受控部署和可观测性。
1. GitHub Actions：gofmt 检查、go test、race（资源允许时）、sqlc 生成一致性检查、Migration 测试、前端 lint/typecheck/build、依赖审计；使用最小权限 token。
2. 构建带版本标签的镜像；部署必须有审批/环境保护，不能在每次 push 后直接无确认覆盖生产。
3. Go API 接入结构化日志、Prometheus 指标（请求量、延迟、错误率、DB pool 指标）；不要以用户 ID、邮箱、Token 作为高基数或敏感标签。
4. Grafana Dashboard 和基础告警（不可用、5xx 激增、延迟、磁盘/备份失败）；监控面板需认证，不公开敏感指标。
5. 编写发布与回滚 Runbook，做一次非生产环境的失败发布演练。
```

**知识点与重点：** CI/CD、GitHub Actions、最小权限、镜像标签、发布审批、Prometheus、Grafana、指标基数、SLI/SLO、告警疲劳、日志关联。  
**验收：** PR 自动验证；生产部署需要批准；关键故障有监控；失败发布可回滚。  
**复盘：** 为什么 metrics label 不能随意放 user_id？CI 与 CD 有何区别？

---

## Lesson 27 — Kubernetes 基础：kind 中部署 TaskFlow

**目标：** 在不影响 VPS 生产环境的前提下学习 Kubernetes。

**给 Codex 的专用指导：**

```text
执行 Lesson 27：只在本地 kind 或明确授权的测试集群部署 TaskFlow。
1. 解释 Pod、Deployment、Service、ConfigMap、Secret、Ingress/Gateway、Namespace、Readiness/Liveness、Resource Requests/Limits。
2. 为无状态 Go API 编写 Kubernetes 清单，设置安全上下文、探针、资源限制和副本数；数据库优先使用隔离的外部测试 PostgreSQL，避免在本节自行设计生产级数据库 Operator。
3. 使用 kind 启动、部署、查看日志、扩缩容、滚动更新、回滚；验证 Session 与 API 多副本兼容性。
4. 不迁移现有生产服务；记录 Docker Compose 与 Kubernetes 的复杂度和适用场景差异。
```

**知识点与重点：** Kubernetes 控制循环、Pod/Deployment/Service、探针、Secret、滚动更新、无状态设计、配置与密钥分离。  
**验收：** kind 上 API 两副本可用；滚动更新成功；健康探针有效。  
**复盘：** Deployment 和 Pod 有什么区别？为什么 Session 存 PostgreSQL 比存在进程内存更适合多副本？

---

## Lesson 28 — Helm、GitOps 思维与项目收尾

**目标：** 整理可复用部署配置，形成可展示的完整工程项目。

**给 Codex 的专用指导：**

```text
执行 Lesson 28：在测试环境引入 Helm 和发布演练。
1. 将 K8s manifests 组织为 Helm Chart，使用 values 管理镜像版本、资源限制、Ingress、配置；Secret 不写明文 values。
2. 验证 helm lint/template/install/upgrade/rollback；提供测试环境发布流程，介绍 GitOps 思路但不强制安装 Argo CD。
3. 完善 README、架构图、API 文档、开发指南、部署/恢复 Runbook、测试矩阵、安全边界、已知限制。
4. 总结 Go、SQL、认证、并发、测试、Docker、CI/CD、监控、Kubernetes 的知识地图，生成项目展示材料和简历项目描述草稿。
5. 最终交付需说明哪些能力已真实测试，哪些仅是设计或可选实验。
```

**知识点与重点：** Helm、模板、配置分层、GitOps、回滚、文档工程、可复现交付、技术表达。  
**验收：** 测试环境 Helm 安装与回滚成功；README 可引导新开发者启动；项目能力描述可验证。  
**复盘：** Helm 与 Docker Compose 分别解决什么问题？为什么不应该为了学习 K8s 直接替换运行稳定的 VPS 部署？

---

## 3. Codex 每节交付报告格式

要求 Codex 在每节结束时使用以下结构，避免“代码改完了”却没有学习反馈。

```markdown
## Lesson XX 完成报告
### 1. 实际完成的功能
### 2. 修改文件清单及原因
### 3. 架构与请求链路（从 HTTP 到 SQL）
### 4. 本节 Go / SQL / Web / Security 知识点
### 5. 核心代码讲解（关键 3–5 段）
### 6. 测试命令、实际结果与未执行项目
### 7. 安全风险、兼容性和技术债
### 8. 本地手动验收步骤
### 9. 三道理解题（先不给答案）
### 10. 下一节前置条件
```

## 4. 学习者的最小参与要求（避免“只会提示词，不懂代码”）

每节至少完成以下四件事：

1. **画出调用链**：例如 `HTTP → Gin Middleware → Handler → Service → Repository → sqlc → PostgreSQL`。
2. **解释关键设计**：为什么选这个接口、事务、索引、认证方案或数据模型。
3. **自己跑验收**：至少一个成功案例、一个错误案例、一个边界/权限案例。
4. **复盘 3 道题**：用自己的话解释；不理解的代码交给 ChatGPT 逐行讲解。

建议每节提交一个小 PR 或清晰的 Git commit，避免把多个小节混成一个难以审查的大变更。

## 5. 推荐执行顺序与下一步

**立即开始：Lesson 12。** 先让 Codex 执行 Inspect 和 Plan；确认现有仓库状态与 Migration 版本，再实施注册模块。Lesson 12 完成后，按照 13 → 14 → 15 的顺序优先实现认证和隔离；**Lesson 14 未完成前，不允许公开 Task API**。

可直接发给 Codex 的第一句话：

```text
请阅读仓库根目录的 plan.md，开始 Lesson 12。先执行 Inspect + Plan，告诉我现有代码与 Lesson 12 的差距、预计修改的文件、Migration 方案、测试计划和安全风险；确认后再实施。不要一次性做 Lesson 13 或后面的内容。
```

---

*本计划是可演进的路线图。每节的实现细节必须根据实际仓库和验收结果调整，不应机械套用教学示例。*
