# Lesson 13 — 登录、Session 与退出

## 1. 范围与完成的功能

新增 `POST /auth/login`、`GET /auth/me`、`POST /auth/logout`。
只使用 `Authorization: Bearer <token>`，不设置 Cookie、不实现 JWT。
每次登录创建独立 Session，固定有效期 24 小时，无滑动续期；退出只撤销当前会话。
Token 由 32 字节 `crypto/rand` 随机数编码为 43 字符 Base64URL，数据库只保存
该规范化字符串的 SHA-256 摘要。登录成功时返回一次 Token。

本节不实现 Task 所有权，也不保护现有 Task 路由；这是 Lesson 14 的工作。
保持本机监听，不能公开服务。

## 2. 修改文件

- `migrations/000005_create_sessions.{up,down}.sql`：新增 sessions 表与索引。
- `internal/db/queries/sessions.sql`：创建、有效会话查询、按会话和用户撤销。
- `internal/db/sqlc/`：由 sqlc 生成，未手工修改。
- `internal/session/service.go`：登录、Token、强类型身份与业务接口。
- `internal/session/postgres_repository.go`：pgtype 映射与错误包装。
- `internal/session/handler.go`：HTTP、认证 Middleware、统一路由挂载。
- `internal/session/limiter.go`：线程安全、有界的最小登录限流。
- `internal/session/*_test.go`：单元、HTTP、并发及真实数据库测试。
- `internal/user/service.go`：导出既有 NormalizeEmail，让注册和登录共享策略。
- `cmd/api/main.go`：依赖注入及挂载。
- `scripts/test-lesson13.sh`：一次性本地 PostgreSQL 测试环境。
- `scripts/test-lesson12.sh`：固定迁移目标为版本 4，保留上一课验收契约。

## 3. 请求调用链

登录：HTTP → 限流 → Handler（4 KiB JSON）→ Service（邮箱规范化）→
User Repository → Argon2id 验证 → crypto/rand → Session Repository → sqlc INSERT。

受保护请求：Authorization → Middleware → Token 结构验证 → SHA-256 →
sqlc 查询未撤销、未过期且用户存在的 Session → 类型化 context → Handler。

退出：认证得到 Principal → Repository → 原子 UPDATE，同时限定 session ID、
user ID、未撤销、未过期。只有更新一行才返回 204。

## 4. Go / SQL / 安全知识点

- Authentication 确定身份；Authorization 决定权限。401 表示缺少有效认证；
  403 用于已认证主体没有操作权限。本节没有角色授权端点，因此不人为引入 403。
- Session Token 具有 256-bit 随机熵，SHA-256 摘要适合做索引与泄露隔离。
  人类密码通常低熵，必须使用昂贵的 Argon2id，不能照搬 Token 的算法。
- Token 不包含可解码的用户身份，没有 JWT 签名声明；服务端数据库是真实状态。
  因此每次受保护请求要查库，能及时发现撤销。
- `context.Context` 贯穿查询；私有 context key 避免字符串键冲突，`UserID` 是独立类型。
- `expires_at > now()` 严格排除到期时刻；数据库负责时钟与有效状态判断。
- UNIQUE 防止摘要重复；用户 FK 防止孤立会话。删除用户时级联删除其会话。
  本节没有提供删除用户 API。
- 不打印密码、Token、Authorization 或完整 Session 对象；错误响应不返回 SQL。
  Gin 当前依赖的恢复日志会遮蔽 Authorization；正常客户端仍禁止把 Token 放在 URL。

## 5. 核心代码讲解

### 登录与不存在的邮箱

`Service.Login` 使用 `user.NormalizeEmail`，与注册完全一致。
不存在的邮箱使用启动时生成的 dummy hash，也执行一次 Argon2id 验证，最后仍强制拒绝。
错误邮箱/错误密码返回相同的 401 与消息。此措施缩小主要计算时间差，不承诺严格恒时：
数据库查询、调度和无效格式的早期拒绝仍可能有差异。

### 摘要与响应分离

Token 只在登录函数中生成和返回。Repository 接收 `[32]byte` 摘要，根本没有明文 Token 参数。
登录 DTO 包含 `access_token`、`token_type`、`expires_at`；`/me` 使用 Lesson 12 的 UserResponse。
所有本节 auth 路由响应带 `Cache-Control: no-store` 和 `Pragma: no-cache`。

### 会话查询与撤销

`GetActiveSession` JOIN users，WHERE 限定摘要、revoked_at IS NULL、expires_at > now()。
Middleware 拒绝缺失/重复 Authorization、多余字段、错误方案、非规范 Token。
它把 Principal 放进 request context，Handler 不从 body/query 接受身份。

`RevokeSession` 原子更新并返回影响行数。两个并发退出请求只有一个能更新；
第二个返回 401。退出完成后的新请求必须重新查库，会拒绝旧 Token。
已经在退出之前通过认证的在途请求不保证被中途取消。

### 登录限流

进程内固定一分钟窗口，最多 30 个登录请求（包含失败请求），满额返回 429，
`Retry-After: 60`。互斥锁保护计数，无后台 goroutine、无不受控 IP map，也不信任代理头。
密码模块已有两路并发上限；满载返回 503 `AUTH_BUSY`，Retry-After: 1。
固定窗口边界可能形成突发，重启会清零，多实例之间不共享；全局配额也会被一个调用者耗尽。
这只是本地学习基线，公网前需要部署级限流、可信代理和账号/IP 策略。

