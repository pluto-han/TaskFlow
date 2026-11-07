import { test, expect } from "@playwright/test";
test("profile channel binding, private code and mobile layout", async ({
  page,
}) => {
  let bound = false,
    version = 1;
  const code = "C".repeat(43);
  await page.route("http://127.0.0.1:5173/api/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
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
          email: "discord@example.com",
          created_at: "2026-01-01T00:00:00Z",
        },
      });
    if (path.endsWith("/discord/notifications"))
      return route.fulfill({ json: [] });
    if (path.endsWith("/discord/code"))
      return route.fulfill({
        status: 201,
        json: { code, expires_at: "2099-01-01T00:00:00Z" },
      });
    if (path.endsWith("/integrations/discord")) {
      if (req.method() === "DELETE") {
        expect(req.postDataJSON().version).toBe(version);
        bound = false;
        version++;
        return route.fulfill({ status: 204 });
      }
      return route.fulfill({
        json: {
          enabled: true,
          version,
          bound,
          guild_id: bound ? "111" : null,
          channel_id: bound ? "222" : null,
          channel_name: bound ? "test-channel" : null,
          bound_at: bound ? "2026-11-06T12:00:00Z" : null,
        },
      });
    }
    return route.fulfill({ status: 404 });
  });
  await page.goto("/login");
  await page.getByLabel("Email address").fill("discord@example.com");
  await page
    .getByLabel("Password", { exact: true })
    .fill("a long learning passphrase");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page
    .getByRole("button", { name: "Generate connection code", exact: true })
    .click();
  await expect(
    page.getByRole("region", { name: "One-time connection code" }),
  ).toContainText(code);
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
  await page.setViewportSize({ width: 375, height: 812 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/lesson33-code-mobile.png",
    fullPage: true,
  });
  bound = true;
  version = 2; // A Discord-signed command completes the binding outside the website.
  await page.getByRole("button", { name: "Refresh connection" }).click();
  await expect(page.getByText("#test-channel")).toBeVisible();
  await expect(
    page.getByRole("region", { name: "One-time connection code" }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Disconnect and revoke codes" })
    .click();
  await page.getByRole("button", { name: "Confirm disconnect" }).click();
  await expect(page.getByText("Not connected", { exact: true })).toBeVisible();
});
