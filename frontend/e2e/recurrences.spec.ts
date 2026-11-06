import { test, expect } from "@playwright/test";

test("recurrence management keeps existing styling and works on mobile", async ({
  page,
}) => {
  let rule: Record<string, unknown> | null = null;
  await page.route("http://127.0.0.1:5173/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/login"))
      return route.fulfill({
        json: {
          access_token: "test",
          token_type: "Bearer",
          expires_at: "2099-01-01T00:00:00Z",
        },
      });
    if (path.endsWith("/me"))
      return route.fulfill({
        json: {
          id: "u",
          email: "recurrence@example.com",
          created_at: "2026-01-01T00:00:00Z",
        },
      });
    if (path.endsWith("/integrations/discord"))
      return route.fulfill({
        json: {
          enabled: true,
          bound: true,
          version: 1,
          guild_id: "111",
          channel_id: "222",
          channel_name: "test",
          bound_at: "2026-11-06T12:00:00Z",
        },
      });
    if (path.endsWith("/projects") || path.endsWith("/tasks"))
      return route.fulfill({ json: [] });
    if (path.endsWith("/preview"))
      return route.fulfill({
        json: {
          plan_date: "2026-11-05",
          timezone: "Europe/Amsterdam",
          dates: ["2026-11-05", "2026-11-08", "2026-11-11"],
        },
      });
    if (path.endsWith("/recurrences")) {
      if (request.method() === "POST") {
        rule = {
          ...request.postDataJSON(),
          id: "00000000-0000-4000-8000-000000000001",
          next_run_date: "2026-11-05",
          version: 1,
          paused: false,
          preview_dates: ["2026-11-05", "2026-11-08", "2026-11-11"],
        };
        return route.fulfill({ status: 201, json: rule });
      }
      return route.fulfill({
        json: {
          items: rule ? [rule] : [],
          next_cursor: "",
          plan_date: "2026-11-05",
          timezone: "Europe/Amsterdam",
          scheduler_enabled: false,
        },
      });
    }
    if (path.includes("/recurrences/") && rule) {
      if (request.method() === "DELETE") {
        rule = null;
        return route.fulfill({ status: 204 });
      }
      const patch = request.postDataJSON();
      expect(patch.version).toBe(rule.version);
      rule = { ...rule, ...patch, version: Number(rule.version) + 1 };
      return route.fulfill({ json: rule });
    }
    return route.fulfill({ status: 404 });
  });
  await page.goto("/login");
  await page.getByLabel("邮箱").fill("recurrence@example.com");
  await page
    .getByLabel("密码", { exact: true })
    .fill("a long learning passphrase");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("link", { name: "任务", exact: true }).click();
  await page.getByRole("button", { name: "循环任务", exact: true }).click();
  await page.getByRole("button", { name: "新建循环规则" }).click();
  await page.getByLabel("任务标题").fill("每三天阅读");
  await page.getByLabel("每隔几天").fill("3");
  await page.getByLabel("Discord 频道提醒").check();
  await expect(page.getByText(/周期预览：2026-11-05/)).toBeVisible();
  await page.getByRole("button", { name: "保存规则" }).click();
  await expect(page.getByRole("heading", { name: "每三天阅读" })).toBeVisible();
  await expect(page.getByText("Discord 提醒", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "暂停", exact: true }).click();
  await expect(page.getByText("每 3 天 · 已暂停")).toBeVisible();
  await page.getByRole("button", { name: "恢复", exact: true }).click();
  await expect(page.getByText("每 3 天 · 启用")).toBeVisible();
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/lesson31-mobile.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "删除", exact: true }).click();
  await expect(page.getByText(/已有任务和历史记录会保留/)).toBeVisible();
  await page.getByRole("button", { name: "确认删除" }).click();
  await expect(
    page.getByRole("heading", { name: "还没有循环规则" }),
  ).toBeVisible();
});
