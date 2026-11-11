import { resolve } from "node:path";
import { test, expect } from "@playwright/test";

for (const width of [1280, 390, 320]) {
  test(`account settings layout and modal at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.route("**/api/**", async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === "/api/auth/browser/session")
        return route.fulfill({
          json: {
            user: {
              id: "user",
              email: "alex.morgan@example.com",
              created_at: "2026-01-01T10:00:00Z",
            },
            csrf_token: "C".repeat(43),
            expires_at: "2099-01-01T00:00:00Z",
          },
        });
      if (path === "/api/auth/email/verification")
        return route.fulfill({ json: { enabled: true, verified_at: null } });
      if (path === "/api/auth/sessions")
        return route.fulfill({
          json: {
            items: [
              {
                id: "current",
                current: true,
                credential_kind: "browser",
                created_at: "2026-11-11T10:00:00Z",
                expires_at: "2026-11-12T10:00:00Z",
              },
              {
                id: "other",
                current: false,
                credential_kind: "browser",
                created_at: "2026-11-11T09:00:00Z",
                expires_at: "2026-11-12T09:00:00Z",
              },
            ],
            next_cursor: "",
          },
        });
      if (path === "/api/integrations/discord")
        return route.fulfill({
          json: { enabled: false, bound: false, version: 1 },
        });
      if (path.endsWith("/notifications")) return route.fulfill({ json: [] });
      return route.fulfill({
        status: 404,
        json: { error: { code: "ROUTE_NOT_FOUND" } },
      });
    });
    await page.goto("/profile");
    await expect(
      page.getByRole("button", { name: "End current session" }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width !== 320)
      await page.screenshot({
        path: resolve(
          import.meta.dirname,
          `../../../artifacts/lesson50/profile-${width}.png`,
        ),
        fullPage: true,
      });
    await page.locator("summary").focus();
    await page.keyboard.press("Enter");
    await expect(page.getByLabel("Current password")).toBeVisible();
    if (width !== 320)
      await page
        .getByRole("region", { name: "Sign-in & security" })
        .screenshot({
          path: resolve(
            import.meta.dirname,
            `../../../artifacts/lesson50/security-expanded-${width}.png`,
          ),
        });
    await page.getByRole("button", { name: "End current session" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
    await expect(
      page.getByRole("button", { name: "End current session" }),
    ).toBeFocused();
  });
}
test("recovery link is cleared before requests and never submitted on load", async ({
  page,
}) => {
  const token = "T".repeat(43);
  let writes = 0;
  await page.route("**/api/**", async (route) => {
    if (route.request().method() === "POST") writes++;
    return route.fulfill({
      status: 401,
      json: { error: { code: "UNAUTHORIZED" } },
    });
  });
  await page.goto(`/reset-password#token=${token}`);
  await expect(
    page.getByRole("heading", { name: "Choose a new password" }),
  ).toBeVisible();
  expect(new URL(page.url()).hash === "").toBe(true);
  expect(writes).toBe(0);
  await page.screenshot({
    path: resolve(
      import.meta.dirname,
      "../../../artifacts/lesson50/reset-desktop.png",
    ),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: resolve(
      import.meta.dirname,
      "../../../artifacts/lesson50/reset-mobile.png",
    ),
    fullPage: true,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
