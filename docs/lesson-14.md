# Lesson 14 — Task 所有权与跨用户隔离

## 1. 实际完成

所有已实现的 `/tasks` 路由均要求有效 Bearer Session。每条任务有非空 owner_id，
创建时从认证上下文赋值。列表、单条查询、更新和删除均在 SQL 中按当前用户过滤。
内存 Repository 遵循相同契约，保留 version 乐观锁。

客户端不能通过请求体指定 owner_id/user_id；这类字段返回 400。
未认证返回 401，访问他人资源和不存在资源均返回 404。
本节不实现 Lesson 15 的 PATCH due_date 三态、分页或筛选。

## 2. 文件改动

- `migrations/000006_add_task_owner.{up,down}.sql`：非空 UUID 所有者、用户外键、复合索引。
- `internal/db/queries/tasks.sql`：每条读写查询携带 owner_id；sqlc 文件重新生成。
- `internal/task/model.go`：内部 OwnerID（不改变现有响应字段）。
- `internal/task/repository.go`、`service.go`：显式传入 `session.UserID`。
- `internal/task/postgres_repository.go`：pgtype.UUID 映射，所有操作限定主体。
- `internal/task/memory_repository.go`：同样的归属检查，拒绝覆盖已有 ID，保留原子版本比较。
- `internal/task/handler.go`：从 context 获取身份，拒绝客户端所有权字段，集中挂载受保护路由。
- `cmd/api/main.go`：以 Task Mount 替换旧的无认证路由注册。
- 现有 Task 测试：增加认证主体和 Bearer 头。
- `internal/task/{auth,ownership,integration}_test.go`：认证测试夹具、双实现隔离契约、真实数据库 E2E。
- `scripts/test-lesson14.sh`：一次性数据库验证；修正 Lesson 12 的固定版本，恢复缺失的 Lesson 13 测试脚本。

## 3. 请求链路

HTTP → Session Middleware → 摘要查库、检查到期/撤销 → 类型化 Principal →
Task Handler 提取 UserID → Service(ctx, ownerID, …) → Repository(ctx, ownerID, …) →
sqlc 参数化 SQL → PostgreSQL。

Handler 即使被误挂到没有 Middleware 的路由，也会因 context 无认证主体返回 401。
Service 拒绝空主体。Repository 的授权过滤不可被上层业务校验替代。

## 4. 关键知识点

- Authentication 证明身份，Authorization 限定资源。知道任务 ID 不等于有访问权。
- IDOR/BOLA：仅按客户端提供的 task ID 查询会泄露其他用户资源；必须同时限定 owner_id。
- `WHERE owner_id = $n` 把隔离边界落在 SQL，而不是依赖前端隐藏或查询后过滤。
- 404 避免通过 403/409 暴露他人任务是否存在或版本状态。
- `NOT NULL + FK` 保证新任务必须有真实用户；默认非级联删除策略防止删用户时连带删除任务。
- `(owner_id, created_at DESC, id DESC)` 索引配合当前按用户查询及稳定排序；本节没有增加分页。

## 5. 核心代码说明

### 显式主体参数

Repository 的每个方法都要求 `session.UserID`，例如：

```go
FindByID(ctx context.Context, ownerID session.UserID, id string) (*Task, error)
```

创建采用这个认证参数，忽略内部 Task 对象里可能不一致的 OwnerID；更新不修改数据库 owner_id。
没有使用请求 JSON、query string 或任意自填 header 作为身份来源。

### SQL 授权和并发

```sql
WHERE id = $1 AND version = $7 AND owner_id = $8
```

更新同时匹配 ID、所有者和预期版本，原子执行 version=version+1。
没有更新到行时，后续存在性检查仍携带同一个 owner_id：自己任务的旧版本是 409，
他人任务或不存在的任务是 404。删除也同时限定 id 和 owner_id。

### HTTP 输入边界

Task POST/PATCH 使用独立 JSON decoder，拒绝未知字段和多个 JSON 值，
然后复用 Gin 的 binding 校验。owner_id/user_id（包括 null）因此不能被接受。
请求体上限为 64 KiB；非法、未知字段或过大请求当前统一返回 400 INVALID_REQUEST。
GET 的 query string 不提供主体，也不改变当前用户的列表范围。
响应继续使用原 Task 字段；内部 OwnerID 使用 json:"-"。

### 迁移选择与用户授权

检查时本地库为 5、dirty=false，4 条任务、0 个用户，无法推断旧任务归属。
用户明确确认：“这些是可删除的测试任务，允许清理后强制所有权”。
因此执行受行数校验和表锁保护的本地清理（DELETE 4），随后应用 000006，直接增加
非空 owner_id。这是本次已授权测试数据的简化升级路线，迁移 SQL 本身不包含 DELETE。