## 6. API 与测试

| 请求 | 正常响应 | 主要失败 |
| --- | --- | --- |
| POST /auth/login | 200，Token/类型/到期时间 | 401 UNAUTHORIZED、429 LOGIN_RATE_LIMITED、503 AUTH_BUSY |
| GET /auth/me | 200，id/email/created_at | 401 UNAUTHORIZED |
| POST /auth/logout | 204，无响应体 | 401 UNAUTHORIZED |

登录须 Content-Type: application/json。错误 JSON/类型为 400 INVALID_REQUEST；
超过 4096 字节为 413 REQUEST_TOO_LARGE；凭据缺失、无效邮箱、错误密码为 401。
数据库异常为通用 500 INTERNAL_ERROR，context deadline 为 504 REQUEST_TIMEOUT。
401 带 WWW-Authenticate: Bearer。

```sh
gofmt -l cmd/api internal/session internal/user/service.go
sqlc generate
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
scripts/test-lesson12.sh
scripts/test-lesson13.sh
```

普通测试不会自动连接数据库。Lesson 13 脚本创建一次性 postgres:17 容器，
使用本机随机端口和独立 DB，退出仅清理它自己创建的容器与卷。
trust 认证仅限一次性本地测试，不可用于部署。
测试覆盖注册→登录→me→退出、数据库摘要存储、错误凭据、用户隔离、多 Session 隔离、
伪造身份、撤销、到期、删除用户、并发退出、限流并发、数据库故障与 context。
脚本还验证空库重放 000001–000005、含用户/任务数据的 4→5 升级，比较完整行摘要。

## 7. 安全边界与技术债

- 不开放公网：Task 尚未鉴权/隔离，HTTPS 和完整防滥用未完成。
- 当前是 Bearer API；客户端应只在内存中持有 Token，不放 URL、日志或长期 localStorage。
  本机 HTTP 仅用于开发；非本机传输必须 HTTPS。
- 未来浏览器 Cookie 方案必须设计 HttpOnly、Secure、SameSite 和 CSRF；不能直接混用。
- 无刷新 Token、自动轮换、会话列表、撤销全部会话或后台清理任务。
  到期/撤销行不会被自动清理；索引为后续维护留出查询路径。
- 当前 User 没有禁用状态字段；用户有效性指存在于 users 中。后续引入禁用状态时，
  必须同步修改 Session 有效性查询。
- 已有 Compose 开发凭据问题保持原样，本节不轮换，不推送或部署。

## 8. 本地手动验收

先配置 DATABASE_URL 并确认本地迁移为版本 5、dirty=false，再执行 `go run ./cmd/api`。
若仍为干净的版本 4，检查目标后使用 `migrate -path migrations -database "$DATABASE_URL" up 1`。
不要执行 force/down/drop 或删除已有卷。

使用一次性测试账号，先通过 `/auth/register` 注册，然后：

```sh
curl -i http://127.0.0.1:9090/auth/login \
  -H 'Content-Type: application/json' \
  --data '{"email":"lesson13@example.com","password":"lesson thirteen test passphrase"}'
```

成功应为 200，包含 access_token、Bearer 类型和到期时间。
在本地交互式 Bash 输入本次 Token（不写入命令历史）：

```sh
read -rsp 'Token: ' TASKFLOW_TEST_TOKEN
curl -i http://127.0.0.1:9090/auth/me -H "Authorization: Bearer $TASKFLOW_TEST_TOKEN"
curl -i -X POST http://127.0.0.1:9090/auth/logout -H "Authorization: Bearer $TASKFLOW_TEST_TOKEN"
curl -i http://127.0.0.1:9090/auth/me -H "Authorization: Bearer $TASKFLOW_TEST_TOKEN"
unset TASKFLOW_TEST_TOKEN
```

预期 200 → 204 → 401。错误密码、缺失 Authorization 也应为 401。
使用第二个账号登录，me 应只返回其本人；A 退出不影响 B。
原始 Token 不应出现在数据库，可只检查 `octet_length(token_hash)` 为 32。

## 9. 三道理解题

1. 为什么随机 Session Token 可以只存 SHA-256 摘要，而用户密码不能这样保存？
2. 为什么退出只删除客户端 Token 不够？并发退出如何由 SQL 保证正确？
3. 认证成功为什么仍不能证明有权读取任意 Task？401 和 403 分别表示什么？

## 10. 下一节前置条件

完成手动验收并由学习者接受本节后，才进入 Lesson 14：Task 所有权与跨用户隔离。
当前没有增加 owner_id、修改 Task Repository 或保护 Task 路由。

## 执行记录（2026-10-26）

- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 均通过。
- Lesson 12 隔离数据库回归通过；Lesson 13 数据库生命周期/并发测试在 race 模式重复三次通过。
- 空库重放、含历史数据的 4→5 升级均通过。
- sqlc 再生成结果逐字节一致；gofmt 与 git diff --check 无问题。
- 本地开发库已从 `4|f` 升级为 `5|f`；任务/用户完整行摘要未变化。
  当前任务 4 条、用户 0 条、Session 0 条。测试数据仅进入一次性测试库。
- 未修改已应用迁移、未提交/推送、未部署，也未实现 Lesson 14。
