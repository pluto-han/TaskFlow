import { test, expect } from "@playwright/test";

test.use({ timezoneId: "America/Los_Angeles" });

test("today CRUD, draft rollover and history in a non-Dutch mobile browser", async ({
  page,
}) => {
  // Los Angeles still shows November 4; the plan must use the server day.
  await page.clock.setFixedTime(new Date("2026-11-05T00:30:00Z"));
  let businessDate = "2026-11-05";
  let id = 0;
  const tasks = [
    {
      id: "old",
      title: "昨天的计划",
      description: "",
      priority: 2,
      completed: false,
      version: 1,
      can_move: false,
      plan_date: "2026-11-04",
      project_id: null,
      due_date: null,
      created_at: "2026-11-04T10:00:00Z",
    },
  ];
  await page.route("http://127.0.0.1:5173/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname.endsWith("/login"))
      return route.fulfill({
        json: {
          access_token: "test",
          token_type: "Bearer",
          expires_at: "2099-01-01T00:00:00Z",
        },
      });
    if (url.pathname.endsWith("/me"))
      return route.fulfill({
        json: {
          id: "u",
          email: "today@example.com",
          created_at: "2026-01-01T00:00:00Z",
        },
      });
    if (url.pathname.endsWith("/projects")) return route.fulfill({ json: [] });
    if (url.pathname.endsWith("/tasks/today")) {
      if (request.method() === "POST") {
        const body = request.postDataJSON();
        expect(body).not.toHaveProperty("project_id");
        expect(body.plan_date).toBe(businessDate);
        const task = {
          ...tasks[0],
          ...body,
          id: `created-${++id}`,
          completed: false,
          version: 1,
          can_move: false,
        };
        tasks.push(task);
        return route.fulfill({ status: 201, json: task });
      }
      return route.fulfill({
        json: {
          items: tasks.filter((t) => t.plan_date === businessDate),
          next_cursor: "",
          plan_date: businessDate,
          timezone: "Europe/Amsterdam",
          server_time: `${businessDate}T10:00:00Z`,
          next_day_at: `${businessDate}T23:00:00Z`,
        },
      });
    }
    if (url.pathname.endsWith("/tasks"))
      return route.fulfill({
        json: tasks.filter(
          (t) =>
            !url.searchParams.get("plan_date") ||
            t.plan_date === url.searchParams.get("plan_date"),
        ),
      });
    const taskIndex = tasks.findIndex((t) =>
      url.pathname.endsWith(`/tasks/${t.id}`),
    );
    if (taskIndex >= 0) {
      if (request.method() === "DELETE") {
        tasks.splice(taskIndex, 1);
        return route.fulfill({ status: 204 });
      }
      if (request.method() === "PATCH") {
        const patch = request.postDataJSON();
        tasks[taskIndex] = {
          ...tasks[taskIndex],
          ...patch,
          version: tasks[taskIndex].version + 1,
        };
      }
      return route.fulfill({ json: tasks[taskIndex] });
    }
    return route.fulfill({
      status: 404,
      json: { error: { code: "ROUTE_NOT_FOUND", message: "Not found" } },
    });
  });
  await page.goto("/login");
  await page.getByLabel("邮箱地址").fill("today@example.com");
  await page.getByLabel("密码", { exact: true }).fill("a long test passphrase");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("link", { name: "任务", exact: true }).click();
  await page.getByRole("button", { name: "今日任务", exact: true }).click();
  await expect(
    page.getByText("荷兰时间 / Europe/Amsterdam · 2026-11-05"),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "新建项目" })).toHaveCount(0);
  await page.getByRole("button", { name: "＋ 新建任务" }).click();
  await expect(page.getByLabel("所属项目")).toHaveCount(0);
  await page.getByLabel("任务标题").fill("荷兰今日任务");
  await page.getByRole("button", { name: "保存任务" }).click();
  await expect(
    page.getByRole("heading", { name: "荷兰今日任务" }),
  ).toBeVisible();
  await page.getByRole("checkbox", { name: "完成任务：荷兰今日任务" }).check();
  await expect(
    page.getByRole("checkbox", { name: "标记未完成：荷兰今日任务" }),
  ).toBeChecked();
  await page.getByRole("button", { name: "编辑任务：荷兰今日任务" }).click();
  await expect(page.getByLabel("所属项目")).toHaveCount(0);
  await page.getByLabel("任务标题").fill("已编辑的今日任务");
  await page.getByRole("button", { name: "保存任务" }).click();
  await page.getByRole("button", { name: "＋ 新建任务" }).click();
  await page.getByLabel("任务标题").fill("跨日保留草稿");
  businessDate = "2026-11-06";
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(
    page.getByRole("region", { name: "计划日期变化" }),
  ).toBeVisible();
  await expect(page.getByLabel("任务标题")).toHaveValue("跨日保留草稿");
  await expect(page.getByRole("button", { name: "保存任务" })).toBeDisabled();
  await page.getByRole("button", { name: "刷新并确认计划日期" }).click();
  await page.getByRole("button", { name: "保存任务" }).click();
  await expect(
    page.getByRole("heading", { name: "已编辑的今日任务" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: "跨日保留草稿" }),
  ).toBeVisible();
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/today-mobile.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: /全部任务/ }).click();
  await page.getByLabel("筛选计划日期").fill("2026-11-05");
  await expect(
    page.getByRole("heading", { name: "已编辑的今日任务" }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "跨日保留草稿" })).toHaveCount(
    0,
  );
  await page
    .getByRole("button", { name: "删除任务：已编辑的今日任务" })
    .click();
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(page.getByText("这里还没有任务")).toBeVisible();
});
