GATE: OPEN 32 (rounds used up)

# A 方案细化记录

范围：独立设计原型与实施文档。未修改应用实现、依赖、API 或后端。

## 调整

- 导航改为同一套 Lucide UI 本地 SVG，保留来源与许可元数据。
- 移除评审条、占位头像、重复 Settings 入口及设置内容重复标题。
- 保留 A 配色；统一标题、任务标题、次信息、行高及内容边界。
- 主、次按钮使用技能原样 motion.css，提供短暂 hover / press 反馈。
- 改善任务描述对比度；勾选框关联可见标题并提供 44px 标签点击区域。
- 筛选后选择当前可见任务；空列表不显示旧详情；恢复必要键盘焦点。

## 技能适用范围

software；用户已选定的暖灰纸面、炭黑导航、橄榄绿操作、衬线标题与无衬线正文。没有重新抽选配色或插入营销 hero、照片、页脚、shader、循环动画、dealt moment；这些会改变已批准的任务工作台。

Voice: technical-dry；使用现有业务名词与动作标签；无解释性文案。

无照片、虚构证明、品牌重命名、联系表单或 canonical 域名；演示任务、账号、日期与频道沿用已有模拟数据。正式实现不得将它们写成真实业务数据。

## 已执行检查

- 静态 copy-lint：0 FAIL，0 WARN。
- gate：执行 3 轮，最终 OPEN 32；无 waiver，未修改检查器或技能资产。
- 快速 consistency 与 layout：1440 / 390；附带 320px 溢出检查。
- `assets`：PASS。
- `off-viewport`：PASS。
- `spill`：PASS。
- `overlap-text`：PASS。
- `collide`：PASS。
- `a11y-name`：PASS。
- `field-label`：PASS。
- `focus-ring`：PASS。
- `text-contrast`：PASS。
- `menu-toggle`：PASS。
- `page-meta`：PASS。

门禁仍有 23 条合并后的 WARN，包括较小触控目标、布局边界、操作按钮措辞等；没有将 WARN 或 OPEN 视为全部通过。完整原始报告见 `artifacts/frontend-concept/.taste-skill/gate/consistency/report.json`。

## 全部未通过项

