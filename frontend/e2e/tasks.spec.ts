import { test, expect } from "@playwright/test";

test.use({ timezoneId: "Europe/Amsterdam" });

test("workspace pagination, filtering and mobile layout", async ({ page }) => {
  const task = {
    can_move: true,
    id: "task-a",
    title: "为重要的事留出时间",
    description: "整理今天的想法，专注做好一件事。",
    completed: false,
    priority: 3,
    project_id: null,
    due_date: "2026-12-15T09:30:00Z",
    created_at: "2026-01-01T00:00:00Z",
    version: 1,
  };
  await page.route("http://127.0.0.1:5173/api/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/login"))
      return route.fulfill({
        json: {
          access_token: "memory-only",
          token_type: "Bearer",
          expires_at: new Date(Date.now() + 3600000).toISOString(),
        },
      });
    if (url.pathname.endsWith("/me"))
      return route.fulfill({
        json: {
          id: "u",
          email: "learner@example.com",
          created_at: "2026-01-01T00:00:00Z",
        },
      });
    if (url.pathname.endsWith("/projects"))
      return route.fulfill({
        json: [
          {
            id: "11111111-1111-4111-8111-111111111111",
            name: "个人成长",
            role: "owner",
            description: "",
            created_at: task.created_at,
            updated_at: task.created_at,
          },
        ],
      });
    if (url.searchParams.get("project_id")) return route.fulfill({ json: [] });
    if (url.searchParams.get("cursor"))
      return route.fulfill({
        json: [
          {
            ...task,
            id: "task-b",
            title: "读完一本书",
            priority: 1,
            due_date: null,
          },
        ],
      });
    return route.fulfill({
      json: [task],
      headers: { "X-Next-Cursor": "next" },
    });
  });
  await page.goto("/login");
  await page.getByLabel("邮箱地址").fill("learner@example.com");
  await page.getByLabel("密码", { exact: true }).fill("a long test passphrase");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("link", { name: "任务", exact: true }).click();
  await expect(page.getByRole("heading", { name: task.title })).toBeVisible();
  await page.getByRole("button", { name: "加载更多" }).click();
  await expect(page.getByRole("heading", { name: "读完一本书" })).toBeVisible();
  await page.screenshot({
    path: "test-results/tasks-desktop.png",
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "个人成长", exact: true })
    .click();
  await expect(page.getByText("这里还没有任务")).toBeVisible();
  await page.getByRole("button", { name: "全部任务", exact: false }).click();
  await expect(page.getByRole("heading", { name: task.title })).toBeVisible();
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/tasks-mobile.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "编辑任务：" + task.title }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByLabel("截止日期（本地时间）")).toHaveValue(
    /^2026-12-15T10:30(:00)?$/,
  );
  await page.screenshot({
    path: "test-results/task-editor-mobile.png",
    fullPage: true,
  });
  await page.getByLabel("截止日期（本地时间）").fill("2026-03-29T02:30");
  await page.getByRole("button", { name: "保存任务" }).click();
  await expect(page.getByRole("alert")).toContainText("夏令时");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(
    page.getByRole("button", { name: "编辑任务：" + task.title }),
  ).toBeFocused();
});

test("actual project/task CRUD, ownership filtering, null deadline and deletion policy", async ({
  page,
}) => {
  test.skip(
    process.env.TASKFLOW_LIVE_E2E !== "1",
    "Requires an isolated API/database",
  );
  await page.goto("/register");
  const email = `tasks-${Date.now()}@example.com`;
  await page.getByLabel("邮箱地址").fill(email);
  await page
    .getByLabel("密码", { exact: true })
    .fill("a long live task passphrase");
  await page.getByLabel("确认密码").fill("a long live task passphrase");
  await page.getByRole("button", { name: "创建账号" }).click();
  await page.getByRole("link", { name: "前往登录" }).click();
  await page.getByLabel("邮箱地址").fill(email);
  await page
    .getByLabel("密码", { exact: true })
    .fill("a long live task passphrase");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("link", { name: "任务", exact: true }).click();
  await expect(page.getByText("这里还没有任务")).toBeVisible();
  await page.getByRole("button", { name: "新建项目", exact: true }).click();
  await page.getByLabel("项目名称").fill("学习计划");
  await page.getByLabel("项目描述").fill("浏览器验收");
  await page.getByRole("button", { name: "保存项目" }).click();
  await page.getByRole("button", { name: "学习计划", exact: true }).click();
  await page.getByRole("button", { name: "编辑项目", exact: true }).click();
  await page.getByLabel("项目描述").fill("更新项目描述");
  await page.getByRole("button", { name: "保存项目" }).click();
  await expect(page.getByText("更新项目描述", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "新建任务", exact: false }).click();
  await page.getByLabel("任务标题").fill("完成 Lesson 19");
  await page.getByLabel("描述", { exact: true }).fill("从浏览器创建");
  await page.getByLabel("优先级", { exact: true }).selectOption("3");
  await page.getByLabel("截止日期（本地时间）").fill("2026-12-15T10:30");
  const createdResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/tasks") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "保存任务" }).click();
  const created = await (await createdResponse).json();
  expect(created.due_date).toBe("2026-12-15T09:30:00Z");
  expect(created.project_id).not.toBeNull();
  await expect(
    page.getByRole("heading", { name: "完成 Lesson 19" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "删除项目", exact: true }).click();
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByRole("alert")).toContainText("项目仍有任务");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page
    .getByRole("checkbox", { name: "完成任务：完成 Lesson 19" })
    .check();
  await expect(page.getByRole("checkbox")).toBeEnabled();
  await expect(page.getByRole("checkbox")).toBeChecked();
  await page.getByRole("button", { name: "编辑任务：完成 Lesson 19" }).click();
  await page.getByRole("button", { name: "清除截止日期" }).click();
  await page.getByLabel("所属项目").selectOption("");
  const patch = page.waitForRequest(
    (request) =>
      request.url().includes("/api/tasks/") && request.method() === "PATCH",
  );
  await page.getByRole("button", { name: "保存任务" }).click();
  expect((await patch).postDataJSON()).toEqual({
    version: 2,
    due_date: null,
    project_id: null,
  });
  await expect(page.getByText("这里还没有任务")).toBeVisible();
  await page.getByRole("button", { name: "删除项目", exact: true }).click();
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await page.getByRole("button", { name: "未归类", exact: true }).click();
  await page.getByRole("button", { name: "删除任务：完成 Lesson 19" }).click();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "完成 Lesson 19" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "删除任务：完成 Lesson 19" }).click();
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByText("这里还没有任务")).toBeVisible();
});
