# TaskFlow frontend

React + TypeScript + Vite。采用暖灰纸面、深色导航和紧凑任务列表；登录进入 Tasks，个人资料、安全与集成分为三个 Settings 页面。
页面设计、完整路由和验收记录见 [前端重构计划](../docs/frontend-refactoring-plan.md)。

## 开发

使用 Node.js 24.x（`.nvmrc`）。在仓库根目录启动已配置 DATABASE_URL 的 Go API：

```bash
go run ./cmd/api
```

另一个终端：

```bash
cd frontend
npm ci
npm run dev
```

打开 http://127.0.0.1:5173 。默认代理到 http://127.0.0.1:9090 。
需要调整后端地址时复制 `.env.example` 为 `.env.local`，修改 TASKFLOW_API_TARGET；该变量只供 Vite 服务端使用。
浏览器固定请求同源 `/api/*`，Vite 去掉 `/api` 后转发。无需放宽后端 CORS。

浏览器使用服务端会话与 HttpOnly Cookie；启动时通过会话接口恢复登录。CSRF 凭据仅保存在 React 内存中，不向 localStorage / sessionStorage 写入凭据。邀请、验证和重置链接的令牌从 fragment 提取后立即移除，仅在内存保留，明确提交后消费。

## 路由与目录

| 路由 | 页面 |
| --- | --- |
| `/tasks`、`/tasks/today`、`/tasks/unassigned` | 全部任务、今日个人计划、未归属任务 |
| `/tasks/recurring` | 周期规则 |
| `/projects/:projectId/tasks`、`/projects/:projectId/settings` | 项目任务、项目设置 |
| `/settings/account`、`/settings/security`、`/settings/integrations` | 账号、安全、Discord |
| `/profile` | 兼容跳转 Account |

`layouts/` 维护框架及项目目录；`pages/` 装配路由；`tasks/`、`projects/`、`recurrences/`、`settings/` 维护各自交互；`components/` 放共享控件。`auth/` 统一会话与 CSRF，`api/` 统一请求及类型，测试位于 `test/`。

视图、筛选和选中任务保存在 URL；草稿、弹窗及展开状态留在组件中。Today 是 Europe/Amsterdam 的个人计划，不是按截止日期聚合。

## 检查

```bash
npm run api:check
npm run lint
npm run typecheck
npm test
npm run build
npx playwright install chromium
npm run test:e2e
```

OpenAPI 改动后执行 `npm run api:generate`，审阅 `src/api/schema.d.ts`；不要手工修改生成文件。
默认浏览器测试模拟 API；5 条真实服务用例由仓库根目录的隔离夹具执行：

```bash
npm --prefix frontend run build
python3 scripts/test_integration.py --repeat 1 --browser-e2e
```

夹具创建临时数据库，启动真实 API、fake inbox 与 stub Discord，完成后清理。另行使用 `TASKFLOW_LIVE_E2E=1` 时必须配置隔离 API，测试会创建账号，不得指向生产或日常数据库。

## 构建部署边界

`npm run build` 输出 `dist/`。生产静态服务器需要：

- 把 `/api/*` 反向代理到 Go API 并去掉 `/api`，正确转发 Cookie / Set-Cookie。
- `/tasks/*`、`/projects/*`、`/settings/*` 及公共页面路由回退到 index.html；不要把 `/api` 的错误回退成 HTML。
- 使用 HTTPS，按环境设置安全响应头，避免代理记录 Authorization/请求体。

`npm run preview` 只预览构建文件，**没有配置 API 代理**，不是完整登录环境或生产服务器。

## 共享项目

开发数据库需应用仓库现有迁移。项目权限设计的历史说明见 [Lesson 20](../docs/lesson-20.md)。

朋友先注册，从 Settings → Account 复制账号 ID；项目所有者在 Project settings 生成邀请链接并手工分享。邀请绑定该账号，24 小时有效、单次使用、可撤销。链接中的 fragment 在页面启动时移除，令牌仅保存在内存，登录后明确点击接受；中途刷新需重新打开原链接。

Member 可编辑、完成、删除项目内所有任务，但只能移动自己创建的任务。所有者可以移除成员或转移所有权；有任务时仍拒绝删除项目。移除后对方的项目任务保留，对方后续请求被拒绝，先前已显示的内容无法收回。

真实浏览器测试包含两个隔离浏览器账号的邀请、协作、撤权和所有权转移流程。

## 生产安全边界

构建时注入只允许同源资源的 CSP；开发模式保留 HMR。HTML 设置 no-referrer，正式静态服务器仍需发送 frame-ancestors/DENY、HSTS 和 nosniff 等安全响应头。API 的安全响应头不能替代静态站点响应头。详见 [安全审查及上线阻断项](../docs/lesson-22.md)。

生产 API 需要明确配置 HTTPS origin、可信 TLS 代理和 verify-full 数据库连接；前端继续同源代理，不开放跨域 credentials。注册/登录限流返回 429，界面提示一分钟后重试。Compose 密码改为环境变量；现有数据库卷的密码不会自动改变。
