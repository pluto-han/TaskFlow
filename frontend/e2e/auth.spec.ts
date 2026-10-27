import { test, expect } from "@playwright/test";

test("login, revocation, refresh and responsive layout", async ({ page }) => {
  let expired = false;
  await page.route("**/api/auth/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/login"))
      return route.fulfill({
        json: {
          access_token: "browser-memory-token",
          token_type: "Bearer",
          expires_at: new Date(Date.now() + 3600000).toISOString(),
        },
      });
    if (path.endsWith("/logout")) return route.fulfill({ status: 204 });
    if (expired)
      return route.fulfill({
        status: 401,
        json: { error: { code: "UNAUTHORIZED", message: "Unauthorized" } },
      });
    return route.fulfill({
      json: {
        id: "user-1",
        email: "learner@example.com",
        created_at: "2026-01-01T00:00:00Z",
      },
    });
  });
  await page.goto("/profile");
  await expect(page).toHaveURL(/\/login$/);
  await page.screenshot({
    path: "test-results/login-desktop.png",
    fullPage: true,
  });
  async function login() {
    await page.getByLabel("邮箱地址").fill("learner@example.com");
    await page
      .getByLabel("密码", { exact: true })
      .fill("a long test passphrase");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "你的个人空间" }),
    ).toBeVisible();
  }
  await login();
  expect(
    await page.evaluate(() => ({
      local: localStorage.length,
      session: sessionStorage.length,
      cookies: document.cookie,
    })),
  ).toEqual({ local: 0, session: 0, cookies: "" });
  await page.screenshot({
    path: "test-results/profile-desktop.png",
    fullPage: true,
  });
  await page.reload();
  await expect(page).toHaveURL(/\/login$/);
  await login();
  expired = true;
  await page.getByRole("button", { name: "刷新资料" }).click();
  await expect(page.getByRole("status")).toContainText("登录已失效");
  expired = false;
  await login();
  await page.getByRole("button", { name: "退出登录" }).click();
  await expect(page.getByRole("status")).toContainText("已安全退出");
  await page.setViewportSize({ width: 375, height: 812 });
  await page.screenshot({
    path: "test-results/login-mobile.png",
    fullPage: true,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.getByRole("link", { name: "创建账号" }).click();
  await page.getByRole("button", { name: "创建账号" }).click();
  await expect(page.getByLabel("邮箱地址")).toBeFocused();
  await page.screenshot({
    path: "test-results/register-mobile.png",
    fullPage: true,
  });
});

test("actual API registration/login/profile/logout through Vite proxy", async ({
  page,
}) => {
  test.skip(
    process.env.TASKFLOW_LIVE_E2E !== "1",
    "Requires an explicitly configured isolated API/database",
  );
  const email = `browser-${Date.now()}@example.com`;
  await page.goto("/register");
  await page.getByLabel("邮箱地址").fill(email);
  await page
    .getByLabel("密码", { exact: true })
    .fill("a long live browser passphrase");
  await page.getByLabel("确认密码").fill("a long live browser passphrase");
  await page.getByRole("button", { name: "创建账号" }).click();
  await expect(page.getByRole("status")).toContainText("注册成功");
  await page.getByRole("link", { name: "前往登录" }).click();
  await page.getByLabel("邮箱地址").fill(email);
  await page
    .getByLabel("密码", { exact: true })
    .fill("a long live browser passphrase");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: email })).toBeVisible();
  await page.getByRole("button", { name: "刷新资料" }).click();
  await expect(page.getByRole("button", { name: "退出登录" })).toBeEnabled();
  await page.getByRole("button", { name: "退出登录" }).click();
  await expect(page.getByRole("status")).toContainText("已安全退出");
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
});
