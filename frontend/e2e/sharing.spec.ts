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
    await page.getByLabel("邮箱地址").fill(email);
    await page.getByLabel("密码", { exact: true }).fill(password);
    await page.getByRole("button", { name: "登录", exact: true }).click();
  }
  async function register(page: Page, email: string) {
    await page.goto(base + "/register");
    await page.getByLabel("邮箱地址").fill(email);
    await page.getByLabel("密码", { exact: true }).fill(password);
    await page.getByLabel("确认密码").fill(password);
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/auth/register") &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "创建账号" }).click();
    const account = await (await response).json();
    await page.getByRole("link", { name: "前往登录" }).click();
    await login(page, email);
    await expect(
      page.getByRole("heading", { name: "你的个人空间" }),
    ).toBeVisible();
    return account.id as string;
  }
  const suffix = Date.now(),
    ownerEmail = `sharing-owner-${suffix}@example.com`,
    memberEmail = `sharing-member-${suffix}@example.com`;
  try {
    await register(owner, ownerEmail);
    const memberID = await register(member, memberEmail);
    await owner.getByRole("link", { name: "任务", exact: true }).click();
    await owner.getByRole("button", { name: "新建项目", exact: true }).click();
    await owner.getByLabel("项目名称").fill("共享空间");
    await owner.getByRole("button", { name: "保存项目" }).click();
    await owner.getByRole("button", { name: "共享空间", exact: true }).click();
    await owner.getByRole("button", { name: "新建任务", exact: false }).click();
    await owner.getByLabel("任务标题").fill("所有者创建的任务");
    await owner.getByRole("button", { name: "保存任务" }).click();
    await owner.getByRole("button", { name: "成员与邀请" }).click();
    await owner.getByLabel("接收者账号 ID").fill(memberID);
    await owner.getByRole("button", { name: "生成邀请链接" }).click();
    const invitation = await owner
      .getByLabel("仅本次显示，请手工分享给接收者")
      .inputValue();
    await owner.getByRole("button", { name: "关闭对话框" }).click();
    await member.goto(invitation);
    await expect(
      member.getByRole("heading", { name: "项目邀请" }),
    ).toBeVisible();
    expect(new URL(member.url()).hash).toBe("");
    await member.getByRole("link", { name: "登录后接受邀请" }).click();
    await login(member, memberEmail);
    await member.getByRole("button", { name: "接受邀请" }).click();
    await member.getByRole("link", { name: "打开工作空间" }).click();
    await member.getByRole("button", { name: "共享空间", exact: true }).click();
    await expect(
      member.getByRole("button", { name: "删除项目", exact: true }),
    ).toHaveCount(0);
    await member
      .getByRole("button", { name: "编辑任务：所有者创建的任务" })
      .click();
    await expect(member.getByLabel("所属项目")).toBeDisabled();
    await member.getByLabel("任务标题").fill("成员协作修改");
    await member.getByRole("button", { name: "保存任务" }).click();
    await member
      .getByRole("button", { name: "新建任务", exact: false })
      .click();
    await member.getByLabel("任务标题").fill("成员自己的项目任务");
    await member.getByRole("button", { name: "保存任务" }).click();
    await owner.getByRole("button", { name: "刷新", exact: true }).click();
    await expect(
      owner.getByRole("heading", { name: "成员协作修改" }),
    ).toBeVisible();
    await owner.getByRole("button", { name: "成员与邀请" }).click();
    await expect(owner.getByRole("button", { name: "移除成员" })).toBeVisible();
    await owner.screenshot({
      path: "test-results/sharing-members.png",
      fullPage: true,
    });
    await owner.getByRole("button", { name: "移除成员", exact: true }).click();
    await owner.getByRole("button", { name: "确认移除成员" }).click();
    await expect(owner.getByRole("dialog")).not.toBeVisible();
    // The member's existing session cannot see even their own project task now.
    await member.getByRole("button", { name: "刷新", exact: true }).click();
    await expect(
      member.getByRole("heading", { name: "成员自己的项目任务" }),
    ).toHaveCount(0);
    await member.getByRole("button", { name: "刷新项目列表" }).click();
    await expect(
      member.getByRole("button", { name: "共享空间", exact: true }),
    ).toHaveCount(0);
    // Reinvite, then transfer ownership through an explicit confirmation.
    await owner.getByRole("button", { name: "成员与邀请" }).click();
    await owner.getByLabel("接收者账号 ID").fill(memberID);
    await owner.getByRole("button", { name: "生成邀请链接" }).click();
    const again = await owner
      .getByLabel("仅本次显示，请手工分享给接收者")
      .inputValue();
    await owner.getByRole("button", { name: "关闭对话框" }).click();
    await member.goto(again);
    await member.getByRole("link", { name: "登录后接受邀请" }).click();
    await login(member, memberEmail);
    await member.getByRole("button", { name: "接受邀请" }).click();
    await member.getByRole("link", { name: "打开工作空间" }).click();
    await owner.getByRole("button", { name: "成员与邀请" }).click();
    await owner
      .getByRole("button", { name: "转移所有权", exact: true })
      .click();
    await owner.getByRole("button", { name: "确认转移所有权" }).click();
    await expect(
      owner.getByRole("button", { name: "删除项目", exact: true }),
    ).toHaveCount(0);
    await member.getByRole("button", { name: "刷新项目列表" }).click();
    await member.getByRole("button", { name: "共享空间", exact: true }).click();
    await expect(
      member.getByRole("button", { name: "删除项目", exact: true }),
    ).toBeVisible();
    await owner.getByRole("button", { name: "成员与邀请" }).click();
    await owner.getByRole("button", { name: "退出项目", exact: true }).click();
    await owner.getByRole("button", { name: "确认退出项目" }).click();
    await expect(
      owner.getByRole("button", { name: "共享空间", exact: true }),
    ).toHaveCount(0);
    expect(
      await member.evaluate(() => localStorage.length + sessionStorage.length),
    ).toBe(0);
  } finally {
    await ownerContext.close();
    await memberContext.close();
  }
});