| # | 检查 | 位置 | 保留原因 |
| --- | --- | --- | --- |
| 1 | `hero-fill` | main#main > div.view-tabs "All tasks Open Completed" | 工作台首屏同时显示标题、筛选和任务，不设置全屏营销 hero。 |
| 2 | `flush` | table.task-table > thead > tr > th.check "Complete" | 表头背景与分隔线有意相接；隐藏的表头无障碍标签不占视觉空白。 |
| 3 | `placeholder` | header.top > div.identity > span.email "alex@example.com" | 已有演示账号，不是联系地址；正式实现读取登录用户邮箱。 |
| 4 | `no-visual` | main#main > div.work-grid > div "Tasks with project, priority and due dat" | 任务列表本身是产品内容，不为列表添加照片或特效。 |
| 5 | `button-styles` | (one screen) | 检查器将侧栏当前页面的选中外观计作第三类按钮；操作按钮仍为主、次两类。 |
| 6 | `button-styles` | (the page) | 检查器将侧栏当前页面的选中外观计作第三类按钮；操作按钮仍为主、次两类。 |
| 7 | `off-grid` | nav#sidebar > a:nth-of-type(1) "All tasks" | 导航行的外边界与图标、文字的内缩边界不同；同类行共享同一边界。 |
| 8 | `links` | header.top > a.brand "TaskFlow" | 原型使用 hash 路由和 hashchange 渲染，不是跳转到静态 section ID。 |
| 9 | `links` | a#tasks-nav "Tasks" | 原型使用 hash 路由和 hashchange 渲染，不是跳转到静态 section ID。 |
| 10 | `links` | a#settings-nav "Settings" | 原型使用 hash 路由和 hashchange 渲染，不是跳转到静态 section ID。 |
| 11 | `links` | nav#sidebar > a:nth-of-type(1) "All tasks" | 原型使用 hash 路由和 hashchange 渲染，不是跳转到静态 section ID。 |
| 12 | `links` | nav#sidebar > a:nth-of-type(2) "Today" | 原型使用 hash 路由和 hashchange 渲染，不是跳转到静态 section ID。 |
| 13 | `links` | nav#sidebar > a:nth-of-type(3) "Unassigned" | 原型使用 hash 路由和 hashchange 渲染，不是跳转到静态 section ID。 |
| 14 | `links` | nav#sidebar > a:nth-of-type(4) "Recurring tasks" | 原型使用 hash 路由和 hashchange 渲染，不是跳转到静态 section ID。 |
| 15 | `links` | nav#sidebar > a:nth-of-type(5) "Website redesign" | 原型使用 hash 路由和 hashchange 渲染，不是跳转到静态 section ID。 |
| 16 | `moment-none` | (the page) | 已确认不增加大动画或宣传段落。 |
| 17 | `hero-screen` | main#main > header.page-heading "All tasksNew task" | 不把标题区扩展为整屏营销内容。 |
| 18 | `hero-screen` | main#main > div.view-tabs "All tasksOpenCompleted" | 不把标题区扩展为整屏营销内容。 |
| 19 | `section-height` | main#main > div.list-footer "8 tasksLoad more" | 任务计数按内容高度，不扩展为一整屏。 |
| 20 | `section-air` | main#main > div.view-tabs / main#main > div.toolbar | 保持紧凑工作区，不增加 128px 以上的段间空白。 |
| 21 | `section-air` | main#main > div.work-grid / main#main > div.list-footer | 保持紧凑工作区，不增加 128px 以上的段间空白。 |
| 22 | `section-air` | main#main > div.toolbar / main#main > div.work-grid | 保持紧凑工作区，不增加 128px 以上的段间空白。 |
| 23 | `one-section` | main#main > header.page-heading > div > h1 "All tasks" | 筛选、列表和详情需要同时可见。 |
| 24 | `container-edges` | header.top "TaskFlow TasksSettings alex@example.com " | 侧栏、顶栏及任务表采用各自的明确列边界，不采用营销页单栏容器。 |
| 25 | `container-edges` | main#main > header.page-heading "All tasksNew task" | 侧栏、顶栏及任务表采用各自的明确列边界，不采用营销页单栏容器。 |
| 26 | `container-edges` | a.skip-link "Skip to content" | 侧栏、顶栏及任务表采用各自的明确列边界，不采用营销页单栏容器。 |
| 27 | `container-edges` | header.top > a.brand "TaskFlow" | 侧栏、顶栏及任务表采用各自的明确列边界，不采用营销页单栏容器。 |
| 28 | `container-edges` | a#tasks-nav "Tasks" | 侧栏、顶栏及任务表采用各自的明确列边界，不采用营销页单栏容器。 |
| 29 | `container-edges` | main#main > div.work-grid > aside.task-detail > h2.detail-title "Review navigation and entry points" | 侧栏、顶栏及任务表采用各自的明确列边界，不采用营销页单栏容器。 |
| 30 | `container-edges` | table.task-table > thead > tr > th:nth-of-type(2) "Task / Project" | 侧栏、顶栏及任务表采用各自的明确列边界，不采用营销页单栏容器。 |
| 31 | `text-only` | main#main > div.work-grid > aside.task-detail "Review navigation and entry points" | 详情显示任务文本和字段，不增加装饰性图片。 |
| 32 | `container` | page | 沿用已确认的应用栅格，无营销页 .wrap 容器。 |

## 验证限制

完整 consistency 的表单提交、hover 运动、减少动态偏好、隐藏视觉和滚动巡检未执行；本轮不验证真实后端提交或业务测试。已生成 1440、1024、820、768、390px 截图，查看了桌面、手机和 Account 页面；其余业务页面也已导出。`perf.mjs` 在 1440 / 390 各运行 3 次：0 FAIL、0 WARN、0 broken requests；仅代表本地模拟原型，不代表真实应用性能。Prettier、脚本语法、SVG、图片引用与已删除文案检查通过。
