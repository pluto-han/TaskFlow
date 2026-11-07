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
      title: "Yesterday’s plan",
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
  await page.getByLabel("Email address").fill("today@example.com");
  await page
    .getByLabel("Password", { exact: true })
    .fill("a long test passphrase");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("link", { name: "Tasks", exact: true }).click();
  await page.getByRole("button", { name: "Today", exact: true }).click();
  await expect(
    page.getByText("Amsterdam time / Europe/Amsterdam · 2026-11-05"),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "New project" })).toHaveCount(
    0,
  );
  await page.getByRole("button", { name: "＋ New task" }).click();
  await expect(page.getByLabel("Project", { exact: true })).toHaveCount(0);
  await page.getByLabel("Task title").fill("Amsterdam today task");
  await page.getByRole("button", { name: "Save task" }).click();
  await expect(
    page.getByRole("heading", { name: "Amsterdam today task" }),
  ).toBeVisible();
  await page
    .getByRole("checkbox", { name: "Complete task: Amsterdam today task" })
    .check();
  await expect(
    page.getByRole("checkbox", {
      name: "Mark incomplete: Amsterdam today task",
    }),
  ).toBeChecked();
  await page
    .getByRole("button", { name: "Edit task: Amsterdam today task" })
    .click();
  await expect(page.getByLabel("Project", { exact: true })).toHaveCount(0);
  await page.getByLabel("Task title").fill("Edited today task");
  await page.getByRole("button", { name: "Save task" }).click();
  await page.getByRole("button", { name: "＋ New task" }).click();
  await page.getByLabel("Task title").fill("A draft for a new day");
  businessDate = "2026-11-06";
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(
    page.getByRole("region", { name: "Planning date changed" }),
  ).toBeVisible();
  await expect(page.getByLabel("Task title")).toHaveValue(
    "A draft for a new day",
  );
  await expect(page.getByRole("button", { name: "Save task" })).toBeDisabled();
  await page.getByRole("button", { name: "Refresh and confirm date" }).click();
  await page.getByRole("button", { name: "Save task" }).click();
  await expect(
    page.getByRole("heading", { name: "Edited today task" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: "A draft for a new day" }),
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
  await page.getByRole("button", { name: /All tasks/ }).click();
  await page.getByLabel("Filter by planned date").fill("2026-11-05");
  await expect(
    page.getByRole("heading", { name: "Edited today task" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "A draft for a new day" }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Delete task: Edited today task" })
    .click();
  await page.getByRole("button", { name: "Confirm delete" }).click();
  await expect(page.getByText("No tasks here yet")).toBeVisible();
});
