import { test, expect, type Page } from "@playwright/test";

test("two accounts share tasks, bind invite recipient, revoke access and transfer ownership", async ({
  browser,
}) => {
  test.skip(
    process.env.TASKFLOW_LIVE_E2E !== "1",
    "Requires isolated API/database",
  );
  const ownerContext = await browser.newContext(),
    memberContext = await browser.newContext();
  const owner = await ownerContext.newPage(),
    member = await memberContext.newPage();
  const base = "http://127.0.0.1:5173";
  const password = "a long shared browser passphrase";
  async function login(page: Page, email: string) {
    await page.getByLabel("Email address").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
  }
  async function register(page: Page, email: string) {
    await page.goto(base + "/register");
    await page.getByLabel("Email address").fill(email);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByLabel("Confirm password").fill(password);
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/auth/register") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Create account" }).click();
    const account = await (await response).json();
    await page.getByRole("link", { name: "Continue to sign in" }).click();
    await login(page, email);
    await expect(
      page.getByRole("heading", { name: "A space of your own" }),
    ).toBeVisible();
    return account.id as string;
  }
  const suffix = Date.now(),
    ownerEmail = `sharing-owner-${suffix}@example.com`,
    memberEmail = `sharing-member-${suffix}@example.com`;
  try {
    await register(owner, ownerEmail);
    const memberID = await register(member, memberEmail);
    await owner.getByRole("link", { name: "Tasks", exact: true }).click();
    await owner
      .getByRole("button", { name: "New project", exact: true })
      .click();
    await owner.getByLabel("Project name").fill("A shared space");
    await owner.getByRole("button", { name: "Save project" }).click();
    await owner
      .getByRole("button", { name: "A shared space", exact: true })
      .click();
    await owner.getByRole("button", { name: "New task", exact: false }).click();
    await owner.getByLabel("Task title").fill("A task from the owner");
    await owner.getByRole("button", { name: "Save task" }).click();
    await owner.getByRole("button", { name: "Members & invitations" }).click();
    await owner.getByLabel("Recipient account ID").fill(memberID);
    await owner.getByRole("button", { name: "Create invitation link" }).click();
    const invitation = await owner
      .getByLabel("Shown once. Share this link privately with the recipient.")
      .inputValue();
    await owner.getByRole("button", { name: "Close dialog" }).click();
    await member.goto(invitation);
    await expect(
      member.getByRole("heading", { name: "Project invitation" }),
    ).toBeVisible();
    expect(new URL(member.url()).hash).toBe("");
    await member.getByRole("link", { name: "Sign in to accept" }).click();
    await login(member, memberEmail);
    await member.getByRole("button", { name: "Accept invitation" }).click();
    await member.getByRole("link", { name: "Open workspace" }).click();
    await member
      .getByRole("button", { name: "A shared space", exact: true })
      .click();
    await expect(
      member.getByRole("button", { name: "Delete project", exact: true }),
    ).toHaveCount(0);
    await member
      .getByRole("button", { name: "Edit task: A task from the owner" })
      .click();
    await expect(member.getByLabel("Project", { exact: true })).toBeDisabled();
    await member.getByLabel("Task title").fill("A shared edit from a member");
    await member.getByRole("button", { name: "Save task" }).click();
    await member
      .getByRole("button", { name: "New task", exact: false })
      .click();
    await member.getByLabel("Task title").fill("A member’s project task");
    await member.getByRole("button", { name: "Save task" }).click();
    await owner.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(
      owner.getByRole("heading", { name: "A shared edit from a member" }),
    ).toBeVisible();
    await owner.getByRole("button", { name: "Members & invitations" }).click();
    await expect(
      owner.getByRole("button", { name: "Remove member" }),
    ).toBeVisible();
    await owner.screenshot({
      path: "test-results/sharing-members.png",
      fullPage: true,
    });
    await owner
      .getByRole("button", { name: "Remove member", exact: true })
      .click();
    await owner.getByRole("button", { name: "Confirm remove member" }).click();
    await expect(owner.getByRole("dialog")).not.toBeVisible();
    // The member's existing session cannot see even their own project task now.
    await member.getByRole("button", { name: "Refresh", exact: true }).click();
    await expect(
      member.getByRole("heading", { name: "A member’s project task" }),
    ).toHaveCount(0);
    await member.getByRole("button", { name: "Refresh projects" }).click();
    await expect(
      member.getByRole("button", { name: "A shared space", exact: true }),
    ).toHaveCount(0);
    // Reinvite, then transfer ownership through an explicit confirmation.
    await owner.getByRole("button", { name: "Members & invitations" }).click();
    await owner.getByLabel("Recipient account ID").fill(memberID);
    await owner.getByRole("button", { name: "Create invitation link" }).click();
    const again = await owner
      .getByLabel("Shown once. Share this link privately with the recipient.")
      .inputValue();
    await owner.getByRole("button", { name: "Close dialog" }).click();
    await member.goto(again);
    await member.getByRole("link", { name: "Sign in to accept" }).click();
    await login(member, memberEmail);
    await member.getByRole("button", { name: "Accept invitation" }).click();
    await member.getByRole("link", { name: "Open workspace" }).click();
    await owner.getByRole("button", { name: "Members & invitations" }).click();
    await owner
      .getByRole("button", { name: "Transfer ownership", exact: true })
      .click();
    await owner
      .getByRole("button", { name: "Confirm transfer ownership" })
      .click();
    await expect(
      owner.getByRole("button", { name: "Delete project", exact: true }),
    ).toHaveCount(0);
    await member.getByRole("button", { name: "Refresh projects" }).click();
    await member
      .getByRole("button", { name: "A shared space", exact: true })
      .click();
    await expect(
      member.getByRole("button", { name: "Delete project", exact: true }),
    ).toBeVisible();
    await owner.getByRole("button", { name: "Members & invitations" }).click();
    await owner
      .getByRole("button", { name: "Leave project", exact: true })
      .click();
    await owner.getByRole("button", { name: "Confirm leave project" }).click();
    await expect(
      owner.getByRole("button", { name: "A shared space", exact: true }),
    ).toHaveCount(0);
    expect(
      await member.evaluate(() => localStorage.length + sessionStorage.length),
    ).toBe(0);
  } finally {
    await ownerContext.close();
    await memberContext.close();
  }
});
