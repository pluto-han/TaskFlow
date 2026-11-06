import { expect, test } from "@playwright/test";

test("browser to API, scheduler, outbox and stubbed Discord", async ({ page }) => {
  test.skip(!process.env.TASKFLOW_FLOW_BASE_URL, "requires the isolated Go browser fixture");
  // Exercise React's pending route render on slower machines, including CI.
  const browserSession = await page.context().newCDPSession(page);
  await browserSession.send("Emulation.setCPUThrottlingRate", { rate: 4 });
  const email = "browser-flow@example.com";
  const password = "a long browser integration passphrase";
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByRole("link", { name: "创建账号" }).click();
  await expect(page).toHaveURL(/\/register$/);
  // The URL can change before React replaces the login form. Both forms have
  // an email input, so wait for a register-only control before filling it.
  await expect(page.getByLabel("确认密码")).toBeVisible();
  await page.getByLabel("邮箱地址").fill(email);
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByLabel("确认密码").fill(password);
  await expect(page.getByLabel("邮箱地址")).toHaveValue(email);
  const registration = page.waitForResponse((response) =>
    new URL(response.url()).pathname === "/api/auth/register" &&
    response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "创建账号" }).click();
  expect((await registration).status()).toBe(201);
  await expect(page.getByRole("status")).toContainText("注册成功");
  await page.getByRole("link", { name: "前往登录" }).click();
  await expect(page.getByRole("heading", { name: "欢迎回来。", exact: true })).toBeVisible();
  await page.getByLabel("邮箱地址").fill(email);
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("button", { name: "生成绑定码", exact: true }).click();
  const text = await page.getByRole("region", { name: "一次性绑定码" }).locator("code").innerText();
  const code = text.match(/code:([A-Za-z0-9_-]{43})/)?.[1];
  expect(code).toBeTruthy();
  const bound = await page.request.post("/__fixture/bind", { data: { code } });
  expect(bound.status()).toBe(200);
  await page.getByRole("button", { name: "刷新绑定状态" }).click();
  await expect(page.getByText("#test-channel")).toBeVisible();
  await page.getByRole("link", { name: "任务", exact: true }).click();
  await page.getByRole("button", { name: "循环任务", exact: true }).click();
  await page.getByRole("button", { name: "新建循环规则" }).click();
  await page.getByLabel("任务标题").fill("Browser flow reminder");
  await page.getByLabel("每隔几天").fill("3");
  await page.getByLabel("Discord 频道提醒").check();
  await page.getByRole("button", { name: "保存规则" }).click();
  await expect(page.getByRole("heading", { name: "Browser flow reminder" })).toBeVisible();
  expect((await page.request.post("/__fixture/pump")).status()).toBe(204);
  await page.getByRole("button", { name: "今日任务", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Browser flow reminder" })).toBeVisible();
  const messages = await (await page.request.get("/__fixture/messages")).json();
  expect(messages).toHaveLength(1);
  expect(messages[0].content).toContain("<@333> TaskFlow 今日任务：Browser flow reminder");
  expect(messages[0].allowed_mentions).toEqual({ parse: [], users: ["333"] });
});
