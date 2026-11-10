import { test, expect } from "@playwright/test";

test("login, revocation, refresh and responsive layout", async ({ page }) => {
  let expired = false;
  let signedIn = false;
  const restored = {
    user: {
      id: "user-1",
      email: "learner@example.com",
      created_at: "2026-01-01T00:00:00Z",
    },
    expires_at: "2099-01-01T00:00:00Z",
    csrf_token: "C".repeat(43),
  };
  await page.route(
    "http://127.0.0.1:5173/api/integrations/discord/notifications",
    (route) => route.fulfill({ json: [] }),
  );
  await page.route("http://127.0.0.1:5173/api/integrations/discord", (route) =>
    route.fulfill({
      json: {
        enabled: false,
        bound: false,
        version: 1,
        guild_id: null,
        channel_id: null,
        channel_name: null,
        bound_at: null,
      },
    }),
  );
  await page.route("**/api/auth/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/login")) {
      signedIn = true;
      return route.fulfill({ json: restored });
    }
    if (path.endsWith("/logout")) {
      signedIn = false;
      return route.fulfill({ status: 204 });
    }
    if (expired || !signedIn)
      return route.fulfill({
        status: 401,
        json: { error: { code: "UNAUTHORIZED", message: "Unauthorized" } },
      });
    return route.fulfill({ json: restored });
  });
  await page.goto("/profile");
  await expect(page).toHaveURL(/\/login$/);
  await page.screenshot({
    path: "test-results/login-desktop.png",
    fullPage: true,
  });
  async function login() {
    await page.getByLabel("Email address").fill("learner@example.com");
    await page
      .getByLabel("Password", { exact: true })
      .fill("a long test passphrase");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "A space of your own" }),
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
  await expect(
    page.getByRole("heading", { name: "A space of your own" }),
  ).toBeVisible();
  expired = true;
  await page.getByRole("button", { name: "Refresh profile" }).click();
  await expect(page.getByRole("status")).toContainText(
    "Your session has ended",
  );
  expired = false;
  await login();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("status")).toContainText(
    "You have signed out safely",
  );
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
  await page.getByRole("link", { name: "Create account" }).click();
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByLabel("Email address")).toBeFocused();
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
  await page.getByLabel("Email address").fill(email);
  await page
    .getByLabel("Password", { exact: true })
    .fill("a long live browser passphrase");
  await page
    .getByLabel("Confirm password")
    .fill("a long live browser passphrase");
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByRole("status")).toContainText("Your space is ready");
  await page.getByRole("link", { name: "Continue to sign in" }).click();
  await page.getByLabel("Email address").fill(email);
  await page
    .getByLabel("Password", { exact: true })
    .fill("a long live browser passphrase");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("heading", { name: email })).toBeVisible();
  await page.getByRole("button", { name: "Refresh profile" }).click();
  await expect(page.getByRole("button", { name: "Sign out" })).toBeEnabled();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("status")).toContainText(
    "You have signed out safely",
  );
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
});
