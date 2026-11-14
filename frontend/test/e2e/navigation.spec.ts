import { expect, test } from "@playwright/test";

test("URL views survive reload and settings; filters reset pagination", async ({
  page,
}) => {
  await page.clock.setFixedTime(new Date("2026-11-14T10:00:00Z"));
  const requests: string[] = [];
  const item = {
    id: "first",
    title: "First task",
    description: "",
    priority: 1,
    completed: false,
    version: 1,
    created_at: "2026-11-14T10:00:00Z",
    due_date: null,
    project_id: null,
    plan_date: null,
    recurrence_id: null,
    can_move: true,
  };
  await page.route("http://127.0.0.1:5173/api/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/api/auth/browser/session")
      return route.fulfill({
        json: {
          user: {
            id: "u",
            email: "navigation@example.com",
            created_at: item.created_at,
          },
          csrf_token: "C".repeat(43),
          expires_at: "2099-01-01T00:00:00Z",
        },
      });
    if (url.pathname === "/api/projects") return route.fulfill({ json: [] });
    if (url.pathname === "/api/tasks") {
      requests.push(url.search);
      const more = url.searchParams.has("cursor");
      return route.fulfill({
        json: [more ? { ...item, id: "second", title: "Second task" } : item],
        headers: more ? {} : { "X-Next-Cursor": "next" },
      });
    }
    if (url.pathname === "/api/tasks/deep")
      return route.fulfill({
        json: { ...item, id: "deep", title: "A task from another page" },
      });
    if (url.pathname === "/api/tasks/today")
      return route.fulfill({
        json: {
          items: [],
          next_cursor: "",
          plan_date: "2026-11-14",
          timezone: "Europe/Amsterdam",
          server_time: "2026-11-14T10:00:00Z",
          next_day_at: "2026-11-14T23:00:00Z",
        },
      });
    if (url.pathname === "/api/auth/email/verification")
      return route.fulfill({ json: { enabled: false, verified_at: null } });
    if (url.pathname === "/api/auth/sessions")
      return route.fulfill({ json: { items: [], next_cursor: "" } });
    if (url.pathname === "/api/integrations/discord")
      return route.fulfill({
        json: { enabled: false, bound: false, version: 1 },
      });
    if (url.pathname.endsWith("/notifications"))
      return route.fulfill({ json: [] });
    return route.fulfill({
      status: 404,
      json: { error: { code: "ROUTE_NOT_FOUND" } },
    });
  });
  await page.goto("/tasks?priority=1");
  await expect(
    page.getByRole("button", { name: item.title, exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Second task", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Filter by priority").selectOption("3");
  await expect(
    page.getByRole("button", { name: "Second task", exact: true }),
  ).toHaveCount(0);
  await expect.poll(() => requests.at(-1)).toBe("?limit=20&priority=3");
  await page.goBack();
  await expect(page.getByLabel("Filter by priority")).toHaveValue("1");
  await page.getByRole("link", { name: "Today", exact: true }).click();
  await expect(page).toHaveURL(/\/tasks\/today\?priority=1$/);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Today", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Filter by priority")).toHaveValue("1");
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  await page.getByRole("link", { name: "Security", exact: true }).click();
  await expect(page).toHaveURL(/\/settings\/security$/);
  await page.getByRole("link", { name: "Tasks", exact: true }).click();
  await expect(page).toHaveURL(/\/tasks\/today\?priority=1$/);
  await page.goto("/profile");
  await expect(page).toHaveURL(/\/settings\/account$/);
  await page.goto("/tasks?priority=1&task=deep");
  await expect(
    page.getByRole("heading", { name: "A task from another page" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "First task", exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "A task from another page" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Close task details" }).click();
  await expect(page).toHaveURL(/\/tasks\?priority=1$/);
  await page.goBack();
  await expect(
    page.getByRole("heading", { name: "A task from another page" }),
  ).toBeVisible();
  await page.goto("/tasks?priority=1&task=unavailable");
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByRole("button", { name: /Edit task:/ })).toHaveCount(0);
});