其他数据库如仍有任务，迁移会拒绝执行，不能把本次授权推广到其他数据或环境。
真实数据应另行采用可空列 → 明确逐条归属 → 回填 → 校验 → NOT NULL 的迁移，
不可直接执行本节的空表升级。失败后可能出现 migrate dirty 状态，不能未经调查 force。

## 6. 测试与检查

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
scripts/test-lesson12.sh
scripts/test-lesson13.sh
scripts/test-lesson14.sh
sqlc generate
gofmt -l internal/task cmd/api/main.go
git diff --check
```

普通测试使用内存仓库和真实认证 Middleware 的测试 Session Repository。
集成脚本创建随机本机端口上的一次性 PostgreSQL；只删除自己创建的容器和测试卷，
不会清理本地开发库。普通 go test 没有显式测试环境变量时跳过数据库集成测试。

测试覆盖：

- A/B 各自创建和列出任务，双向 get/update/delete 跨用户拒绝。
- 直接调用内存与 PostgreSQL Repository 也不能绕过所有权检查。
- JSON 伪造 owner_id/user_id 拒绝，query 参数不能切换身份。
- 所有五个 Task 路由未认证返回 401；过期、撤销 Session 不能再访问。
- 两个请求读取同一版本后并发更新，内存和真实 SQL 都恰好一成功、一冲突。
- 数据库拒绝 NULL owner、不存在用户，以及删除仍有任务的用户。
- 旧版本 409、数据库故障只返回通用 500、context 传递。
- 空库重放 1–6；带历史任务时用事务验证 000006 拒绝且完整数据摘要不变。
- 清理一次性任务夹具后 5→6 成功，用户与 Session 完整行摘要不变。

## 7. 兼容性与剩余风险

这是有意的 API 认证要求变更：旧客户端必须先登录，并携带 Bearer。
未知 JSON 字段现在会返回 400；PATCH due_date 尚未实现，不能继续依赖其被静默忽略。
所有权列没有默认值，旧版服务的无 owner INSERT 会失败；升级后应运行新代码，
不要回退到未鉴权的旧程序或执行会丢失所有权的 down 迁移。

用户已有任务时不能直接删除用户；未来删除账号要另行确定数据保留/删除策略。
当前登录限流仍是单进程全局基线。HTTPS、完整防滥用、安全部署和审计仍待后续课程。
完成本节的隔离测试并不等于已经满足公网部署条件。

## 8. 本地手动验收

当前本地库已是版本 6，无需再运行清理或迁移。配置 DATABASE_URL 后启动：

```sh
go run ./cmd/api
```

通过 `/auth/register` 注册两个一次性账号 A/B，再通过 `/auth/login` 分别获取 Token。
在本地交互式 Bash 输入 A 的 Token（避免写入命令历史）：

```sh
read -rsp 'A token: ' TASKFLOW_A_TOKEN
curl -i http://127.0.0.1:9090/tasks \
  -H "Authorization: Bearer $TASKFLOW_A_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"title":"A private task","priority":2}'
```

记录返回的任务 ID。使用 B 的 Token 查询列表应看不到它，GET/PATCH/DELETE 该 ID
均应为 404。使用 A Token 正常更新并提供 version=1，应返回 version=2；重用旧版本为 409。
不携带 Token 访问 `/tasks` 应为 401。创建请求增加 `"owner_id":"任意值"` 应为 400。
完成后 unset shell 中的 Token 变量；不要将 Token 放到 URL、日志或长期 localStorage。

## 9. 三道理解题

1. 为什么查询后的前端隐藏或 Service 判断不能代替 SQL 中的 owner_id 条件？
2. 为什么更新失败后的存在性检查也必须带 owner_id，否则可能泄露什么？
3. 本次为什么可以清理后直接 NOT NULL，而真实历史数据需要分阶段回填？

## 10. 下一节条件

学习者完成双账号验收、理解上述边界并接受本节后，才能进入 Lesson 15。
本节没有增加分页、筛选或 PATCH 三态。

## 执行记录（2026-10-26）

- 普通测试、race 测试、go vet、gofmt、git diff --check 均通过。
- sqlc 再生成逐字节一致。
- Lesson 14 真实 PostgreSQL 隔离与并发测试在 race 模式重复三次通过。
- Lesson 12 首次回归因脚本升级到最新版本而不匹配版本 4 断言失败；固定目标版本后通过。
  恢复仓库中缺失的 Lesson 13 脚本后，其会话回归与迁移测试也通过。
- 本地按用户明确授权删除 4 条旧测试任务；随后迁移到 `6|f`，owner_id 为 NOT NULL。
  用户和 Session 完整行摘要未变化，当前 tasks/users/sessions 均为 0。
- 迁移 SQL 不包含清理；没有执行 force/down/drop 或删除现有数据库卷。
- 未自动提交、推送、部署或进入 Lesson 15。
