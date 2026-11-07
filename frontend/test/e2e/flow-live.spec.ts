import { expect, test } from "@playwright/test";

test("browser to API, scheduler, outbox and stubbed Discord", async ({
  page,
}) => {
  test.skip(
    !process.env.TASKFLOW_FLOW_BASE_URL,
    "requires the isolated Go browser fixture",
  );
  // Exercise React's pending route render on slower machines, including CI.
  const browserSession = await page.context().newCDPSession(page);
  await browserSession.send("Emulation.setCPUThrottlingRate", { rate: 4 });
  const email = "browser-flow@example.com";
  const password = "a long browser integration passphrase";
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByRole("link", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/register$/);
  // The URL can change before React replaces the login form. Both forms have
  // an email input, so wait for a register-only control before filling it.
  await expect(page.getByLabel("Confirm password")).toBeVisible();
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await expect(page.getByLabel("Email address")).toHaveValue(email);
  const registration = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/auth/register" &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Create account" }).click();
  expect((await registration).status()).toBe(201);
  await expect(page.getByRole("status")).toContainText("Your space is ready");
  await page.getByRole("link", { name: "Continue to sign in" }).click();
  await expect(
    page.getByRole("heading", { name: "Welcome back.", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page
    .getByRole("button", { name: "Generate connection code", exact: true })
    .click();
  const text = await page
    .getByRole("region", { name: "One-time connection code" })
    .locator("code")
    .innerText();
  const code = text.match(/code:([A-Za-z0-9_-]{43})/)?.[1];
  expect(code).toBeTruthy();
  const bound = await page.request.post("/__fixture/bind", { data: { code } });
  expect(bound.status()).toBe(200);
  await page.getByRole("button", { name: "Refresh connection" }).click();
  await expect(page.getByText("#test-channel")).toBeVisible();
  await page.getByRole("link", { name: "Tasks", exact: true }).click();
  await page
    .getByRole("button", { name: "Recurring tasks", exact: true })
    .click();
  await page.getByRole("button", { name: "New recurring task" }).click();
  await page.getByLabel("Task title").fill("Browser flow reminder");
  await page.getByLabel("Repeat every (days)").fill("3");
  await page.getByLabel("Discord channel reminder").check();
  await page.getByRole("button", { name: "Save schedule" }).click();
  await expect(
    page.getByRole("heading", { name: "Browser flow reminder" }),
  ).toBeVisible();
  expect((await page.request.post("/__fixture/pump")).status()).toBe(204);
  await page.getByRole("button", { name: "Today", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Browser flow reminder" }),
  ).toBeVisible();
  const messages = await (await page.request.get("/__fixture/messages")).json();
  expect(messages).toHaveLength(1);
  expect(messages[0].content).toContain("<@333>");
  expect(messages[0].content).toContain("Browser flow reminder");
  expect(messages[0].allowed_mentions).toEqual({ parse: [], users: ["333"] });
});
