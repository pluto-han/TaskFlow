import { expect, test } from "@playwright/test";

test("account UI to PostgreSQL and fake mail: verify, revoke, change and recover", async ({
  page,
  browser,
}) => {
  test.skip(
    !process.env.TASKFLOW_FLOW_BASE_URL,
    "requires the isolated database and fake inbox",
  );
  const email = "account-flow@example.com";
  let password = "initial account passphrase";
  await page.goto("/register");
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await page.getByRole("link", { name: "Continue to sign in" }).click();
  async function signIn() {
    await page.getByLabel("Email address").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Sign-in & security" }),
    ).toBeVisible();
  }
  await signIn();
  await page.getByRole("button", { name: "Send verification email" }).click();
  await expect(
    page.getByRole("button", { name: "Email requested" }),
  ).toBeDisabled();
  const mail = await (await page.request.post("/__fixture/mail")).json();
  expect(mail.length === 1).toBe(true);
  const verifyLink = mail[0].Link as string;
  await page.goto(verifyLink);
  await expect(
    page.getByRole("button", { name: "Verify email address" }),
  ).toBeVisible();
  expect(new URL(page.url()).hash === "").toBe(true);
  // Opening a page does not consume the capability. Only this click verifies it.
  await page.getByRole("button", { name: "Verify email address" }).click();
  await expect(
    page.getByRole("heading", { name: "Email verified", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Go to profile" }).click();
  await expect(page.getByText("Verified", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText("Verified", { exact: true })).toBeVisible();
  // A distinct browser context creates a second session, not another tab in the same session.
  const peer = await browser.newContext();
  const peerPage = await peer.newPage();
  await peerPage.goto(`${process.env.TASKFLOW_FLOW_BASE_URL}/login`);
  await peerPage.getByLabel("Email address").fill(email);
  await peerPage.getByLabel("Password", { exact: true }).fill(password);
  await peerPage.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    peerPage.getByRole("heading", { name: "Sign-in & security" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Refresh sessions" }).click();
  await page.getByRole("button", { name: /End session signed in/ }).click();
  await page.getByRole("button", { name: "Confirm sign out" }).click();
  await expect(page.getByText("Session signed out.")).toBeVisible();
  await peerPage.reload();
  await expect(
    peerPage.getByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  await peer.close();
  await page.getByText("Change password", { exact: true }).click();
  await page.getByLabel("Current password").fill(password);
  password = "updated account passphrase";
  await page.getByLabel("New password", { exact: true }).fill(password);
  await page.getByLabel("Confirm new password").fill(password);
  await page.getByRole("button", { name: "Update password" }).click();
  await expect(
    page.getByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  await signIn();
  const recoveryPeer = await browser.newContext();
  const recoveryPage = await recoveryPeer.newPage();
  await recoveryPage.goto(`${process.env.TASKFLOW_FLOW_BASE_URL}/login`);
  await recoveryPage.getByLabel("Email address").fill(email);
  await recoveryPage.getByLabel("Password", { exact: true }).fill(password);
  await recoveryPage
    .getByRole("button", { name: "Sign in", exact: true })
    .click();
  await expect(
    recoveryPage.getByRole("heading", { name: "Sign-in & security" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await page.getByRole("link", { name: "Forgot your password?" }).click();
  await expect(
    page.getByRole("heading", { name: "Forgot your password?" }),
  ).toBeVisible();
  await page.getByLabel("Email address").fill(email);
  await page.getByRole("button", { name: "Send reset link" }).click();
  await expect(
    page.getByRole("heading", { name: "Check your email" }),
  ).toBeVisible();
  const resets = await (await page.request.post("/__fixture/mail")).json();
  expect(resets.length === 1).toBe(true);
  const resetLink = resets[0].Link as string;
  await page.goto(resetLink);
  await expect(
    page.getByRole("heading", { name: "Choose a new password" }),
  ).toBeVisible();
  expect(new URL(page.url()).hash === "").toBe(true);
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
  password = "recovered account passphrase";
  await page.getByLabel("New password", { exact: true }).fill(password);
  await page.getByLabel("Confirm new password").fill(password);
  await page.getByRole("button", { name: "Save new password" }).click();
  await expect(
    page.getByRole("heading", { name: "Password updated" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Continue to sign in" }).click();
  await signIn();
  await recoveryPage.reload();
  await expect(
    recoveryPage.getByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  await recoveryPeer.close();
  await page.goto(resetLink);
  await page
    .getByLabel("New password", { exact: true })
    .fill("another passphrase");
  await page.getByLabel("Confirm new password").fill("another passphrase");
  await page.getByRole("button", { name: "Save new password" }).click();
  await expect(
    page.getByRole("heading", { name: "This link is no longer available" }),
  ).toBeVisible();
});
