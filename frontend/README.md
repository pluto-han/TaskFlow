# TaskFlow frontend

Lesson 18：React + TypeScript + Vite 登录、注册、个人资料与退出。
完整教学说明、认证边界和验收步骤见 [docs/lesson-18.md](../docs/lesson-18.md)。

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

Token 只在当前页面的 React 内存中；刷新、新标签页或关闭页面后需要重新登录。
没有 localStorage/sessionStorage、Cookie 或 refresh token 持久化。

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
默认浏览器测试模拟 API。启用真实后端用例前，必须启动连接**隔离测试数据库**的 API；再设置 TASKFLOW_API_TARGET 为该 API 地址，TASKFLOW_LIVE_E2E=1，然后执行 `npm run test:e2e`。它会创建测试账号，不要指向生产或日常数据库。

## 构建部署边界

`npm run build` 输出 `dist/`。生产静态服务器需要：

- 把 `/api/*` 反向代理到 Go API 并去掉 `/api`，保留 Authorization。
- `/login`、`/register`、`/profile` 等前端路由回退到 index.html；不要把 `/api` 的错误回退成 HTML。
- 使用 HTTPS，按环境设置安全响应头，避免代理记录 Authorization/请求体。

`npm run preview` 只预览构建文件，**没有配置 API 代理**，不是完整登录环境或生产服务器。

## Lesson 20：共享项目

API 数据库需先应用 migration 000009。完整权限矩阵、并发设计、验收与风险见 [Lesson 20](../docs/lesson-20.md)。

朋友先注册，从个人资料复制账号 ID；项目所有者在“成员与邀请”生成链接并手工分享。邀请绑定该账号，24 小时有效、单次使用、可撤销。链接中的 fragment 在页面启动时移除，令牌仅保存在内存，登录后明确点击接受；中途刷新需重新打开原链接。

Member 可编辑、完成、删除项目内所有任务，但只能移动自己创建的任务。所有者可以移除成员或转移所有权；有任务时仍拒绝删除项目。移除后对方的项目任务保留，对方后续请求被拒绝，先前已显示的内容无法收回。

生产 SPA 回退也需覆盖 `/tasks` 和 `/invite`。真实浏览器测试包含两个隔离浏览器账号的邀请、协作、撤权和所有权转移流程。

## Lesson 22：生产安全边界

构建时注入只允许同源资源的 CSP；开发模式保留 HMR。HTML 设置 no-referrer，正式静态服务器仍需发送 frame-ancestors/DENY、HSTS 和 nosniff 等安全响应头。API 的安全响应头不能替代静态站点响应头。详见 [安全审查及上线阻断项](../docs/lesson-22.md)。

生产 API 需要明确配置 HTTPS origin、可信 TLS 代理和 verify-full 数据库连接；前端继续同源代理，不开放跨域 credentials。注册/登录限流返回 429，界面提示一分钟后重试。Compose 密码改为环境变量；现有数据库卷的密码不会自动改变。
