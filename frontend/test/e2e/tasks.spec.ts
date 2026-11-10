import { mockBrowserAuth } from "./auth-fixture";
import { test, expect } from "@playwright/test";

test.use({ timezoneId: "Europe/Amsterdam" });

test("workspace pagination, filtering and mobile layout", async ({ page }) => {
  const task = {
    can_move: true,
    id: "task-a",
    title: "Make time for what matters",
    description: "Gather today’s thoughts. Give one thing your full attention.",
    completed: false,
    priority: 3,
    project_id: null,
    due_date: "2026-12-15T09:30:00Z",
    created_at: "2026-01-01T00:00:00Z",
    version: 1,
  };
  const auth = mockBrowserAuth("learner@example.com");
  await page.route("http://127.0.0.1:5173/api/**", async (route) => {
    const url = new URL(route.request().url());
    if (await auth(route)) return;
    if (url.pathname.endsWith("/projects"))
      return route.fulfill({
        json: [
          {
            id: "11111111-1111-4111-8111-111111111111",
            name: "Room to grow",
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
            title: "Finish a book",
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
  await page.getByLabel("Email address").fill("learner@example.com");
  await page
    .getByLabel("Password", { exact: true })
    .fill("a long test passphrase");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("link", { name: "Tasks", exact: true }).click();
  await expect(page.getByRole("heading", { name: task.title })).toBeVisible();
  await page.getByRole("button", { name: "Load more" }).click();
  await expect(
    page.getByRole("heading", { name: "Finish a book" }),
  ).toBeVisible();
  await page.screenshot({
    path: "test-results/tasks-desktop.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "Room to grow", exact: true }).click();
  await expect(page.getByText("No tasks here yet")).toBeVisible();
  await page.getByRole("button", { name: "All tasks", exact: false }).click();
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
  await page.getByRole("button", { name: "Edit task: " + task.title }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page.getByLabel("Due date (local time)")).toHaveValue(
    /^2026-12-15T10:30(:00)?$/,
  );
  await page.screenshot({
    path: "test-results/task-editor-mobile.png",
    fullPage: true,
  });
  await page.getByLabel("Due date (local time)").fill("2026-03-29T02:30");
  await page.getByRole("button", { name: "Save task" }).click();
  await expect(page.getByRole("alert")).toContainText("daylight saving");
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(
    page.getByRole("button", { name: "Edit task: " + task.title }),
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
  await page.getByLabel("Email address").fill(email);
  await page
    .getByLabel("Password", { exact: true })
    .fill("a long live task passphrase");
  await page.getByLabel("Confirm password").fill("a long live task passphrase");
  await page.getByRole("button", { name: "Create account" }).click();
  await page.getByRole("link", { name: "Continue to sign in" }).click();
  await page.getByLabel("Email address").fill(email);
  await page
    .getByLabel("Password", { exact: true })
    .fill("a long live task passphrase");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("link", { name: "Tasks", exact: true }).click();
  await expect(page.getByText("No tasks here yet")).toBeVisible();
  await page.getByRole("button", { name: "New project", exact: true }).click();
  await page.getByLabel("Project name").fill("Learning plan");
  await page.getByLabel("Project description").fill("Browser acceptance test");
  await page.getByRole("button", { name: "Save project" }).click();
  await page
    .getByRole("button", { name: "Learning plan", exact: true })
    .click();
  await page.getByRole("button", { name: "Edit project", exact: true }).click();
  await page
    .getByLabel("Project description")
    .fill("Updated project description");
  await page.getByRole("button", { name: "Save project" }).click();
  await expect(
    page.getByText("Updated project description", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "New task", exact: false }).click();
  await page.getByLabel("Task title").fill("Finish Lesson 19");
  await page
    .getByLabel("Description", { exact: true })
    .fill("Created in the browser");
  await page.getByLabel("Priority", { exact: true }).selectOption("3");
  await page.getByLabel("Due date (local time)").fill("2026-12-15T10:30");
  const createdResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/tasks") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Save task" }).click();
  const created = await (await createdResponse).json();
  expect(created.due_date).toBe("2026-12-15T09:30:00Z");
  expect(created.project_id).not.toBeNull();
  await expect(
    page.getByRole("heading", { name: "Finish Lesson 19" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Delete project", exact: true })
    .click();
  await page.getByRole("button", { name: "Confirm delete" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "This project still has tasks",
  );
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await page
    .getByRole("checkbox", { name: "Complete task: Finish Lesson 19" })
    .check();
  await expect(page.getByRole("checkbox")).toBeEnabled();
  await expect(page.getByRole("checkbox")).toBeChecked();
  await page
    .getByRole("button", { name: "Edit task: Finish Lesson 19" })
    .click();
  await page.getByRole("button", { name: "Clear due date" }).click();
  await page.getByLabel("Project", { exact: true }).selectOption("");
  const patch = page.waitForRequest(
    (request) =>
      request.url().includes("/api/tasks/") && request.method() === "PATCH",
  );
  await page.getByRole("button", { name: "Save task" }).click();
  expect((await patch).postDataJSON()).toEqual({
    version: 2,
    due_date: null,
    project_id: null,
  });
  await expect(page.getByText("No tasks here yet")).toBeVisible();
  await page
    .getByRole("button", { name: "Delete project", exact: true })
    .click();
  await page.getByRole("button", { name: "Confirm delete" }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await page.getByRole("button", { name: "Unassigned", exact: true }).click();
  await page
    .getByRole("button", { name: "Delete task: Finish Lesson 19" })
    .click();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Finish Lesson 19" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Delete task: Finish Lesson 19" })
    .click();
  await page.getByRole("button", { name: "Confirm delete" }).click();
  await expect(page.getByText("No tasks here yet")).toBeVisible();
});
