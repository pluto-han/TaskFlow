import { mockBrowserAuth } from "./auth-fixture";
import { test, expect } from "@playwright/test";

test("recurrence management keeps existing styling and works on mobile", async ({
  page,
}) => {
  let rule: Record<string, unknown> | null = null;
  const auth = mockBrowserAuth("recurrence@example.com");
  await page.route("http://127.0.0.1:5173/api/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (await auth(route)) return;
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
  await page.getByLabel("Email address").fill("recurrence@example.com");
  await page
    .getByLabel("Password", { exact: true })
    .fill("a long learning passphrase");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("link", { name: "Tasks", exact: true }).click();
  await page
    .getByRole("button", { name: "Recurring tasks", exact: true })
    .click();
  await page.getByRole("button", { name: "New recurring task" }).click();
  await page
    .getByLabel("Task title")
    .fill("A little reading, every three days");
  await page.getByLabel("Repeat every (days)").fill("3");
  await page.getByLabel("Discord channel reminder").check();
  await expect(page.getByText(/Schedule preview: 2026-11-05/)).toBeVisible();
  await page.getByRole("button", { name: "Save schedule" }).click();
  await expect(
    page.getByRole("heading", { name: "A little reading, every three days" }),
  ).toBeVisible();
  await expect(
    page.getByText("Discord reminder", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Pause", exact: true }).click();
  await expect(page.getByText("Every 3 days · Paused")).toBeVisible();
  await page.getByRole("button", { name: "Resume", exact: true }).click();
  await expect(page.getByText("Every 3 days · Active")).toBeVisible();
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
  await page.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(
    page.getByText(/Your existing tasks and history will stay/),
  ).toBeVisible();
  await page.getByRole("button", { name: "Confirm delete" }).click();
  await expect(
    page.getByRole("heading", { name: "Room for a new rhythm" }),
  ).toBeVisible();
});
